# Frontend Audit — AxonRouter-GO Dashboard

> Tanggal: 2026-07-12  
> Scope: `web/src/pages/*.svelte` + komponen pendukung (`web/src/lib/components`).  
> Metode: `grep`, `read`, `npm run build`.

## 1. Daftar Halaman

| File | Rute | Keterangan singkat |
|------|------|--------------------|
| `Dashboard.svelte` | `/` | Statistik request/token/cost + ChartBar |
| `Providers.svelte` | `/providers` | Katalog provider + status ringkasan |
| `ProviderAdd.svelte` | `/providers` (flow) | Pilih & konfigurasi provider baru |
| `ProviderDetail.svelte` | `/providers/:id` | Detail provider, list connections, model tests |
| `ConnectionDetail.svelte` | `/providers/:id/:connId` | Detail single connection + proxy assignment |
| `Combos.svelte` | `/combos` | List routing combos |
| `ComboDetail.svelte` | `/combos/:id` | Edit combo |
| `ProxyPools.svelte` | `/proxy-pools` | Pools, groups, assignments, deploy |
| `ProxyPoolDetail.svelte` | `/proxy-pools/:id` | Detail + edit pool |
| `APIKeys.svelte` | `/api-keys` | Proxy API key management |
| `Logs.svelte` | `/logs` | Request logs + active requests octopus |
| `Quota.svelte` | `/quota` | Quota cache cards per connection |
| `ModelPricing.svelte` | `/model-pricing` (rute tidak terdaftar di `App.svelte`!) | CRUD harga per model |
| `Optimization.svelte` | `/optimization` | Compression + cache settings |
| `Settings.svelte` | `/settings` | Runtime settings |
| `CLITools.svelte` | `/cli-tools` | Snippet CLI generator |
| `Login.svelte` | `/` (unauthenticated) | Login screen |
| `NotFound.svelte` | fallback | 404 |

### Catatan rute
- `ModelPricing.svelte` **tidak dipasang di router** `App.svelte`. Halaman itu ada di source tapi tidak ada `matchRoute()` untuk `/model-pricing` (baris 45–112 `App.svelte`). Ini kemungkinan tombol/link ke sana akan menjadi 404 meski file-nya ada.

## 2. Verdict Umum: Konsisten? Responsif? Bebas Bug?

| Aspek | Verdict |
|-------|---------|
| Layout dasar | Mayoritas halaman pakai `<div class="flex flex-1 flex-col gap-6 p-6">`. Cukup konsisten, tapi ada variasi padding (`p-4 md:p-6` di `Providers.svelte`, `w-full` nggak perlu di `Logs.svelte`). |
| Tipografi heading | Hampir semua pakai `text-display-lg` kecuali `ModelPricing.svelte` pakai `text-title-md` — kelas itu **tidak didefinisikan**, jadi visual-nya jatuh ke default. |
| Komponen shadcn/ui | Card, Button, Input, Label, Dialog, Select dipakai di banyak tempat. Tapi `<table>` umumnya manual, `Tabs` hampir tidak dipakai, dan beberapa halaman masih pakai `<select>` native. |
| Toast (`svelte-sonner`) | Semua halaman backend-action sudah pakai toast, **kecuali** `ProviderAdd.svelte` dan `AddProviderModal.svelte`. `App.svelte` malah pakai `toast` tanpa import. |
| Responsiveness | Secara struktur responsif (grid + `overflow-x-auto`). Ada beberapa fixed width (`w-64`, `w-[180px]`, `max-w-[200px]`) yang bisa pecah di layar kecil. |
| Build warning | `npm run build` di `web/` berhasil tanpa warning. |
| Bug runtime | **Ada bug nyata**: `App.svelte:108` memanggil `toast.success()` tanpa import `toast`. Logout bakal crash/undefined. |

**Kesimpulan:** Belum sepenuhnya konsisten. Semua halaman fungsional, tapi ada pola-pola duplikasi, komponen yang tidak dipakai, dan satu bug runtime.

## 3. Bug & Potensi Bug

### 3.1 Bug runtime

| File | Baris | Masalah | Dampak |
|------|-------|---------|--------|
| `web/src/App.svelte` | 108 | `toast.success('Signed out')` dipanggil tanpa `import { toast } from 'svelte-sonner'` | Saat logout, `toast` adalah `undefined` → error runtime. |
| `web/src/pages/ModelPricing.svelte` | 131 | Kelas `text-title-md` tidak ada di `app.css` | Heading tidak mendapat ukuran yang dimaksud; tampil seperti teks biasa. |

### 3.2 Pola yang menyimpang dari konvensi proyek

| Konvensi (AGENTS.md) | Pelanggaran |
|----------------------|-------------|
| Tombol: `<Button … class="text-body-sm rounded-sm cursor-pointer">` | Banyak tombol pakai `text-button-md rounded-pill px-5` (selalu kapsul) atau tidak pakai `cursor-pointer`. Contoh: `Combos.svelte:123`, `ProxyPools.svelte:378`, `ProviderAdd.svelte:273`. |
| Impor lucide harus subpath (`@lucide/svelte/icons/xxx`) | 4 file masih impor dari root `@lucide/svelte`: `CLITools.svelte:12`, `Logs.svelte:14`, `Quota.svelte:22-23`, `SidebarNav.svelte:14-15`. |
| Jangan pakai `alert()`/`confirm()` | `ConnectionDetail.svelte:105` dan `ProxyPoolDetail.svelte:98` masih pakai `confirm()` untuk delete. |
| Gunakan `svelte-sonner` untuk setiap backend response | `ProviderAdd.svelte` tidak import toast; `AddProviderModal.svelte` juga tidak (hanya inline `error`). |
| Jangan pakai raw hex/arbitrary class menyimpang | `Providers.svelte:218` (`bg-[radial-gradient(...rgba...)]`), `Quota.svelte` raw `#4285f4` untuk provider dot, `NotFound.svelte` gradient raw hex di `<style>`. |

### 3.3 Komponen yang tidak efisien/tidak terpakai

| File | Status | Masalah |
|------|--------|---------|
| `web/src/lib/components/DataTable.svelte` | **Tidak pernah di-import** | Dibuat tapi tidak dipakai; semua tabel diimplementasi manual. |
| `AddProviderModal.svelte` | Hanya dipakai lewat `Providers.svelte` | Tidak pakai toast, padahal action-nya mutasi backend. |
| `ModelPickerDialog.svelte` | Dipakai di `CLITools.svelte` | Impor lucide dari root `@lucide/svelte`. |

### 3.4 Isu responsivitas yang ditemukan

| File | Masalah | Saran |
|------|---------|-------|
| `ProviderDetail.svelte` | `Select.Trigger class="w-[180px]"` (baris 237) dan beberapa `max-w-[180px]` | Di layar kecil potensi overflow. Gunakan `w-full sm:w-[180px]`. |
| `ConnectionDetail.svelte` | Input rename `w-64` (baris 162), select proxy `w-full` tapi container fixed | Cukup, tapi input rename sebaiknya `w-full sm:w-64`. |
| `ProxyPoolDetail.svelte` | Edit name input `w-64` (baris 154) | Sama seperti di atas. |
| `Combos.svelte` / `ProxyPools.svelte` | `max-w-[200px]` / `max-w-[160px]` pada link/text di dalam tabel | Pada layar sempit ini memaksakan lebar minimum; sebaiknya gunakan `min-w-0` + `truncate` tanpa max arbitrary. |
| `NotFound.svelte` | `text-[120px] md:text-[180px]` | Memang responsive, tapi arbitrary class; bisa diganti dengan `text-[clamp(...)]` atau ukuran token yang lebih aman. |
| `CLITools.svelte` | Detail dialog `sm:max-w-2xl max-h-[90vh]` (baris 283) + step number `size-6` | Cukup responsif, tapi beberapa badge pakai `text-[11px]`. |

## 4. Inefisiensi & Duplikasi yang Bisa Dibuat Lebih Baik

### 4.1 Status/State badge duplikat
Polosan badge status muncul di hampir setiap halaman tabel/card:
- `Combos.svelte:147-157` & `173-176`
- `ProxyPools.svelte:431-434`, `439-452`, `517-521`
- `ProviderDetail.svelte:298-309`

Warna, font 10px, border, pill sama-sama di-hardcode. **Rekomendasi:** buat komponen `<StatusBadge status active />` yang menerima variant dan menghasilkan badge konsisten.

### 4.2 Tabel manual
Semua tabel (`APIKeys`, `ModelPricing`, `Combos`, `ProxyPools`, `Logs`) dibuat manual padahal `DataTable.svelte` sudah ada. Dua kemungkinan:
- Jika `DataTable.svelte` terlalu sederhana, **perluas DataTable** supaya mendukung `cell` snippet dan aksi.
- Atau hapus `DataTable.svelte` kalau memang tidak mau dipakai.

Saat ini file `DataTable.svelte` hanya menambah maintenance tanpa nilai.

### 4.3 Tab switcher manual
`Optimization.svelte` dan `ProxyPools.svelte` membuat tab sendiri:
```svelte
<div class="inline-flex w-fit items-center gap-1 rounded-lg bg-muted p-1">
  <button …>Compression</button>
  <button …>Cache</button>
</div>
```
Padahal shadcn/ui `Tabs` sudah tersedia di `web/src/lib/components/ui/tabs`. Pakai `Tabs` bawaan supaya keyboard navigation dan styling konsisten.

### 4.4 Inline SVG untuk icon panah kembali
`ProviderDetail.svelte:169` dan `ProviderAdd.svelte:195` memakai inline SVG manual. Seharusnya:
```svelte
import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
```

### 4.5 `<select>` native vs `<Select>` shadcn
`ConnectionDetail.svelte:264-280`, `ProviderAdd.svelte:247-250`, `CLITools.svelte:347/439/469` pakai `<select>` HTML biasa. UI-nya tidak konsisten dengan Select component yang dipakai di tempat lain.

### 4.6 `ProxyPools.svelte` terlalu monolitik
File ini 832+ baris, menangani pools, groups, assignments, deploy, import bulk, dan banyak dialog. Sangat rentan terhadap regressi. Split menjadi:
- `ProxyPoolList.svelte`
- `ProxyGroupList.svelte`
- `ProxyAssignments.svelte`
- `ProxyDeploy.svelte`
- Dialog/dialog create pool/group tetap di `ProxyPools.svelte` atau dipisah ke subkomponen.

### 4.7 Ikon sidebar duplikat
`SidebarNav.svelte` memberi icon `Layers` untuk **Combos** dan **Optimization**. Pengguna bisa bingung karena dua menu berbeda pakai ikon sama. Optimization sebaiknya ikon `Zap`/`Sparkles`.

## 5. Rekomendasi Per Prioritas

### 🔴 High (bug/runtime)
1. `App.svelte`: tambahkan `import { toast } from 'svelte-sonner'`.
2. `ModelPricing.svelte`: ganti `text-title-md` dengan `text-display-lg`.
3. `ModelPricing.svelte`: daftarkan rute `/model-pricing` di `App.svelte::matchRoute()` — atau hapus file jika memang tidak dipakai.
4. `ConnectionDetail.svelte` & `ProxyPoolDetail.svelte`: ganti `confirm()` dengan `<AlertDialog>` shadcn.
5. `ProviderAdd.svelte` & `AddProviderModal.svelte`: tambahkan `toast` untuk success/error backend.

### 🟡 Medium (konsistensi & komponen)
6. Buat `<StatusBadge>` reusable; ganti semua inline status pill di `Combos`, `ProxyPools`, `ProviderDetail`.
7. Putuskan nasib `DataTable.svelte`: hapus atau pakai dan perluas.
8. Ganti tab manual di `Optimization.svelte` & `ProxyPools.svelte` dengan komponen `Tabs` shadcn.
9. Ganti `<select>` native dengan `<Select.Root>` di `ConnectionDetail`, `ProviderAdd`, `CLITools`.
10. Standardisasi semua tombol: `class="text-body-sm rounded-sm cursor-pointer"` sesuai AGENTS.md; hindari `rounded-pill` kcuali memang desain khusus.

### 🟢 Low (DX & estetika)
11. Perbaiki impor lucide ke subpath di `CLITools`, `Logs`, `Quota`, `SidebarNav`, `ModelPickerDialog`.
12. Ganti inline SVG back arrow dengan `ArrowLeftIcon`.
13. Hapus/hindari arbitrary class seperti `bg-[radial-gradient(...)]`, raw hex di `<style>`, `text-[10px]`/`text-[11px]` untuk badge.
14. `ProxyPools.svelte`: refactor menjadi subkomponen untuk maintainability.
15. `SidebarNav.svelte`: icon unik untuk menu Optimization.
16. Penyeragaman padding wrapper: `p-6` saja (atau `p-4 md:p-6` di semua halaman jika memang ingin lebih padat di mobile).

## 6. Bottom Line

**Tidak semua halaman 100% konsisten**, tapi tidak ada kehancuran besar. Halaman-halaman berjalan, build bersih, dan layout responsive-nya sudah pada tempatnya.

Yang paling penting diperbaiki dulu:
1. **Bug logout** di `App.svelte`.
2. **ModelPricing yang tidak di-route-kan** dan heading class yang undefined.
3. **Missing toast** di flow add provider.
4. **Blocking `confirm()`** di dua halaman delete.

Setelah itu, fokus deduplikasi badge/tabel/select akan memberikan konsistensi visual yang jauh lebih rapi dan mudah dirawat.
