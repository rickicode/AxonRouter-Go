# Analisis & Perbaikan Bug Sistem `/v1/`

## Ringkasan
Laporan analisis bug `/v1/` telah diimplementasikan. Perubahan sudah di-merge ke `master`, semua test `go test ./... -count=1` berhasil, dan `go build ./...` clean.

## Bug yang Diperbaiki

### 1. `/v1/responses` memanggil `writeUpstreamClientError` sebelum failover
- **File**: `internal/api/handlers/v1/responses.go:128`
- **Hasil**: Error client upstream (4xx) langsung dipassthrough ke pemanggil, tidak lagi menandai connection sebagai `auth_failed`/`model_not_found` atau memicu auto-disable.
- **Test**: `responses_test.go` regression test.

### 2. `checkTokenBudget` diterapkan di semua endpoint non-chat
- **File**: `responses.go`, `embeddings.go`, `images.go`, `tts.go`, `stt.go`, `video.go`, `unified.go`
- **Hasil**: API key dengan `max_tokens` budget sekarang tidak bisa dibypass lewat embeddings, images, audio, video, responses, atau unified.
- **Test**: `token_budget_test.go` mencakup 7 endpoint.

### 3. Cloudflare model discovery di-cache 5 menit
- **File**: `internal/models/catalog.go`, `internal/api/handlers/v1/models.go`
- **Hasil**: `GET /v1/models` tidak lagi memicu HTTP request ke Cloudflare setiap pemanggilan.
- **Test**: `catalog_test.go`.
- **Review follow-up**: timestamp cache diperbarih hanya setelah fetch sukses, agar kegagalan transient tidak menekan retry selama TTL.

### 4. Auth cache diperkuat
- **File**: `internal/api/middleware/auth_cache.go`, `internal/api/middleware/auth.go`
- **Hasil**:
  - `AuthCache.Validate` sekarang menyimpan hasil sendiri di dalam singleflight path.
  - `validateKey` mengembalikan error DB secara eksplisit; `Auth` langsung fail-closed 500 tanpa query ulang.
  - Log error DB tidak lagi duplikat.
  - `AuthCache.Get` memperbaiki TOCTOU saat menghapus entry expired.
- **Test**: `auth_test.go` diperbarui.

## Verifikasi
- `go test ./... -count=1` → **PASSED**
- `go build ./...` → **CLEAN**
- `go vet ./...` → **CLEAN**

## Catatan Reviewer
Reviewer awal menemukan dua masalah:
1. CF cache timestamp di-update sebelum fetch sukses → **sudah diperbaiki**.
2. Log error DB ganda di auth → **sudah diperbaiki** dengan membedakan DB error dari "key tidak ditemukan/table kosong".

## Status
- Semua task plan selesai.
- Fitur siap di-mark complete.

