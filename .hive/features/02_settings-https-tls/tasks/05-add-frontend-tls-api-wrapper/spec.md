# Task: 05-add-frontend-tls-api-wrapper

## Feature: 02_settings-https-tls

## Dependencies

- **3. add-admin-tls-config-api** (03-add-admin-tls-config-api)

## Plan Section

### 5. Add frontend TLS API wrapper
**Depends on**: 3
**Files:**
- Modify: `web/src/lib/api.ts`

**What to do:**
- Add `tlsApi` object with methods:
  - `get()` → returns TLS config object.
  - `save(payload)` → PUT to `/api/admin/tls-config`.
  - `publicIp()` → returns `{ ip: string }`.
  - `checkDns(domain)` → returns `{ domain, resolved_ips, public_ip, ok }`.

**Verify:**
- `cd web && npx tsc --noEmit` → no errors (assuming the command exists).

---

## Task Type

modification

## Context

## todo

# Todo: 03-add-admin-tls-config-api

- [x] Read plan and verify dependencies satisfied
- [x] Write failing tests for TLSHandler (Get, Put, PublicIP, CheckDNS)
- [x] Implement TLSHandler in internal/api/handlers/admin/tls.go
- [x] Register TLS routes in internal/api/router.go
- [x] Run verification: go test ./internal/api -run TestTLS -v
- [in_progress] Commit work


## Completed Tasks

- 01-add-https-config-model-and-yaml-loader: Created internal/config/https.go with HTTPSConfig model, IsValid validation, and YAML load/save helpers. Added internal/config/https_test.go covering disabled/enabled validation, missing-file defaults, and round-trip persistence. Verified with go test ./internal/config -run HTTPS -v (7/7 PASS) and go vet ./internal/config.
- 02-add-public-ip-detection-helper: Created internal/network/publicip.go with PublicIP(client) helper honoring AXON_PUBLIC_IP, falling back from ipv4.icanhazip.com to ifconfig.me/ip with a 10s timeout and 64-byte read limit. Added tests covering env override, primary/fallback services, whitespace trim, max read size, and dual-failure cases. Verified with `go build ./internal/network` and `go test ./internal/network` (7/7 pass).
- 03-add-admin-tls-config-api: Created internal/api/handlers/admin/tls.go with TLSHandler exposing Get/Put/PublicIP/CheckDNS backed by config.LoadHTTPSConfig/SaveHTTPSConfig and network.PublicIP. Registered /api/admin/tls-config routes in internal/api/router.go. Added internal/api/tls_test.go with 5 tests covering defaults, save/round-trip, validation, public IP env override, and DNS match. Verification: go test ./internal/api -run TestTLS -v passes (5/5), full internal/api package tests pass, and go vet is clean.
- 04-refactor-router-to-support-dual-httphttps-listeners: Added dual HTTP/HTTPS listener. HTTP uses net.Listen + http.Server with Gin engine; HTTPS path uses autocert.Manager with TLS-ALPN-01 on :443 when https.yml enabled/valid. Shutdown now stops both servers with timeout. Updated cmd/server/main.go to load https.yml and call router.Start. Verified go build ./... and go test ./internal/api -v pass.
