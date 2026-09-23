# Client Area Activity Heatmap

## Goal

Show a GitHub-style daily activity heatmap on the client-area Overview page
(`DashboardView.vue`), visualising total daily token usage across the signed-in
user's owned access keys.

## Metric & range

- Metric: total tokens = `uncached_input_tokens` + `output_tokens` +
  `cache_read_tokens` + `cache_write_5m_tokens` + `cache_write_1h_tokens` +
  `cache_write_unknown_tokens`, summed per calendar day.
- Range: last 365 days (UTC days), GitHub default ~53-week grid.

## Architecture

Reuse the existing `GET /api/clientarea/usage` endpoint. The dashboard already
calls it; add an optional `granularity=day` query parameter that appends a
`daily` array to the response. Without the parameter the response shape is
unchanged (existing per-model contract preserved).

### Backend (`internal/userdash/usage.go`)

- New `DailyTokenRow { day_ms int64, total_tokens int64 }`.
- `QueryOwnedDailyTokens(ctx, userID, fromMS, toMS)`: join `usage_stats` to
  owned `access_keys`, group by `bucket_start_ms / 86400000`, sum the six token
  columns. Day bucket is returned as `day_ms` (aligned UTC midnight).
- `handleUsage`: when `granularity=day`, respond `{ "models": [...],
  "daily": [...] }`; otherwise respond the legacy array.

Data is hourly-bucketed and UTC-aligned (`bucket_start_ms % 3600000 == 0`), so
integer day division is exact.

### Frontend

- `api/clientarea.ts`: add optional `daily` to the usage response, a
  `granularity` param, and a `DailyTokenRow` type.
- New `features/clientarea/UsageHeatmap.vue`:
  - Props: `rows: DailyTokenRow[]`, `loading`, `failed`.
  - Builds 53 columns x 7 rows (Sun..Sat), aligned so the last column ends on
    the current day.
  - Five intensity levels from a threshold scale over the max daily value;
    zero = empty cell.
  - `AppTooltip` per cell: date + formatted token count.
  - Month labels above columns, weekday labels on the left, legend below.
  - Pure CSS grid; no new dependency.
- `DashboardView.vue`: request 365 days with `granularity=day`, keep the
  existing per-model totals from the same response, render `<UsageHeatmap>`
  below the stat cards.
- i18n: `clientarea.dashboard.heatmap.*` in en-US, zh-CN, ja-JP
  (title, description, less/more legend, tokens-per-day tooltip, empty).

## Testing

- Go: extend `keys_test.go` coverage — a test asserting daily token sums are
  owned-key-scoped, day-aligned, and that the legacy (no-granularity) shape is
  unchanged.
- Frontend: heatmap level threshold function carries a small runnable check.

## Out of scope

- Per-key or per-model heatmaps.
- Request-count or cost metrics.
- Configurable range.
