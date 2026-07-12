# TODOS.md — AxonRouter-GO

## OpenCode Free — Per-Model Exhaustion

### Problem
Sistem exhaustion saat ini per-koneksi. Tapi OpenCode Free rate limit-nya **per-model**.
Ketika `hy3-free` kena 429, seluruh koneksi `oc` di-mark exhausted — padahal model lain masih jalan.

### Bukti
```
hy3-free               → 429 (limit)
deepseek-v4-flash-free → 200 (OK, IP sama)
mimo-v2.5-free         → 200 (OK, IP sama)
nemotron-3-ultra-free  → 200 (OK, IP sama)
north-mini-code-free   → 200 (OK, IP sama)
```

### Yang Perlu Diubah

- [ ] Exhaustion cache per-model key untuk oc (`connID + model`, bukan cuma `connID`)
- [ ] Saat oc dapat 429 → mark model itu exhausted, koneksi tetap ready
- [ ] RateLimitProber — probe dengan model yang sama yang kena limit
- [ ] Routing logic — `hy3-free` exhausted → coba model lain di koneksi yang sama
- [ ] Semua model oc exhausted → baru fail ke provider lain (cx/cf/ag)

### Referensi
- Exhaustion cache: `internal/quota/exhaustion_cache.go`
- Failover logic: `internal/api/handlers/v1/handler.go` → `handleFailoverError`
- RateLimitProber: `internal/background/rate_limit_prober.go`
- Eligibility: `internal/connstate/eligibility.go`

---

## RateLimitProber — Background Auto-Recovery

### Status: ✅ Sudah dibuat, belum deploy

- [ ] Rebuild binary dengan `make build`
- [ ] Restart server 3777
- [ ] Test: kena 429 → cooldown 5 menit → prober reset status ke ready

### File
- `internal/background/rate_limit_prober.go`
- Wire: `internal/api/router.go`
