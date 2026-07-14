# Task: 06-redesign-settingssvelte-with-tabs

## Feature: 02_settings-https-tls

## Dependencies

- **5. add-frontend-tls-api-wrapper** (05-add-frontend-tls-api-wrapper)

## Plan Section

### 6. Redesign Settings.svelte with tabs
**Depends on**: 5
**Files:**
- Modify: `web/src/pages/Settings.svelte`
- Modify: `web/src/lib/components/ChangePasswordCard.svelte`

**What to do:**
- Wrap page with `Tabs.Root` default value `security`.
- Tabs: `Security`, `HTTPS`, `Runtime`.
- **Security tab:** render redesigned `ChangePasswordCard` (better spacing, icon, helper text) and keep import/export settings card.
- **Runtime tab:** refactor current settings list into category cards; each row shows label, description, current value, and inline Edit/Save controls with fewer nested dividers.
- **HTTPS tab:** create new section with IP display, DNS instructions, form, and save action.
- Remove the old monolithic layout.

**Verify:**
- `cd web && npm run build` → zero warnings.

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
- 05-add-frontend-tls-api-wrapper: Added tlsApi to web/src/lib/api.ts with get, save, publicIp, and checkDns methods backed by /api/admin/tls-config endpoints; introduced TLSConfig/TLSConfigPayload/TLSCheckDNSResult types matching the backend response shapes. Added unit tests in web/src/lib/__tests__/tls-api.test.ts covering all four methods. Tests pass (4/4). Vite build succeeds with no warnings. `npx tsc --noEmit` currently fails due to pre-existing TypeScript issues in the repo (proxy-pools-api.test.ts missing beforeEach import, usageApi URLSearchParams type, and Svelte component type declarations), not introduced by this change.
