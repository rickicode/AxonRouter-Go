# Comprehensive Design — Admin API + Token/Cost Tracking Fix

> Status: DRAFT — menunggu approval sebelum implementasi.

---

## 1. Ringkasan Scope

Project ini akan mengerjakan **3 hal sekaligus**:

1. **Admin API** — programmatic API dengan master key.
2. **Fix Token Tracking** — sistem pencatatan token (input/output) saat ini tidak berfungsi dengan baik.
3. **Fix Cost Tracking** — estimasi cost dari `model_pricing` DB.
4. **API Key Token Budget** — batas total token kumulatif per API key.

Semua harus **non-blocking** terhadap stream/response `/v1/*`.

---

## 2. Masalah Saat Ini (Token/Cost Tracking)

### 2.1 Token Extraction Rentan / Salah

File `internal/api/handlers/v1/stream.go`:

- `ExtractTokensFromBody` hanya mendukung format OpenAI `usage.prompt_tokens` / `usage.completion_tokens`.
- Return early kalau `PromptTokens == 0`, padahal bisa jadi cuma `CompletionTokens` yang ada.
- Tidak support format `usage.input_tokens` / `usage.output_tokens` yang sering dipakai translator internal.
- `ExtractTokensFromFinalChunk` punya masalah serupa.

### 2.2 Tidak Ada `api_key_id` di Log

- `internal/usage.LogEntry` tidak punya field `api_key_id`.
- `request_logs` tidak punya kolom `api_key_id`.
- Jadi tidak bisa menghitung usage per API key.

### 2.3 Cost Estimation Tidak Konsisten

- `EstimateCost` dijalankan di tracker hanya kalau `CostUsd == 0`.
- Harga diambil dari cache `model_pricing`, tapi cache mungkin kosong/kadaluarsa.
- Belum ada mekanisme untuk memastikan harga canonical dari `model_pricing` selalu dipakai.

---

## 3. Desain Solusi Token/Cost Tracking

### 3.1 Non-Blocking Guarantee

- Semua logging/cost tracking dilakukan **secara asinkron**.
- `internal/usage.Tracker` sudah pakai channel buffer + flush goroutine — tetap dipakai.
- Extract token log tetap dilakukan di request goroutine, tapi cuma parsing JSON kecil.
- Update `api_key_usage` untuk budget enforcement dilakukan dengan query cepat dan bisa didefer ke async kalau perlu.

### 3.2 Robust Token Extraction

Update `internal/api/handlers/v1/stream.go`:

- `ExtractTokensFromBody` support banyak format:
  - OpenAI: `usage.prompt_tokens` / `usage.completion_tokens`
  - Internal normalized: `usage.input_tokens` / `usage.output_tokens`
  - Claude: `message.usage.input_tokens` / `output_tokens`
  - Gemini: `usageMetadata.promptTokenCount` / `candidatesTokenCount`
- Jangan return early hanya karena `prompt_tokens == 0`.
- Fallback: kalau ada `total_tokens`, gunakan sebagai sum / approximation.

### 3.3 Tambah `api_key_id` ke Log

1. Tambah field `ApiKeyID string` di `internal/usage.LogEntry`.
2. Tambah kolom `api_key_id TEXT` di `request_logs` via migration.
3. Update INSERT di `tracker.go` untuk menyertakan `api_key_id`.
4. Di semua `h.tracker.Log(&usage.LogEntry{...})`, set `ApiKeyID: c.GetString("api_key_id")`.

File yang perlu diubah untuk point 4:

- `internal/api/handlers/v1/chat.go`
- `internal/api/handlers/v1/messages.go`
- `internal/api/handlers/v1/responses.go`
- `internal/api/handlers/v1/embeddings.go`
- `internal/api/handlers/v1/images.go`
- `internal/api/handlers/v1/stt.go`
- `internal/api/handlers/v1/tts.go`
- `internal/api/handlers/v1/video.go`
- `internal/api/handlers/v1/handler.go`

### 3.4 Cost Tracking Fix

1. Pastikan `model_pricing` seed sudah benar (ga ada $0, ga ada duplicate).
2. Pastikan `usage.InitPricing(cfg.DB)` dipanggil di startup (sudah ada di `router.go`).
3. Tambah background periodic reload pricing cache, misal setiap 1 jam.
4. `EstimateCost` harus selalu dipanggil dengan model ID canonical (strip provider prefix).
5. Add `CostUsd` field ke `LogEntry` di call site kalau sudah diketahui (misal dari upstream response); kalau tidak, tracker akan estimasi.

### 3.5 Audit & Debug

- Log di `request_logs` harus selalu punya `input_tokens`, `output_tokens`, `cost_usd`.
- Tambah endpoint admin: `GET /admin/api/v1/logs` untuk verifikasi.

---

## 4. Admin API System

### 4.1 Master API Key

- Disimpan di tabel `settings` dengan key `admin_api_key`.
- Format: `axr_` + 64 karakter hex random.
- Auto-generate saat startup kalau belum ada.
- Bisa di-regenerate via dashboard di halaman Developers.
- Di-load ke memory melalui `internal/adminapi.KeyManager`.

### 4.2 Auth Layers

| Path | Auth |
|------|------|
| `/api/admin/developers/master-key` | JWT session admin |
| `/admin/api/v1/*` | `Authorization: Bearer axr_...` |
| `/api/admin/*` | JWT session admin (tetap) |

### 4.3 Developers Page

- URL: `/developers`
- Tampilkan master key, base URL, tombol regenerate, dokumentasi endpoint, contoh curl.
- Menu di sidebar.

### 4.4 Programmatic Admin API

- Prefix: `/admin/api/v1`
- Response standar REST:
  - List: `{ "data": [...], "meta": { "page", "per_page", "total" } }`
  - Single: `{ "data": {...} }`
  - Error: `{ "error": { "message", "code" } }`
- Endpoint reuse semua admin handler, contoh:
  - `POST /admin/api/v1/api-keys`
  - `GET /admin/api/v1/logs`
  - `GET /admin/api/v1/providers`
  - dst.

---

## 5. API Key Token Budget (max_tokens)

### 5.1 Makna

`max_tokens` = total token budget kumulatif / lifetime per API key.

### 5.2 Schema

```sql
ALTER TABLE api_keys ADD COLUMN max_tokens INTEGER DEFAULT 0; -- 0 = unlimited

CREATE TABLE IF NOT EXISTS api_key_usage (
  api_key_id TEXT PRIMARY KEY REFERENCES api_keys(id),
  total_tokens INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);

ALTER TABLE request_logs ADD COLUMN api_key_id TEXT;
```

### 5.3 Enforcement

Sebelum request diteruskan:

1. Ambil `api_key_id` dan `max_tokens`.
2. Kalau `max_tokens > 0`:
   - Baca `total_tokens` dari `api_key_usage`.
   - Kalau `total_tokens >= max_tokens` → reject `429`.
   - Kalau request punya `max_tokens` dan `total_tokens + max_tokens > limit` → reject `400`.
3. Setelah response selesai, update `api_key_usage` dengan token aktual.
4. Simpan `api_key_id` ke `request_logs`.

### 5.4 Perilaku Error

- Request gagal di upstream → token tidak dihitung.
- Budget lifetime, tidak auto-reset.

---

## 6. Implementasi Plan

### Phase 1: Fix Token/Cost Tracking

1. Update `ExtractTokensFromBody` & `ExtractTokensFromFinalChunk` → robust multi-format.
2. Tambah `ApiKeyID` ke `LogEntry`, `request_logs`, dan semua call site.
3. Fix `EstimateCost` & pastikan pricing cache reload periodically.
4. Verifikasi via `GET /api/admin/logs` setelah request test.

### Phase 2: Master API Key + Admin API

1. Buat `internal/adminapi.KeyManager`.
2. Buat middleware `master_auth.go`.
3. Buat handler `developers.go`.
4. Mount `/admin/api/v1/*` dan `/api/admin/developers/*`.
5. Buat halaman `Developers.svelte`.

### Phase 3: API Key Token Budget

1. Migration DB: `max_tokens`, `api_key_usage`, `api_key_id`.
2. Update `APIKeyHandler` untuk handle `max_tokens`.
3. Enforce `max_tokens` di proxy handler.
4. Update UI API Keys.

### Phase 4: Verification

1. `go build ./...`
2. `go test ./...`
3. Smoke test master key + create API key + get logs.
4. Smoke test `/v1/chat/completions` dan cek token/cost tercatat.
5. Smoke test `max_tokens` budget.

---

## 7. File yang Akan Diubah

### Backend

| File | Perubahan |
|------|-----------|
| `internal/api/handlers/v1/stream.go` | Robust token extraction |
| `internal/usage/tracker.go` | Tambah `ApiKeyID`, update INSERT |
| `internal/usage/pricing.go` | Periodic reload, fallback fix |
| `internal/db/migrations.go` | Tambah kolom/tabel |
| `internal/api/handlers/v1/*.go` | Set `ApiKeyID` di LogEntry |
| `internal/api/handlers/v1/handler.go` | Enforce `max_tokens` |
| `internal/adminapi/key_manager.go` | Baru |
| `internal/api/middleware/master_auth.go` | Baru |
| `internal/api/handlers/admin/developers.go` | Baru |
| `internal/api/handlers/admin/apikeys.go` | Handle `max_tokens` |
| `internal/api/router.go` | Mount routes |

### Frontend

| File | Perubahan |
|------|-----------|
| `web/src/pages/Developers.svelte` | Baru |
| `web/src/pages/APIKeys.svelte` | Field `max_tokens` |
| `web/src/App.svelte` | Route `/developers` |
| `web/src/lib/components/sidebar/SidebarNav.svelte` | Menu |
| `web/src/lib/api.ts` | API client |

---

## 8. Keputusan Default yang Perlu Disetujui

| No | Topik | Default |
|----|-------|---------|
| 1 | Master key storage | DB (`settings`), plaintext |
| 2 | Master key format | `axr_` + 64 hex |
| 3 | Auto-generate master key | Ya |
| 4 | Admin API response | Standard REST `{ data, meta }` |
| 5 | Exposed endpoints | Semua admin endpoint |
| 6 | `max_tokens` | Total budget kumulatif, lifetime |
| 7 | Budget reset | Tidak ada |
| 8 | Count token saat upstream error | Tidak dihitung |
| 9 | Non-blocking guarantee | Async tracker, fast pre-check |
| 10 | Pricing reload | Setiap 1 jam |

---

## 9. Risiko & Mitigasi

| Risiko | Mitigasi |
|--------|----------|
| Token extraction breaking existing providers | Test semua translator, support multi-format |
| Master key plaintext di DB | DB access harus diamankan |
| `api_key_usage` query lambat | Index by `api_key_id`, keep small table |
| Budget enforcement mengganggu stream | Pre-check cepat sebelum stream dimulai |
| Cost mismatch | Gunakan canonical `model_pricing`, reload periodic |

---

*Dokumen ini akan di-update saat implementasi berjalan.*
