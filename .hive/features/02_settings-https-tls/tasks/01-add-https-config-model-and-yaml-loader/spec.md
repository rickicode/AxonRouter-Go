# Task: 01-add-https-config-model-and-yaml-loader

## Feature: 02_settings-https-tls

## Dependencies

_None_

## Plan Section

### 1. Add HTTPS config model and YAML loader
**Depends on**: none
**Files:**
- Create: `internal/config/https.go`
- Test: `internal/config/https_test.go`

**What to do:**
- Define `HTTPSConfig` struct with fields: Enabled, Domain, Email, AcceptTOS, Staging, CertCache.
- Add `IsValid()` method that returns (bool, message).
- Add `LoadHTTPSConfig(dataDir)` and `SaveHTTPSConfig(dataDir, cfg)` using `gopkg.in/yaml.v3`.
- Default CertCache is `certs`.

**Must NOT do:**
- Do not store certificate private key in the YAML file; autocert handles cache files.

**Verify:**
- `go test ./internal/config -run HTTPS -v` → PASS

---
