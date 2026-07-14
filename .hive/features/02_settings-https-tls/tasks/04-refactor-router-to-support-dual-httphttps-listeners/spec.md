# Task: 04-refactor-router-to-support-dual-httphttps-listeners

## Feature: 02_settings-https-tls

## Dependencies

- **1. add-https-config-model-and-yaml-loader** (01-add-https-config-model-and-yaml-loader)

## Plan Section

### 4. Refactor Router to support dual HTTP/HTTPS listeners
**Depends on**: 1
**Files:**
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`

**What to do:**
- In `Router` struct, store `httpServer`, `httpsServer`, and `tlsManager`.
- Add `Start(addr string, cfg config.HTTPSConfig) error` method that:
  - Starts HTTP server in a goroutine using the Gin engine.
  - If enabled/valid and port 443 can be bound, creates an `autocert.Manager` with `HostWhitelist(cfg.Domain)` and starts HTTPS on `:443`.
  - If port 443 is in use, logs a warning and continues with HTTP only.
- Update `Shutdown()` to shut down both servers with 5-second timeout.
- Update `cmd/server/main.go` to load HTTPS config and call `router.Start(...)` instead of `router.Run(...)`.
- Update startup banner to mention HTTPS when active.

**Verify:**
- `go build ./...` → OK
- `go test ./internal/api -v` → PASS

---

## Task Type

modification

## Completed Tasks

- 01-add-https-config-model-and-yaml-loader: Created internal/config/https.go with HTTPSConfig model, IsValid validation, and YAML load/save helpers. Added internal/config/https_test.go covering disabled/enabled validation, missing-file defaults, and round-trip persistence. Verified with go test ./internal/config -run HTTPS -v (7/7 PASS) and go vet ./internal/config.
- 02-add-public-ip-detection-helper: Created internal/network/publicip.go with PublicIP(client) helper honoring AXON_PUBLIC_IP, falling back from ipv4.icanhazip.com to ifconfig.me/ip with a 10s timeout and 64-byte read limit. Added tests covering env override, primary/fallback services, whitespace trim, max read size, and dual-failure cases. Verified with `go build ./internal/network` and `go test ./internal/network` (7/7 pass).
