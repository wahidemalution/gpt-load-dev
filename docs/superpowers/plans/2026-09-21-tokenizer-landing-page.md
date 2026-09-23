# Tokenizer Landing Page (PAYG AI Frontier Indonesia) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ganti landing page GPT-Load menjadi landing "Tokenizer" — layanan PAYG (pay-as-you-go) AI Frontier untuk pasar Indonesia, dengan navbar landing yang independent dari layout login/dashboard, section Models live dari API publik, dan stat card Total Request + Total Token (all time).

**Architecture:**
- Backend: satu endpoint publik tanpa auth `GET /api/public/landing` di package `internal/control` (file baru `landing_public.go`), mengembalikan daftar model (dari `ListProjectModels` dengan `GroupStatus=enabled`, di-deduplikasi per `client_model`) dan agregat all-time (`QueryUsage` dari `FromMS=0` sampai `now`). Hasil di-cache in-memory 60 detik di struct `Server`.
- Frontend modern (Vue 3): `LandingLayout.vue` baru dengan navbar khusus landing; `LandingView.vue` ditulis ulang dengan konten Bahasa Indonesia hardcode (tanpa i18n keys); route `modern-landing` di App.vue dibungkus `LandingLayout`, route lain tidak berubah.
- Login tetap memakai `PublicLayout`. Dashboard/auth/classic frontend tidak disentuh.

**Tech Stack:** Go 1.x + Gin (backend), Vue 3 + TypeScript + vue-i18n + vite (frontend `web/`), pnpm.

## Global Constraints

- Konten landing Bahasa Indonesia, hardcode di komponen — TIDAK menambah/mengubah locale `zh-CN`/`en-US`/`ja-JP`.
- Jangan mengubah route/handler ber-auth yang ada; endpoint baru harus `AuthNone` / tidak tercakup middleware auth control.
- Gaya visual mengikuti design tokens `--modern-*` yang sudah ada (lihat `LandingView.vue` lama dan `PublicLayout.vue`).
- Tidak ada dependency npm baru.
- Judul dokumen tetap memakai brand: ubah suffix `GPT-Load` di `App.vue` menjadi `Tokenizer`.
- Response envelope: `{ code: 0, message: "...", data: {...} }` via `response.SuccessI18n`.

---

### Task 1: Endpoint publik `GET /api/public/landing`

**Files:**
- Create: `internal/control/landing_public.go`
- Modify: `internal/control/http_routes.go` (tambah 1 route di slice `Routes`, setelah `control.home.statistics` sekitar baris 171)
- Test: `internal/control/landing_public_test.go`

**Interfaces:**
- Consumes:
  - `s.service.ListProjectModels(ctx, ProjectModelListQuery{GroupStatus: ProjectModelGroupStatusEnabled, PricingStatus: ProjectModelPricingStatusAll, Page: 1, PageSize: 100})` → `ProjectModelListResponse{Items: []ProjectModelDTO{ClientModel string, ...}}`
  - `s.service.QueryUsage(ctx, requestlog.UsageQuery{FromMS: 0, ToMS: <nowMS>})` → `requestlog.UsageReport{Summary: requestlog.UsageAggregate{RequestCount, UncachedInputTokens, CacheReadTokens, CacheWrite5MTokens, CacheWrite1HTokens, CacheWriteUnknownTokens, OutputTokens, ...}}`
  - `mapUsageAggregate(source requestlog.UsageAggregate) (usageAggregateResponse, error)` — sudah ada di `internal/control/usage.go:523`, field `TotalTokens` & `RequestCount`.
  - `safeEpochMilliseconds(s.now())`, `writeServiceError(c, op, err)`, `response.SuccessI18n(c, "common.success", result)`, `maxSafeInteger` — semua sudah ada di package `control`.
  - `s.now func() time.Time` — field `Server` yang sudah ada.
- Produces:
  - `GET /api/public/landing` (tanpa auth) → data:
    ```json
    {
      "models": [{"name": "gpt-5"}],
      "stats": {"total_requests": 123, "total_tokens": 456789}
    }
    ```
  - Response Go types (dipakai test):
    ```go
    type landingPublicModelResponse struct {
        Name string `json:"name"`
    }
    type landingPublicStatsResponse struct {
        TotalRequests int64 `json:"total_requests"`
        TotalTokens   int64 `json:"total_tokens"`
    }
    type landingPublicResponse struct {
        Models []landingPublicModelResponse `json:"models"`
        Stats  landingPublicStatsResponse   `json:"stats"`
    }
    ```

**Catatan arsitektur route:** `HTTPModule()` di `internal/control/http_routes.go` memasang `Authenticate: s.authenticate()` untuk prefix `/api`. Cek dulu apakah ada mekanisme route publik dalam module control (grep `AuthNone` di `internal/platform/httproute/registry.go` untuk melihat apakah route-level bisa override auth). Jika module-level auth tidak bisa di-skip per-route, tambahkan route ini sebagai **module terpisah** di file `landing_public.go`:

```go
func (s *Server) LandingPublicHTTPModule() httproute.Module {
    return httproute.Module{
        Name:   "landing-public",
        Owner:  httproute.OwnerWeb,
        Auth:   httproute.AuthNone,
        Prefix: "/api/public",
        Routes: []httproute.Route{
            {
                Name:    "landing.public",
                Methods: []string{http.MethodGet},
                Path:    "/landing",
                Handlers: gin.HandlersChain{s.handleLandingPublic},
            },
        },
    }
}
```

dan daftarkan di `internal/container/container.go` (sekitar baris 329-332) sebagai `controlServer.LandingPublicHTTPModule()`. **Pilih pendekatan ini (module terpisah)** — sudah ada preseden `webui.HTTPModule()` dengan `AuthNone`, dan tidak menyentuh middleware auth control sama sekali.

- [ ] **Step 1: Verifikasi kontrak route publik**

Baca `internal/platform/httproute/registry.go` sekitar baris 690-720 untuk memastikan module dengan `Auth: AuthNone` dan tanpa `Authenticate` valid, dan bahwa prefix `/api/public` tidak konflik dengan `NamespacePrefixes: []string{"/api"}` milik module control (periksa logika konflik prefix di registry — jika registry menolak prefix `/api/public` karena tumpang tindih dengan `/api`, dokumentasikan temuan dan gunakan pendekatan lain: tambahkan route di dalam module control tetapi short-circuit di `s.authenticate()` untuk path `/api/public/landing`).

- [ ] **Step 2: Tulis test gagal** — `internal/control/landing_public_test.go`

Ikuti pola test yang ada (lihat `internal/control/client_model_profile_test.go` untuk fixture `NewServer(&config.Config{AuthKey: authTestKey}, fixture.service)` + `RegisterRoutes`). Karena endpoint publik diregistrasi via module terpisah, test harus meregistrasi module tersebut ke gin engine test (lihat bagaimana `HTTPModule()` diregistrasi di `internal/container/container.go` ~baris 325-335 dan tiru di test, atau panggil handler langsung jika fixture terlalu berat — pilih yang paling sederhana sesuai pola test existing).

Test harus memverifikasi (tanpa header Authorization):
1. Status 200 dan envelope sukses.
2. `data.models` berisi dedup `client_model` dari models yang group-nya enabled (seed fixture dengan 2 group, satu enabled satu disabled, model duplikat antar group).
3. `data.stats.total_requests` dan `data.total_tokens` sesuai agregat usage yang di-seed (seed `usage_stats`/`request_logs` sesuai pola fixture test `home_statistics_test.go`).
4. Cache: panggil kedua dalam <60 detik tidak meng-query ulang (cukup assert respons identik; jangan over-engineer — jika sulit di-assert, skip poin 4 dan catat).

```go
// kerangka — sesuaikan dengan fixture existing
func TestLandingPublicReturnsModelsAndAllTimeStatsWithoutAuth(t *testing.T) {
    engine := newLandingPublicTestEngine(t) // helper lokal mengikuti pola fixture test
    req := httptest.NewRequest(http.MethodGet, "/api/public/landing", nil)
    rec := httptest.NewRecorder()
    engine.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200", rec.Code)
    }
    // decode envelope { data: landingPublicResponse }, assert models & stats
}
```

- [ ] **Step 3: Jalankan test, pastikan gagal**

Run: `go test ./internal/control/ -run TestLandingPublic -v`
Expected: FAIL (route 404 / handler tidak ada).

- [ ] **Step 4: Implementasi** — `internal/control/landing_public.go`

```go
package control

import (
    "net/http"
    "sync"
    "time"

    "github.com/gin-gonic/gin"

    "gpt-load/internal/platform/httproute"
    "gpt-load/internal/platform/response"
    "gpt-load/internal/requestlog"
)

const landingPublicCacheTTL = 60 * time.Second

type landingPublicModelResponse struct {
    Name string `json:"name"`
}

type landingPublicStatsResponse struct {
    TotalRequests int64 `json:"total_requests"`
    TotalTokens   int64 `json:"total_tokens"`
}

type landingPublicResponse struct {
    Models []landingPublicModelResponse `json:"models"`
    Stats  landingPublicStatsResponse   `json:"stats"`
}

// cache disimpan di Server (lihat Step berikutnya)
type landingPublicCache struct {
    mu        sync.Mutex
    payload   *landingPublicResponse
    expiresAt time.Time
}
```

Handler `handleLandingPublic`:
1. Cek cache: jika `payload != nil && now < expiresAt` → tulis `response.SuccessI18n(c, "common.success", payload)` dan return.
2. `models, err := s.service.ListProjectModels(ctx, ProjectModelListQuery{GroupStatus: ProjectModelGroupStatusEnabled, PricingStatus: ProjectModelPricingStatusAll, Page: 1, PageSize: 100})` — kumpulkan `ClientModel` unik (map[string]struct{}), urutkan alfabetis (`sort.Strings`), batasi mis. 100 nama. Jika `Pagination.TotalPages > 1`, abaikan halaman berikutnya (catat `ponytail:` comment: hanya 100 model pertama; tambah loop halaman jika deployment punya >100 model).
3. `nowMS, err := safeEpochMilliseconds(s.now())`; `report, err := s.service.QueryUsage(ctx, requestlog.UsageQuery{FromMS: 0, ToMS: nowMS})`; `agg, err := mapUsageAggregate(report.Summary)`; stats = `{TotalRequests: agg.RequestCount, TotalTokens: agg.TotalTokens}`.
4. Simpan ke cache dengan `expiresAt = now.Add(landingPublicCacheTTL)`, lalu tulis respons.
5. Semua error → `writeServiceError(c, "landing_public", err)`.

Tambahkan field ke `Server` di `internal/control/server.go`:

```go
landingPublicCache *landingPublicCache // init di NewServer: &landingPublicCache{}
```

Tambahkan `LandingPublicHTTPModule()` (lihat blok kode di atas) dan registrasi di `internal/container/container.go` (~baris 329):

```go
app.HTTPModule(),
controlServer.LandingPublicHTTPModule(),
controlServer.HTTPModule(),
```

- [ ] **Step 5: Jalankan test, pastikan pass**

Run: `go test ./internal/control/ -run TestLandingPublic -v`
Expected: PASS

- [ ] **Step 6: Jalankan seluruh test package + build**

Run: `go test ./internal/control/ ./internal/container/ && go build ./...`
Expected: PASS semua, build sukses.

- [ ] **Step 7: Commit**

```bash
git add internal/control/landing_public.go internal/control/landing_public_test.go internal/control/server.go internal/container/container.go
git commit -m "feat(control): add public landing endpoint with models and all-time stats"
```

---

### Task 2: API client frontend `getLandingPublic`

**Files:**
- Create: `web/src/frontends/modern/api/landing-public.ts`
- Test: tidak ada test runner frontend di repo ini (verifikasi via typecheck + build di Task 5).

**Interfaces:**
- Consumes: `ApiClient` dari `@shared/http/client` (`request<T>(path: ApiPath, options?)`), helper `record`, `integer`, `list`, `text` dari `./response`.
- Produces:
  ```ts
  export interface LandingPublicStats {
    totalRequests: number
    totalTokens: number
  }
  export interface LandingPublicData {
    models: string[]
    stats: LandingPublicStats
  }
  export async function getLandingPublic(client: ApiClient, signal: AbortSignal): Promise<LandingPublicData>
  ```
  Memanggil `GET /api/public/landing` tanpa auth key. Catatan: `ApiClient.request` mungkin otomatis menyertakan auth header dari `getAuthKey()` — periksa `web/src/shared/http/client.ts`; jika ada opsi untuk melewati auth (mis. `authKey: ''` atau opsi `handleUnauthorized: false`), gunakan; jika tidak, menyertakan auth key pada endpoint publik tidak berbahaya (server mengabaikannya) — pilih jalur paling sederhana yang tidak memicu `onUnauthorized` redirect.

- [ ] **Step 1: Implementasi**

```ts
import type { ApiClient } from '@shared/http/client'

import { integer, list, record, text } from './response'

export interface LandingPublicStats {
  totalRequests: number
  totalTokens: number
}

export interface LandingPublicData {
  models: string[]
  stats: LandingPublicStats
}

export async function getLandingPublic(
  client: ApiClient,
  signal: AbortSignal,
): Promise<LandingPublicData> {
  const row = record(await client.request('/api/public/landing', { signal }))
  const stats = record(row.stats)
  return {
    models: list(row.models).map((item) => text(record(item).name)),
    stats: {
      totalRequests: integer(stats.total_requests),
      totalTokens: integer(stats.total_tokens),
    },
  }
}
```

- [ ] **Step 2: Typecheck**

Run: `cd web && pnpm exec vue-tsc --noEmit -p tsconfig.app.json` (atau script typecheck yang ada di `web/package.json` — cek dulu)
Expected: tidak ada error.

- [ ] **Step 3: Commit**

```bash
git add web/src/frontends/modern/api/landing-public.ts
git commit -m "feat(web): add landing public api client"
```

---

### Task 3: `LandingLayout.vue` + `LandingNavbar.vue` (navbar independent)

**Files:**
- Create: `web/src/frontends/modern/layouts/LandingLayout.vue`
- Create: `web/src/frontends/modern/layouts/LandingNavbar.vue`
- Modify: `web/src/frontends/modern/App.vue` (landing route pakai `LandingLayout`, bukan `PublicLayout`; ubah suffix `document.title` menjadi `Tokenizer`)

**Interfaces:**
- Consumes: `AppearanceMenu.vue` (existing, untuk toggle tema di navbar), design tokens `--modern-*`, komponen `AppButton` dari `@modern/components/ui`.
- Produces: `LandingLayout` dengan satu `<slot />`; navbar dengan anchor `#models`, `#harga`, `#faq` dan tombol "Masuk" → `{ name: 'modern-login' }`.

- [ ] **Step 1: `LandingNavbar.vue`**

Struktur:
- `<header>` sticky (position: sticky; top: 0; z-index di atas konten; background `var(--modern-surface)` dengan border-bottom `var(--modern-border)`).
- Kiri: brand "Tokenizer" — teks tebal (bukan BrandLogo mascot; brand baru) sebagai `RouterLink` ke `modern-landing`.
- Tengah (desktop): anchor links `Model`, `Harga`, `FAQ` (`<a href="#models">` dst. — anchor scroll, bukan router link).
- Kanan: `AppearanceMenu` + `AppButton variant="primary"` berisi `RouterLink` ke `modern-login` dengan label "Masuk".
- Mobile (max-width 760px): sembunyikan anchor tengah (ponytail: tanpa hamburger — anchor tetap bisa diakses via scroll; tambah menu hanya jika diminta).
- Semua styling scoped, pakai tokens `--modern-space-*`, `--modern-surface`, `--modern-border`, `--modern-accent`, `--modern-font-size-*` mengikuti pola `PublicLayout.vue`.

- [ ] **Step 2: `LandingLayout.vue`**

```vue
<script setup lang="ts">
import LandingNavbar from './LandingNavbar.vue'
</script>

<template>
  <div class="modern-landing-layout">
    <LandingNavbar />
    <main class="modern-landing-content"><slot /></main>
  </div>
</template>

<style scoped>
.modern-landing-layout {
  min-height: 100dvh;
  background: var(--modern-subtle);
}
.modern-landing-content {
  display: grid;
  justify-items: center;
}
</style>
```

- [ ] **Step 3: Ubah `App.vue`**

```vue
<RouterView v-slot="{ Component, currentRoute }">
```
menjadi (pertahankan struktur existing, hanya tambah cabang):

```vue
<RouterView v-slot="{ Component, route: currentRoute }">
  <AuthGate v-if="currentRoute.meta.requiresAuth">
    <AppLayout><component :is="Component" /></AppLayout>
  </AuthGate>
  <LandingLayout v-else-if="currentRoute.name === 'modern-landing'">
    <component :is="Component" />
  </LandingLayout>
  <PublicLayout v-else><component :is="Component" /></PublicLayout>
</RouterView>
```

Tambahkan import `LandingLayout`. Ubah `document.title = \`${title.value} · GPT-Load\`` menjadi `\`${title.value} · Tokenizer\``.

- [ ] **Step 4: Typecheck + build**

Run: `cd web && pnpm build` (atau `pnpm exec vite build` + typecheck script di package.json)
Expected: sukses.

- [ ] **Step 5: Commit**

```bash
git add web/src/frontends/modern/layouts/LandingLayout.vue web/src/frontends/modern/layouts/LandingNavbar.vue web/src/frontends/modern/App.vue
git commit -m "feat(web): add independent landing navbar and layout for Tokenizer"
```

---

### Task 4: Tulis ulang `LandingView.vue` (konten Tokenizer, Bahasa Indonesia)

**Files:**
- Modify (rewrite): `web/src/frontends/modern/features/landing/LandingView.vue`
- Modify: `web/src/frontends/modern/i18n/locales/landing.ts` (pangkas ke minimal — hanya `title`, karena `meta.titleKey: 'landing.title'` masih dipakai router; hapus keys lain dari ketiga locale)

**Interfaces:**
- Consumes: `getLandingPublic` dari `@modern/api/landing-public` (Task 2), `ApiClient` via inject `apiClientKey` dari `@shared/http/client-context` (lihat pemakaian di view lain, mis. `ModelsView.vue`), komponen `AppButton`, `AppIcon` dari `@modern/components/ui`, ikon dari `@lucide/vue`.
- Produces: halaman landing lengkap dengan section id anchor: `models`, `harga`, `faq`.

**Struktur konten (semua copy Bahasa Indonesia, hardcode):**

1. **Hero**: badge "PAYG · AI Frontier · Indonesia"; H1 "Akses Model AI Frontier, Bayar Sesuai Pemakaian"; subtext 1-2 kalimat (satu API untuk GPT, Claude, Gemini, dsb.; tanpa langganan; isi saldo dalam Rupiah); CTA primer "Masuk Konsol" → `modern-login`; CTA sekunder anchor "Lihat Model" → `#models`.
2. **Stats strip** (di bawah hero, 3 card berdampingan, grid responsif): "Total Request" (`stats.totalRequests`, format `Intl.NumberFormat('id-ID')`), "Total Token" (`stats.totalTokens`), "Model Tersedia" (`models.length`). Label kecil caps di atas angka besar. Loading: tampilkan "—"; error fetch: sembunyikan strip (jangan tampilkan pesan error di landing).
3. **Models section** (`<section id="models">`): judul "Model Frontier yang Tersedia", grid chip/card nama model dari API (fallback saat kosong/gagal: teks "Model akan segera diumumkan." — jangan render grid kosong).
4. **Fitur** (4 card, ikon lucide `Coins`, `Zap`, `ShieldCheck`, `ChartNoAxesCombined`): "Bayar per Token", "Model Frontier Terbaru", "Aman & Terpisah per Kunci", "Pantau Pemakaian Real-time" — masing-masing 1 kalimat deskripsi.
5. **Cara Kerja** (3 langkah bernomor): "1. Daftar & buat kunci API" → "2. Isi saldo (Rupiah)" → "3. Panggil satu endpoint untuk semua model".
6. **Harga** (`<section id="harga">`): satu panel — "Tanpa langganan. Saldo terpotong per token sesuai harga model yang dipakai." + CTA "Lihat harga di konsol" → `modern-login`.
7. **FAQ** (`<section id="faq">`): 4 item `<details>/<summary>` native (ponytail: tanpa komponen accordion baru): "Bagaimana cara membayar?", "Model apa saja yang didukung?", "Apakah ada biaya langganan?", "Apakah ada batas pemakaian?".
8. **Footer**: "© 2026 Tokenizer" + link "Masuk".

Fetch data: `onMounted` dengan `AbortController`, `getLandingPublic(client, signal)`, cleanup abort di `onBeforeUnmount`.

- [ ] **Step 1: Rewrite `LandingView.vue`** sesuai struktur di atas, styling scoped memakai tokens `--modern-*` (rujuk `LandingView.vue` lama untuk pola class & spacing).

- [ ] **Step 2: Pangkas `landing.ts`**

Setiap export (`zhCN`, `enUS`, `jaJP`) menjadi:

```ts
export const zhCN = { title: 'Tokenizer' }
export const enUS: typeof zhCN = { title: 'Tokenizer' }
export const jaJP: typeof zhCN = { title: 'Tokenizer' }
```

Pastikan tidak ada referensi `landing.*` lain yang tersisa: `grep -rn "landing\." web/src/frontends/modern --include="*.vue" --include="*.ts" | grep -v locales | grep -v "modern-landing\|landing/"`
Expected: hanya `titleKey: 'landing.title'` di router.

- [ ] **Step 3: Typecheck + build**

Run: `cd web && pnpm build`
Expected: sukses.

- [ ] **Step 4: Verifikasi visual manual**

Jalankan dev server backend + frontend, buka `/` tanpa login: navbar Tokenizer tampil, stats menampilkan angka (atau "—" saat loading), daftar model muncul, anchor scroll bekerja, tombol Masuk menuju login, dan halaman login tetap memakai header lama (BrandLogo). Cek juga locale zh/en/ja tidak error.

- [ ] **Step 5: Commit**

```bash
git add web/src/frontends/modern/features/landing/LandingView.vue web/src/frontends/modern/i18n/locales/landing.ts
git commit -m "feat(web): rewrite landing page as Tokenizer PAYG AI Frontier (id-ID)"
```

---

### Task 5: Verifikasi end-to-end

- [ ] **Step 1:** `go build ./... && go test ./internal/control/ ./internal/container/`
- [ ] **Step 2:** `cd web && pnpm build`
- [ ] **Step 3:** Jalankan server (`go run .` atau sesuai README), buka landing di browser, verifikasi: data models & stats live dari `/api/public/landing` (cek Network tab, tanpa Authorization header), navbar independent, login/dashboard tidak terpengaruh.
- [ ] **Step 4:** Update `AGENTS.md` jika ada bagian yang menyebut landing page/branding GPT-Load (cek keberadaan file dulu: `ls AGENTS.md web/AGENTS.md`).
