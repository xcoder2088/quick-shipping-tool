package main

import (
	"net/http"
	"os"
)

// ==================================================
// DIGITAL ASSET LINKS (TWA verification)
// ==================================================
// Required by Google so the Android app (Trusted Web Activity) opens in
// full-screen native mode instead of a Chrome Custom Tab with a visible
// address bar. Google checks this exact URL:
//
//     https://shipping.quickquotetool.ca/.well-known/assetlinks.json
//
// The file itself comes straight from the PWABuilder "Package For Stores"
// download (same one that has QuickProof.aab / signing.keystore in it) —
// drop it at ./static/assetlinks.json in the repo, this handler serves it
// at the correct root-level path with the right content type.

func assetLinksHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	path := "./static/assetlinks.json"

	if _, err := os.Stat(path); err != nil {

		http.NotFound(w, r)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

	http.ServeFile(w, r, path)
}
