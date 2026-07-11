# Usage Page Implementation Plan — AxonRouter-GO

> Analyzed against three reference codebases: **9router** (`/workspaces/9router`), **OmniRoute** (`/workspaces/OmniRoute`), and current **AxonRouter-GO**. All file:line references verified from source.

---

## Part A: Critical Bugs Found in AxonRouter-GO (Must Fix Before Usage Page)

### Bug 1: `cache_creation_tokens` is NOT tracked — merged into `cached_tokens` (LOSSY)

**Root cause:** `stream.go:61` merges cache_creation + cache_read into a single field:
```go
counts.CachedTokens = claude.Message.Usage.CacheCreationInputTokens + claude.Message.Usage.CacheReadInputTokens
```

**Impact:** Cost calculation is wrong. Cache creation (write) tokens are charged at cache READ rate instead of cache WRITE rate. For Claude Sonnet: cache read = $0.30/1M, cache write = $3.75/1M — 12.5x difference.

**Evidence (OmniRoute does it right):** `tokenAccounting.ts:23-42` keeps them separate:
- `getPromptCacheReadTokens()` → `cache_read_input_tokens` / `cached_tokens`
- `getPromptCacheCreationTokens()` → `cache_creation_input_tokens` / `prompt_tokens_details.cache_creation_tokens`
- `usage_history` table has separate columns: `tokens_cache_read` + `tokens_cache_creation`

### Bug 2: `Pricing` struct missing `CachedWritePer1K`

**Root cause:** `pricing.go:11-18`:
```go
type Pricing struct {
    InputPer1K     float64
    OutputPer1K    float64
    ReasonPer1K    float64
    ImagePerUnit   float64
    AudioPerMin    float64
    CachedReadPer1K float64
    // MISSING: CachedWritePer1K  ← exists in ModelPricingRow (line 29) and DB table, but NOT passed through
}
```

**Impact:** Even if we track cache_creation tokens, `EstimateCost` can't charge them at the correct rate. The DB has the column (`cached_write_per_1k`), `ModelPricingRow` has the field, but `GetPricing()` (line 94-102) drops it.

### Bug 3: `EstimateCost` doesn't charge cache_creation, and double-charges cache

**Root cause:** `pricing.go:133-144`:
```go
func EstimateCost(modelID string, inputTokens, outputTokens, reasoningTokens, cachedTokens int64) float64 {
    p := GetPricing(modelID)
    nonCached := inputTokens - cachedTokens  // Bug: cachedTokens includes cache_creation (from stream.go merge)
    cost := float64(nonCached)/1000.0*p.InputPer1K
    cost += float64(cachedTokens)/1000.0*p.CachedReadPer1K  // Bug: ALL charged at read rate, no write rate
    // MISSING: cache_creation at CachedWritePer1K rate
    ...
}
```

**OmniRoute's correct algorithm** (`costCalculator.ts:93-131`):
```typescript
const nonCachedInput = Math.max(0, inputTokens - cachedTokens - cacheCreationTokens);
cost += nonCachedInput * (inputPrice / 1_000_000);
cost += cachedTokens * (cachedPrice / 1_000_000);           // cache READ
cost += cacheCreationTokens * (cacheCreationPrice / 1_000_000); // cache WRITE (separate rate)
cost += outputTokens * (outputPrice / 1_000_000);
cost += reasoningTokens * (reasoningPrice / 1_000_000);
```

### Bug 4: `request_logs` table missing `cache_creation_tokens` column

**Root cause:** `migrations.go:91-107` — table has `cached_tokens` (cache read) but no `cache_creation_tokens`.

### Bug 5: `LogEntry` struct missing `CacheCreationTokens` field

**Root cause:** `tracker.go:15-30` — no field for cache creation tokens.

---

## Part B: Three-Way Comparison (Verified from Source)

| Aspect | 9router | OmniRoute (most advanced) | AxonRouter-GO (current, buggy) |
|---|---|---|---|
| **Cache tracking** | `canonicalizeUsage()` folds both into prompt, keeps `cached_tokens` + `cache_creation_input_tokens` as separate fields in JSON | Separate DB columns: `tokens_cache_read` + `tokens_cache_creation` + `tokens_reasoning` | Merges both into single `cached_tokens` — LOSSY |
| **Cost calc** | `calculateCostFromTokens` ($/1M): subtracts both cache types from input, charges each at own rate | `computeCostFromPricing` ($/1M): same algorithm, pre-computed `stored_cost` + re-calc for aggregates | `EstimateCost` ($/1K): charges all cached at READ rate, no cache_creation charge |
| **Pricing source** | `getPricingForModel(provider, model)` → `PROVIDER_PRICING` + `MODEL_PRICING` + `PATTERN_PRICING` | `getPricingForModel(provider, model)` → DB pricing table with fallback chain | `GetPricing(modelID)` → `model_pricing` DB table with substring fallback. Missing `CachedWritePer1K` passthrough |
| **Storage** | `usageHistory` (per-request) + `usageDaily` (JSON blob per day) | `usage_history` with dedicated token columns + `stored_cost` + `cost_tokens_*` pre-computed | `request_logs` with merged `cached_tokens`, no `cache_creation_tokens` column |
| **Aggregation** | Daily JSON blobs for 7d+, live rows for 24h/today | UNION of recent raw + older aggregated data, per-dimension GROUP BY | Live SQL GROUP BY, per-provider/model only |
| **Real-time** | SSE via EventEmitter | SSE + event bus | Polling `/logs/active` |
| **Cost at save time** | Yes — `calculateCost` called before INSERT | Yes — `stored_cost` column pre-computed | Yes — `EstimateCost` called in `tracker.Log()` |
| **Input token convention** | `canonicalizeUsage` folds cache INTO prompt (cache-inclusive) | `getLoggedInputTokens`: Claude adds cache back, OpenAI passthrough (cache-inclusive) | Not handled — raw provider values stored as-is |

### Best approach per aspect (for AxonRouter-GO):

1. **Token tracking** → OmniRoute's approach: separate columns for `cache_creation_tokens` (most correct, enables accurate cost)
2. **Cost calculation** → OmniRoute's `computeCostFromPricing` algorithm: 5-way split (input, cached_read, cached_write, output, reasoning)
3. **Pricing** → Use existing `model_pricing` DB table (already has `cached_write_per_1k`), just fix `Pricing` struct + `GetPricing()` passthrough
4. **Aggregation** → Live SQL GROUP BY (AxonRouter-GO already has indexes). No `usage_daily` table needed.
5. **Real-time** → Polling (reuse existing `/logs/active`)

---

## Part C: Implementation Plan

### Phase 0: Fix Cache Token Tracking Bugs (Refactor — Required)

> Without this, cost numbers on the usage page will be wrong. This is the "refactor if needed" part.

#### 0.1 Add `CacheCreationTokens` to `StreamTokenCounts`

**File:** `internal/api/handlers/v1/stream.go`

```go
type StreamTokenCounts struct {
    InputTokens         int64
    OutputTokens        int64
    ReasoningTokens     int64
    CachedTokens        int64  // cache READ only
    CacheCreationTokens int64  // cache WRITE (new)
}
```

Fix Claude extraction (line 58-62):
```go
// BEFORE (buggy — merges):
counts.CachedTokens = claude.Message.Usage.CacheCreationInputTokens + claude.Message.Usage.CacheReadInputTokens

// AFTER (correct — separates):
counts.CachedTokens = claude.Message.Usage.CacheReadInputTokens
counts.CacheCreationTokens = claude.Message.Usage.CacheCreationInputTokens
```

Also fix OpenAI extraction to capture `prompt_tokens_details.cache_creation_tokens` if present, and Gemini's `cachedContentTokenCount` stays as cache read.

Update `ExtractTokensFromBody` (non-streaming path, line 87+) to also extract `cache_creation_input_tokens` from Claude-format usage.

#### 0.2 Add `CacheCreationTokens` to `LogEntry`

**File:** `internal/usage/tracker.go:15-30`

```go
type LogEntry struct {
    ...
    CachedTokens        int64
    CacheCreationTokens int64  // NEW
    ...
}
```

#### 0.3 Add `cache_creation_tokens` column to `request_logs`

**File:** `internal/db/migrations.go` — add to ALTER TABLE block (line ~137):

```go
`ALTER TABLE request_logs ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0`,
```

This is idempotent (existing ALTER TABLE error-ignore pattern handles re-runs).

#### 0.4 Fix `Pricing` struct + `GetPricing()` passthrough

**File:** `internal/usage/pricing.go`

```go
type Pricing struct {
    InputPer1K       float64
    OutputPer1K      float64
    ReasonPer1K      float64
    ImagePerUnit     float64
    AudioPerMin      float64
    CachedReadPer1K  float64
    CachedWritePer1K float64  // NEW
}
```

Update `GetPricing()` (line 94-102 and 120-128) to pass through `r.CachedWritePer1K` from `ModelPricingRow`.

Update `defaultPricing` to include `CachedWritePer1K: 0.001` (same as input fallback).

#### 0.5 Fix `EstimateCost` — 5-way cost split

**File:** `internal/usage/pricing.go:132-144`

```go
func EstimateCost(modelID string, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens int64) float64 {
    p := GetPricing(modelID)
    // Subtract BOTH cache types from input to avoid double-charging
    nonCached := inputTokens - cachedTokens - cacheCreationTokens
    if nonCached < 0 {
        nonCached = 0
    }
    cost := float64(nonCached) / 1000.0 * p.InputPer1K
    cost += float64(cachedTokens) / 1000.0 * p.CachedReadPer1K
    cost += float64(cacheCreationTokens) / 1000.0 * p.CachedWritePer1K  // NEW — correct rate
    cost += float64(outputTokens) / 1000.0 * p.OutputPer1K
    cost += float64(reasoningTokens) / 1000.0 * p.ReasonPer1K
    return cost
}
```

**BREAKING:** Signature changes from 5 params to 6. All call sites must be updated.

#### 0.6 Update `flushBatch` INSERT

**File:** `internal/usage/tracker.go:138-163`

Add `cache_creation_tokens` to INSERT column list + value list + `stmt.Exec()` call.

#### 0.7 Update ALL `tracker.Log()` call sites

**Files:** `internal/api/handlers/v1/chat.go`, `messages.go`, `responses.go`, `embeddings.go`, `images.go`, `tts.go`, `stt.go`, `video.go`, `handler.go`

Every call site must pass `CacheCreationTokens: tokenCounts.CacheCreationTokens` (for paths that have token extraction) or `CacheCreationTokens: 0` (for error paths / image/tts/stt that don't have cache).

For the streaming path in `handler.go:794-806`, pass `CacheCreationTokens: tokenCounts.CacheCreationTokens`.

#### 0.8 Update `QueryLogs` SELECT + Scan

**File:** `internal/usage/queries.go:42-66`

Add `cache_creation_tokens` to SELECT column list and `rows.Scan()` destination.

#### 0.9 Update `RequestLog` model + `MarshalJSON`

**File:** `internal/db/models.go` — add `CacheCreationTokens` field to `RequestLog` struct and include in `MarshalJSON()` map.

#### 0.10 Update aggregator queries

**File:** `internal/usage/aggregator.go` — add `cache_creation_tokens` to SUM in `GetProviderUsage`, `GetModelUsage` queries.

---

### Phase 1: Backend — Usage Stats API (Go)

#### 1.1 Extend Aggregator with Period-Based Multi-Dimensional Stats

**File:** `internal/usage/aggregator.go` (extend)

```go
type UsageStats struct {
    TotalRequests        int64                 `json:"total_requests"`
    TotalInputTokens     int64                 `json:"total_input_tokens"`
    TotalOutputTokens    int64                 `json:"total_output_tokens"`
    TotalCachedTokens    int64                 `json:"total_cached_tokens"`
    TotalCacheCreation   int64                 `json:"total_cache_creation_tokens"`
    TotalReasoningTokens int64                 `json:"total_reasoning_tokens"`
    TotalCost            float64               `json:"total_cost"`
    ByProvider           []UsageBreakdownEntry `json:"by_provider"`
    ByModel              []UsageBreakdownEntry `json:"by_model"`
    ByConnection         []UsageBreakdownEntry `json:"by_connection"`
    ByModality           []UsageBreakdownEntry `json:"by_modality"`
    RecentRequests       []RecentRequestEntry  `json:"recent_requests"`
    Last10Minutes        []MinuteBucket         `json:"last_10_minutes"`
}

type UsageBreakdownEntry struct {
    Key               string  `json:"key"`
    Label             string  `json:"label"`
    Requests          int64   `json:"requests"`
    InputTokens       int64   `json:"input_tokens"`
    OutputTokens      int64   `json:"output_tokens"`
    CachedTokens      int64   `json:"cached_tokens"`
    CacheCreationTokens int64 `json:"cache_creation_tokens"`
    ReasoningTokens   int64   `json:"reasoning_tokens"`
    TotalTokens       int64   `json:"total_tokens"`
    CostUsd           float64 `json:"cost_usd"`
    Errors            int64   `json:"errors"`
    LastUsed          int64   `json:"last_used"`
}

type RecentRequestEntry struct {
    Timestamp     int64   `json:"timestamp"`
    ProviderTypeID string  `json:"provider_type_id"`
    ModelID       string  `json:"model_id"`
    InputTokens   int64   `json:"input_tokens"`
    OutputTokens  int64   `json:"output_tokens"`
    CachedTokens  int64   `json:"cached_tokens"`
    CostUsd       float64 `json:"cost_usd"`
    StatusCode    int     `json:"status_code"`
}

type MinuteBucket struct {
    Timestamp    int64   `json:"timestamp"`
    Requests     int64   `json:"requests"`
    InputTokens  int64   `json:"input_tokens"`
    OutputTokens int64   `json:"output_tokens"`
    Cost         float64 `json:"cost"`
}

type ChartBucket struct {
    Label  string  `json:"label"`
    Tokens int64   `json:"tokens"`
    Cost   float64 `json:"cost"`
}
```

Methods:
- `GetUsageStats(period string) (*UsageStats, error)` — period: "today"|"24h"|"7d"|"30d"|"60d"
  - Computes since-cutoff in unix ms
  - SQL GROUP BY for each dimension (provider, model, connection_id, modality)
  - Recent = last 20 rows (deduplicated like 9router)
  - Last10Minutes = 10 per-minute buckets
  - Cost from `SUM(cost_usd)` (pre-computed at save time)
- `GetChartData(period string) ([]ChartBucket, error)`
  - "today"→24 hourly buckets; "24h"→24 hourly; "7d/30d/60d"→daily buckets
  - Each bucket: {label, tokens (input+output+cached+cache_creation+reasoning), cost}

**No `usage_daily` table** — Go SQL + existing indexes handle GROUP BY efficiently. OmniRoute uses aggregation for massive scale (231 providers); AxonRouter-GO targets 100-1000+ connections.

#### 1.2 Usage Handler

**File:** `internal/api/handlers/admin/usage.go` (NEW)

```go
type UsageHandler struct {
    db *sql.DB
}

func NewUsageHandler(database *sql.DB) *UsageHandler
func (h *UsageHandler) Stats(c *gin.Context) // GET /api/admin/usage/stats?period=today
func (h *UsageHandler) Chart(c *gin.Context) // GET /api/admin/usage/chart?period=7d
```

#### 1.3 Register Routes

**File:** `internal/api/router.go` (extend)

```go
usageH := admin.NewUsageHandler(cfg.DB)
adminGroup.GET("/usage/stats", usageH.Stats)
adminGroup.GET("/usage/chart", usageH.Chart)
```

---

### Phase 2: Frontend — Usage Page (Svelte)

#### 2.1 API Client

**File:** `web/src/lib/api.ts` (extend)

```typescript
export const usageApi = {
  stats: (period: string) => fetchApi<UsageStats>(`/usage/stats?period=${period}`),
  chart: (period: string) => fetchApi<ChartBucket[]>(`/usage/chart?period=${period}`),
};
```

Types: `UsageStats`, `UsageBreakdownEntry`, `RecentRequestEntry`, `MinuteBucket`, `ChartBucket`.

#### 2.2 Usage Page Component

**File:** `web/src/pages/Usage.svelte` (NEW)

Layout (AGENTS.md conventions):
```
<div class="flex flex-1 flex-col gap-6 p-6">
  <!-- Heading -->
  <div class="space-y-1">
    <h1 class="text-display-lg">Usage.</h1>
    <p class="text-body-sm text-muted-foreground">Token, cost, and request analytics across all providers.</p>
  </div>

  <!-- Period selector + table view toggle -->
  <div class="flex items-center justify-between">
    <SegmentedControl options={[model, connection, provider]} />
    <SegmentedControl options={[today, 24h, 7d, 30d, 60d]} />
  </div>

  <!-- Overview Cards (5) -->
  <div class="grid gap-4 md:grid-cols-5">
    Total Requests | Input Tokens | Cached Tokens | Output Tokens | Est. Cost
  </div>

  <!-- Active Octopus (reuse existing) -->
  <Card><ActiveOctopus requests={activeRequests} /></Card>

  <!-- Area Chart (tokens/cost toggle) -->
  <Card>Chart with tokens/cost toggle</Card>

  <!-- Breakdown Table with tokens/costs toggle -->
  <Card>UsageTable</Card>
</div>
```

Components:
- **OverviewCards** — 5 stat cards (requests, input, cached, output, cost). Follow Dashboard.svelte pattern (accent top bar, `text-display-sm`, `text-caption-mono`).
- **UsageChart** — Area chart with gradient fill (tokens vs cost toggle). Period-synced.
- **UsageTable** — Sortable table with expandable groups. Table view selector: byModel / byConnection / byProvider. View mode toggle: Tokens (input/cached/cache_creation/output/total columns) vs Costs (input/cached/cache_creation/output/total cost columns). Cost split via token-share allocation.

#### 2.3 Router + Sidebar Integration

**File:** `web/src/App.svelte` (extend)
- Import Usage page
- Add route: `if (segments[0] === 'usage') return { component: Usage, params: {} }`
- Add label: `usage: 'Usage'`

**File:** `web/src/lib/components/sidebar/SidebarNav.svelte` (extend)
- Add: `{ href: '/usage', label: 'Usage', icon: BarChart3 }`

#### 2.4 Stores

**File:** `web/src/lib/stores.ts` (extend)

```typescript
export const usageStats = writable<UsageStats | null>(null);
export const usageChart = writable<ChartBucket[]>([]);
export const usageLoading = writable(false);
export const usagePeriod = writable('today');
export const usageTableView = writable<'model' | 'connection' | 'provider'>('model');
export const usageViewMode = writable<'tokens' | 'costs'>('costs');

export async function loadUsageStats(period: string) { ... }
export async function loadUsageChart(period: string) { ... }
```

---

### Phase 3: Real-Time Updates

- **Polling** for active requests (reuse existing `/api/admin/logs/active`). 3s interval.
- Overview cards + chart re-fetch on period change only.
- Recent requests included in `/usage/stats` response (no separate endpoint).

---

### Phase 4: Testing

- **Unit test Phase 0:** Verify `EstimateCost` with cache_creation tokens charges at correct rate. Verify `ExtractTokensFromFinalChunk` separates cache_read from cache_creation.
- **Unit test Phase 1:** `GetUsageStats` and `GetChartData` with seeded `request_logs` for each period. Verify breakdown sums match totals.
- **Build test:** `go build ./...` + `go vet ./...` + `npm run build` (zero warnings).
- **Smoke test:** `make run-dev`, navigate to `/usage`, verify page renders, period selector works, table toggles work.

---

## File Change Summary

| File | Action | Phase | Description |
|---|---|---|---|
| `internal/api/handlers/v1/stream.go` | MODIFY | 0 | Add `CacheCreationTokens` to `StreamTokenCounts`, fix Claude extraction, fix `ExtractTokensFromBody` |
| `internal/usage/tracker.go` | MODIFY | 0 | Add `CacheCreationTokens` to `LogEntry`, update `flushBatch` INSERT, update `EstimateCost` call |
| `internal/usage/pricing.go` | MODIFY | 0 | Add `CachedWritePer1K` to `Pricing`, fix `GetPricing()` passthrough, fix `EstimateCost` signature + algorithm |
| `internal/db/migrations.go` | EXTEND | 0 | Add `ALTER TABLE request_logs ADD COLUMN cache_creation_tokens` |
| `internal/db/models.go` | EXTEND | 0 | Add `CacheCreationTokens` to `RequestLog` + `MarshalJSON` |
| `internal/usage/queries.go` | EXTEND | 0 | Add `cache_creation_tokens` to SELECT + Scan |
| `internal/api/handlers/v1/chat.go` | MODIFY | 0 | Pass `CacheCreationTokens` to `tracker.Log` |
| `internal/api/handlers/v1/messages.go` | MODIFY | 0 | Pass `CacheCreationTokens` to `tracker.Log` |
| `internal/api/handlers/v1/responses.go` | MODIFY | 0 | Pass `CacheCreationTokens` to `tracker.Log` |
| `internal/api/handlers/v1/handler.go` | MODIFY | 0 | Pass `CacheCreationTokens` to `tracker.Log` (streaming path) |
| `internal/api/handlers/v1/embeddings.go` | MODIFY | 0 | Pass `CacheCreationTokens: 0` to `tracker.Log` |
| `internal/api/handlers/v1/images.go` | MODIFY | 0 | Pass `CacheCreationTokens: 0` to `tracker.Log` |
| `internal/api/handlers/v1/tts.go` | MODIFY | 0 | Pass `CacheCreationTokens: 0` to `tracker.Log` |
| `internal/api/handlers/v1/stt.go` | MODIFY | 0 | Pass `CacheCreationTokens: 0` to `tracker.Log` |
| `internal/api/handlers/v1/video.go` | MODIFY | 0 | Pass `CacheCreationTokens: 0` to `tracker.Log` |
| `internal/usage/aggregator.go` | EXTEND | 0+1 | Add `cache_creation_tokens` to existing SUM queries; add `UsageStats` struct + `GetUsageStats(period)` + `GetChartData(period)` |
| `internal/usage/pricing_test.go` | EXTEND | 0 | Update `EstimateCost` tests for new 6-param signature |
| `internal/api/handlers/admin/usage.go` | NEW | 1 | `UsageHandler` with `Stats` + `Chart` endpoints |
| `internal/api/router.go` | EXTEND | 1 | Register `/usage/stats` + `/usage/chart` routes |
| `web/src/lib/api.ts` | EXTEND | 2 | Add `usageApi` + types |
| `web/src/lib/stores.ts` | EXTEND | 2 | Add usage stores + load functions |
| `web/src/pages/Usage.svelte` | NEW | 2 | Full usage page: cards, octopus, chart, table |
| `web/src/App.svelte` | EXTEND | 2 | Import Usage, add route + label |
| `web/src/lib/components/sidebar/SidebarNav.svelte` | EXTEND | 2 | Add Usage nav item |
| `internal/usage/aggregator_test.go` | NEW | 4 | Test `GetUsageStats` + `GetChartData` |

---

## Key Design Decisions

1. **Separate cache_read from cache_creation** (OmniRoute approach) — Most correct. Enables accurate cost: cache read at `CachedReadPer1K`, cache creation at `CachedWritePer1K`. Cost comes from `model_pricing` DB table (already exists with `cached_write_per_1k` column).

2. **Pre-compute cost at save time** (already done) — `EstimateCost` called in `tracker.Log()`, `cost_usd` stored per-row. Aggregation just `SUM(cost_usd)` — no re-pricing needed at query time (OmniRoute's `stored_cost` pattern).

3. **No `usage_daily` table** — Go SQL + indexes handle GROUP BY. YAGNI until 1M+ rows.

4. **Polling over SSE** — Reuse existing `/logs/active`. Simpler, consistent.

5. **Reuse ActiveOctopus** — User specifically referenced the "octopus" visual.

6. **Dimensions: byProvider, byModel, byConnection, byModality** — Adapted to AxonRouter's schema (no API key per request, but modality is tracked).

7. **Cost breakdown in table via token-share** — `inputCost = nonCachedInput * (totalCost / totalTokens)`. Same as 9router. Not per-rate recompute. Simple and consistent.

8. **Phase 0 MUST run before Phase 1-2** — Cost numbers will be wrong without fixing cache tracking first. The user said "refactor jika perlu asalkan sistem stabil" — this refactor is required for correctness.
