# Task: 02-add-public-ip-detection-helper

## Feature: 02_settings-https-tls

## Dependencies

_None_

## Plan Section

### 2. Add public IP detection helper
**Depends on**: none
**Files:**
- Create: `internal/network/publicip.go`

**What to do:**
- Implement `PublicIP(client *http.Client) (string, error)`.
- Read `AXON_PUBLIC_IP` env var first to skip external call.
- Fetch from `https://ipv4.icanhazip.com/` with fallback to `https://ifconfig.me/ip`.
- Use a 10-second timeout and read at most 64 bytes.

**Must NOT do:**
- Do not expose this result on an unauthenticated endpoint.

**Verify:**
- `go build ./internal/network` → OK

---

## Task Type

greenfield
