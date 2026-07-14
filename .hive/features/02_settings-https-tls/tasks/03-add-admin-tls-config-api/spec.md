# Task: 03-add-admin-tls-config-api

## Feature: 02_settings-https-tls

## Dependencies

- **1. add-https-config-model-and-yaml-loader** (01-add-https-config-model-and-yaml-loader)
- **2. add-public-ip-detection-helper** (02-add-public-ip-detection-helper)

## Plan Section

### 3. Add admin TLS config API
**Depends on**: 1, 2
**Files:**
- Create: `internal/api/handlers/admin/tls.go`
- Modify: `internal/api/router.go` (inside `registerAdminRoutes`)

**What to do:**
- Create `TLSHandler` with `Get`, `Put`, `PublicIP`, and `CheckDNS` methods.
- `PUT /api/admin/tls-config` validates domain/email/TOS and saves YAML.
- `GET /api/admin/tls-config` returns current config + valid flag + cert dir.
- `GET /api/admin/tls-config/public-ip` returns public IP.
- `GET /api/admin/tls-config/check-dns?domain=` resolves domain and compares to public IP.
- Register these routes inside `registerAdminRoutes`.

**Verify:**
- `go test ./internal/api -run TestTLS -v` → PASS

---

## Task Type

modification

## Completed Tasks

- 01-add-https-config-model-and-yaml-loader: Created internal/config/https.go with HTTPSConfig model, IsValid validation, and YAML load/save helpers. Added internal/config/https_test.go covering disabled/enabled validation, missing-file defaults, and round-trip persistence. Verified with go test ./internal/config -run HTTPS -v (7/7 PASS) and go vet ./internal/config.
- 02-add-public-ip-detection-helper: Created internal/network/publicip.go with PublicIP(client) helper honoring AXON_PUBLIC_IP, falling back from ipv4.icanhazip.com to ifconfig.me/ip with a 10s timeout and 64-byte read limit. Added tests covering env override, primary/fallback services, whitespace trim, max read size, and dual-failure cases. Verified with `go build ./internal/network` and `go test ./internal/network` (7/7 pass).
