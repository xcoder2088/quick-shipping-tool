# QuickProof

© 2025-2026 Francois Bradette. All rights reserved.

**This is proprietary, closed-source software.** See [LICENSE](./LICENSE)
for details — no permission is granted to copy, use, modify, or
redistribute this code without the copyright holder's prior written
consent. This repository is private; do not make it public and do not
share its contents outside of individuals explicitly authorized by the
copyright holder.

## Overview
QuickProof is the mobile-facing companion app to QuickQuoteTool: a
Go-backed PWA (installable on Android via TWA — see
`static/assetlinks.json`, `static/manifest.webmanifest`) for photographing
shipments, watermarking the photos, and emailing the complete record.

## Getting Started
This is a multi-file Go package — use `go run .` (not `go run main.go`,
which only compiles that one file and fails with "undefined" errors).

```bash
go mod tidy
go run .
```

Server listens on `:8081`. Email delivery (MailerSend) is optional at
startup — the server still runs without it, but sending will fail until
these are set:

```bash
export MAILERSEND_API_KEY='your-mailersend-api-key'
export MAILERSEND_FROM='your-verified-sender@yourdomain.com'
go run .
```
