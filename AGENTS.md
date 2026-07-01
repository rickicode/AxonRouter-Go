# AGENTS.md — AxonRouter-Go Project Rules

## Anti-Hallucination Rule (CRITICAL)

**DILARANG NGARANG.** Setiap claim tentang provider capabilities, API endpoints, fitur, atau behavior HARUS diverifikasi dari codebase reference:

1. **SELALU cek codebase** — `/workspaces/AxonRouter`, `/workspaces/CLIProxyAPI`, `/workspaces/OmniRoute`
2. **SELALU cek internet** — kalau informasi tidak ada di codebase, search web
3. **JANGAN pernah assume** — kalau tidak tau, bilang tidak tau, jangan ngarang
4. **JANGAN halusinasi** — kalau tidak ada bukti di code, jangan claim itu ada
5. **Quote source** — setiap claim harus ada file reference atau URL

### Contoh Salah:
```
❌ "Mimo punya balance tracking" (ngarang, tidak ada di codebase)
❌ "Codex quota reset setiap minggu" (belum verify dari code)
```

### Contoh Benar:
```
✅ "OmniRoute quotaCache.ts line 15: background refresh setiap 1 menit" (ada di code)
✅ "CLIProxyAPI codex/openai_auth.go: AuthURL = 'https://auth.openai.com/oauth/authorize'" (ada di code)
```

### Saat tidak tau:
```
✅ "Saya tidak tau apakah Mimo punya quota endpoint. Tidak ada referensi di codebase yang tersedia."
```

## Multi-Codebase Comparison Rule (CRITICAL)

Ketika CLIProxyAPI, AxonRouter, dan OmniRoute punya implementasi berbeda untuk sistem yang sama:

1. **Baca SEMUA versi** dari ketiga codebase
2. **Bandingkan** — mana yang paling efisien, paling lengkap, paling stabil
3. **Pilih yang terbaik** — ambil versi terbaik, bukan campur-caduk
4. **Quote source** — tulis dari mana versi yang dipilih dan kenapa

### Contoh:
```
✅ "Quota detection: pakai versi OmniRoute (getUsageForProvider) karena paling lengkap — support 8 provider.
   AxonRouter hanya detect dari error text. CLIProxyAPI tidak ada quota system."

✅ "Circuit breaker: pakai versi AxonRouter karena state machine paling jelas (CLOSED→OPEN→HALF_OPEN)
   dengan configurable threshold. OmniRoute mirip tapi kurang fleksibel."

✅ "Translator: pakai versi CLIProxyAPI karena sudah dalam Go, production-tested, 664 files.
   AxonRouter lebih lengkap tapi dalam TypeScript."
```

### Jangan:
```
❌ Campur-caduk dari beberapa versi tanpa alasan
❌ Pilih versi yang kurang bagus karena "sudah ada"
❌ Ignore versi yang lebih baik karena "terlalu ribet"
```

## Reference Codebases

| Codebase | Path | Bahasa | Kelebihan |
|----------|------|--------|-----------|
| **CLIProxyAPI** | `/workspaces/CLIProxyAPI` | Go | Translator, auth, executor (production-tested) |
| **AxonRouter** | `/workspaces/AxonRouter` | TypeScript | Combo system, dashboard, usage tracking |
| **OmniRoute** | `/workspaces/OmniRoute` | TypeScript | 231 providers, quota cache, policy engine |

## Tech Stack (Fixed)

- Backend: Go + Gin + SQLite
- Frontend: Svelte (embedded via `go:embed`)
- CLI: Minimal — service management + status only
- Config: SQLite (bukan YAML/file-based)

## Provider Naming

- Prefix = provider identifier: `cx/`, `openai/`, `mimo/`, `ag/`, `kiro/`, etc.
- Codex ≠ OpenAI: `cx/gpt-5.4` ≠ `openai/gpt-5.4`
- Custom provider: nama user-given jadi prefix (e.g., `9router/gpt-4o`)

## Scale Assumptions

- 100-1000+ connections per provider
- Routing harus <1ms regardless of connection count
- Pre-computed eligible list (O(1) routing)
- Dashboard wajib pagination

## Execution & Build Rules

1. **Selalu Commit:** Selalu commit hasil kerja ke git repo jika dirasa sudah cukup, semua sudah stabil, dan tidak ada error lagi.
2. **Nol Warning:** Jika saat menjalankan `npm run build` ada warning, maka warning tersebut WAJIB diperbaiki/difiksasi.

## UI/UX Toast Notifications (Wajib)

Semua aksi user yang memicu response dari backend WAJIB menggunakan toast notification (`svelte-sonner`). JANGAN pakai `alert()` atau silent update tanpa feedback.

### Library
- Pakai `svelte-sonner` (sudah terinstall)
- Import: `import { toast } from 'svelte-sonner'`
- `<Toaster />` sudah ada di `App.svelte`

### Pola Wajib
```typescript
// Success action
toast.success('Connection reset to ready');

// Error action
toast.error('Test failed: ' + err.message);

// Info/loading
toast.info('Syncing models...');

// Dengan detail
toast.success(`${model} OK (${latency}ms)`);
toast.error(`Test all: ${ok} passed, ${failed} failed`);
```

### Rules
1. **Setiap API call** yang dipicu user action WAJIB ada toast response (success/error)
2. **JANGAN pakai `alert()`** — gunakan `toast.error()` atau `toast.success()`
3. **JANGAN silent fail** — kalau error, user harus tau via toast
4. **Format toast**: singkat, actionable, include context (nama model, nama connection, jumlah)
5. **Test/Test All**: toast harus show jumlah passed/failed/skipped
6. **Model test**: toast per model show `modelName OK (Xms)` atau `modelName failed: reason`
7. **Delete/Reset**: toast konfirmasi aksi berhasil
8. **Bulk operations**: satu toast summary, bukan satu toast per item

## Page Layout Convention (Wajib Konsisten)

Semua dashboard pages HARUS menggunakan layout pattern yang sama. JANGAN buat `max-w-[Npx]` atau `w-full` di outer wrapper — biarkan flex-1 fill parent.

### Outer wrapper (wajib):
```svelte
<div class="flex flex-1 flex-col gap-6 p-6">
```

### Heading pattern:
```svelte
<div class="space-y-1">
  <h1 class="text-display-lg">Page Title.</h1>
  <p class="text-body-sm text-muted-foreground">Description text</p>
</div>
```

### Card surfaces:
- `bg-card` (`#18181b`) untuk card backgrounds
- `shadow-card` / `shadow-elevated` untuk elevation
- `rounded-xl` (12px) untuk card radius
- `border-border` untuk card borders
- JANGAN pakai raw hex colors (`bg-[#18181b]`) — gunakan Tailwind tokens

### Typography tokens (dari DESIGN.md):
- `text-display-lg` — page headings (32px, 600, -1.28px tracking)
- `text-display-md` — section headings (24px, 600)
- `text-body-sm` — body text (14px, 400)
- `text-body-sm-strong` — bold body (14px, 500)
- `text-caption` — small labels (12px, 400)
- `text-caption-mono` — mono labels (12px, mono)

### Buttons:
- `<Button variant="outline" size="sm" class="text-body-sm rounded-sm cursor-pointer">`
- JANGAN bikin custom button styling — pakai Button component

### Reference pages:
- `Providers.svelte` — gold standard layout
- `Combos.svelte` — card grid pattern
- `Logs.svelte` — table + filters pattern
