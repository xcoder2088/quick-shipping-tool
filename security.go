package main

// ==================================================
// security.go — rate limiting + security headers
//
// No external dependencies: everything here uses only the
// standard library, so no go.mod change / go get is needed.
// ==================================================

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ==================================================
// CLIENT IP
// ==================================================

// clientIP returns the best-effort real visitor IP. The site sits behind
// Cloudflare, so r.RemoteAddr alone would be Cloudflare's edge IP for every
// visitor — useless for rate limiting. CF-Connecting-IP is Cloudflare's
// own header carrying the true client IP and is trustworthy as long as
// Cloudflare is the only thing allowed to reach this origin server
// directly (true for a typical Cloudflare-proxied setup).
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ==================================================
// RATE LIMITER
// ==================================================
//
// Simple fixed-window counter per (IP, bucket). Not as smooth as a token
// bucket, but easy to reason about and enough to stop a script from
// hammering an expensive endpoint (photo upload + outbound email via
// MailerSend, which has its own sending quota/cost) — this is abuse
// prevention, not precise traffic shaping.

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]*rateEntry
}

type rateEntry struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		limit:   limit,
		window:  window,
		entries: make(map[string]*rateEntry),
	}
	go rl.sweepLoop()
	return rl
}

// sweepLoop periodically drops expired entries so the map doesn't grow
// forever under sustained traffic from many distinct IPs.
func (rl *rateLimiter) sweepLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		now := time.Now()
		rl.mu.Lock()
		for k, e := range rl.entries {
			if now.After(e.windowEnd) {
				delete(rl.entries, k)
			}
		}
		rl.mu.Unlock()
	}
}

// allow reports whether the given key (typically "bucket:ip") may proceed,
// incrementing its counter if so.
func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, ok := rl.entries[key]
	if !ok || now.After(e.windowEnd) {
		rl.entries[key] = &rateEntry{count: 1, windowEnd: now.Add(rl.window)}
		return true
	}

	if e.count >= rl.limit {
		return false
	}

	e.count++
	return true
}

// rateLimit wraps a handler so each client IP gets at most `limit` requests
// per `window`. bucket namespaces the limiter so the same IP is tracked
// separately per protected endpoint.
func rateLimit(bucket string, limit int, window time.Duration, next http.HandlerFunc) http.HandlerFunc {
	rl := newRateLimiter(limit, window)
	return func(w http.ResponseWriter, r *http.Request) {
		key := bucket + ":" + clientIP(r)
		if !rl.allow(key) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many requests — please slow down and try again shortly.", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// ==================================================
// SECURITY HEADERS
// ==================================================
//
// securityHeaders wraps every response with a consistent baseline.
// QuickProof is a PWA with inline <style>/<script> throughout and no
// external script/style sources beyond Bootstrap's CDN, so the CSP below
// stays close to that shape rather than a fully locked-down policy —
// tightening further (nonces/hashes for every inline block) is a bigger
// follow-up project, not a drop-in change.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(self)")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
				"img-src 'self' data: blob: https:; "+
				"font-src 'self' data: https://cdn.jsdelivr.net; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
