# Design: Per-API-Key Limits (User-managed, Admin-observable)

Branch: `feat/dashboard-user-api-limit` · Date: 2026-09-23 · Status: approved

## Goal

Let each user configure limits on **their own** API keys from the client area, and
let admins see and edit those same limits from the admin dashboard. Users own the
configuration; admins monitor and can override.

Per key the user can set:

1. **Usage credit** — total cost cap for the key (e.g. total $5 across all requests).
2. **RPM** — requests per minute.
3. **Concurrent** — maximum simultaneous in-flight requests.
4. **Model** — allowlist of models the key may call.

Plus two client-area UX changes:

- The key list shows **total usage** (all-time) instead of 30-day usage.
- Disabling a key becomes a **toggle**; the user can re-enable it.

## Decisions (from Q&A)

- **Usage credit** = total cost limit per key, reusing the existing cost-limit
  engine (not a separate per-key wallet).
- **TPM is skipped** — no storage, no UI, no enforcement in this change.
- **Concurrent is enforced now** — a new runtime engine, not just a stored field.
- **Model** = existing `Filters.Models` allowlist (already enforced at the gateway).
- **Total usage** = lifetime accumulated cost (USD) of the key, not a token count.
- Re-enable applies to keys owned by the user that are currently disabled.

## Verified existing machinery (reused, not rebuilt)

- `access_keys.rpm_limit` (int64, 0 = unlimited) + `ratelimit.AccessKeyRPM`
  sliding-window limiter, enforced at `internal/gateway/handler.go:513` and in
  `websocket.go:519` / `websocket_turn.go:161`.
- Cost limits: normalized `access_key_cost_limit_rules` +
  `access_key_cost_limit_states` (migration `0002`), in-memory
  `internal/accessquota/runtime.go`, loaded into
  `state.AccessKeyView.CostLimitRules`, enforced at `handler.go:458-512` with
  idempotent completion at `handler.go:476-489`.
- Model allowlist: `access_keys.filters` JSON -> `state.FilterSet.Models`,
  already enforced by the gateway router.
- Admin access-key edit surface: `PUT /api/access-keys/:id`
  (`internal/control/access_keys.go`, routes in `internal/control/http_routes.go:447-524`).

## Non-goals

- TPM limiting.
- Per-key balance/wallet separate from the user balance.
- Per-model request/token quotas (key-level per-model limits).
- Concurrent-limit persistence across restarts (it is live in-flight state).
- Payment, email, or voucher flows (unchanged from the user-dashboard design).

## Data model — migration `0023_access_key_concurrency_limit`

Only concurrency needs new storage. Everything else already exists.

New column on `access_keys`:

```
max_concurrent BIGINT NOT NULL DEFAULT 0
  CHECK (max_concurrent >= 0)      -- 0 = unlimited
```

- ALTER style follows migration `0009` / `0022`: guard with
  `db.Migrator().HasColumn(...)`, then `ALTER TABLE ... ADD COLUMN`, with
  dialect-aware identifier quoting; `Validate0023` asserts column + check;
  `ValidateRecoverable0023` accepts both pre- and post-column states idempotently.
- Registered in `internal/storage/migration.go` in contiguous order, with a
  companion `0023_access_key_concurrency_limit_test.go`.

Mapping of the four limits to storage:

| Limit | Storage | Enforced by |
|---|---|---|
| Usage credit | `access_key_cost_limit_rules` (one `total` rule) | `accessquota` runtime |
| RPM | `access_keys.rpm_limit` | `ratelimit.AccessKeyRPM` |
| Concurrent | `access_keys.max_concurrent` (new) | new `ratelimit.AccessKeyConcurrency` |
| Model | `access_keys.filters.models` | gateway router |

Concurrency is **runtime-only** (in-flight count), so there is no state table and
no checkpoint/reconcile path.

## Backend — client area (`internal/userdash/`)

New endpoints, all session-auth and ownership-scoped (`WHERE id = ? AND user_id = ?`):

| Route | Method | Body | Notes |
|---|---|---|---|
| `/api/clientarea/keys/:id/limits` | PATCH | `{ rpm_limit?, max_concurrent?, credit_limit_nano_usd?, models? }` | set-aware optional fields; `credit_limit_nano_usd: 0` clears the total rule; empty `models` clears the allowlist |
| `/api/clientarea/keys/:id/status` | PATCH | `{ status: "active" \| "disabled" }` | replaces disable-only; validates the enum |

Changes to existing client-area surface:

- `OwnedKeyView` gains: `rpm_limit`, `max_concurrent`, `credit_limit_nano_usd`,
  `models []string`, and `usage_total_nano_usd` (all-time cost).
- `ListOwnedKeys` replaces the 30-day usage aggregate with a lifetime-cost
  aggregate over `usage_stats` (sum `estimated_cost_nano_usd`, no time window);
  `usage_30d_nano_usd` and `request_count_30d` are dropped from the client-area
  contract (the dashboard heatmap/usage views use the separate `/usage`
  endpoint and are unaffected).
- `DisableOwnedKey` is generalized to `SetOwnedKeyStatus(ctx, userID, keyID, status)`;
  disabling keeps today's behavior, enabling sets `status = "active"`.

Limit parsing/validation is shared with the admin path:
- RPM: reuse `normalizeRPMLimit` semantics (>= 0, 0 = unlimited).
- Concurrency: >= 0, 0 = unlimited, sane upper bound.
- Credit: >= 0 nano-USD, 0 = no limit; maps to one `total` cost rule.
- Models: trimmed, de-duplicated, bounded count, non-empty strings — reuse
  `normalizeAccessKeyFilters` model handling.

After any mutation that changes enforcement inputs, call `s.refreshRuntime(ctx)`
(existing pattern) so the gateway snapshot picks up the new values. No balance
cache interaction is required (limits do not touch balance).

### Admin — see and edit the same values

- `AdminUserKeyView` (in `internal/userdash/admin.go`) is extended to include
  `rpm_limit`, `max_concurrent`, `credit_limit_nano_usd`, `models`, and
  `usage_total_nano_usd`, populated in `GetAdminUser`.
- Admin editing reuses the existing `PUT /api/access-keys/:id` control endpoint
  (it already supports `rpm_limit`, `cost_limit_rules`, `filters`, `status`).
  Migration `0023` adds `max_concurrent` to that request/response DTO and to the
  access-key collection projection, so admins can set concurrency too.
- No new admin userdash endpoints are required.

## Backend — gateway enforcement

- **RPM / credit / model:** unchanged existing checks.
- **Concurrent:** new `internal/ratelimit/access_key_concurrency.go`:

```
type AccessKeyConcurrency struct { ... }        // sync.Map keyed by access key ID
func (c *AccessKeyConcurrency) Acquire(id uint, limit int64) (release func(), ok bool)
```

  - `limit <= 0` -> always allowed, no counter entry touched.
  - Increment via `atomic.Int64` per key; over limit -> `ok = false`.
  - Returned `release` decrements exactly once (idempotent guard) and deletes
    zeroed entries.
- Wiring: bind `*ratelimit.AccessKeyConcurrency` as a narrow `gateway` interface
  alongside `AccessKeyRPMLimiter` in `internal/container/container.go:78-81`.
- HTTP hook: acquire immediately after the RPM check at `handler.go:513`;
  store the release func on the request-scoped admission struct and call it in
  the **single existing deferred completion block** (`handler.go:476-489`) so
  release is guaranteed on panic/early return.
- WebSocket hooks: acquire per turn at `websocket_turn.go:161` (beside the RPM
  check), release when the turn completes; mirror at `websocket.go:519`.
- Over limit -> `429` with `Retry-After: 1`, new reason
  `reasonAccessKeyConcurrentLimited` in `internal/gateway/reason.go`.

**Correctness constraint:** concurrency release must run on every exit path. Use
`defer` at the single acquire site; never release in a branch that can be skipped.
The counter is process-local (a multi-instance deployment limits per instance);
this is an accepted ceiling, noted in the code with a `ponytail:` comment and the
upgrade path (shared counter/store) if cross-instance enforcement is required.

## Frontend — `web/src/frontends/modern/`

### Client area

- `api/clientarea.ts`: extend `OwnedKeyView` with the new limit and
  `usage_total_nano_usd` fields; add `updateKeyLimits(client, id, input)` and
  `updateKeyStatus(client, id, status)`.
- `features/clientarea/KeysView.vue`:
  - Usage column: **Total usage** (`usage_total_nano_usd`).
  - Row actions: **Limits** (dialog) and an **Enable/Disable** toggle reflecting
    current status.
  - Limits dialog fields: RPM, Concurrent, Usage credit (USD, converted to
    nano-USD), Models multi-select. Reuse the multi-select/scope pattern and
    `AccessKeyQuotaEditor` presentation conventions from
    `features/access-keys/`.
- Create-key dialog is unchanged (limits are edited after creation).

### Admin

- `api/admin-users.ts`: extend `AdminUserKeyView` with the new fields.
- `features/users/UserDetailView.vue`: owned-keys table gains RPM / Concurrent /
  Credit / Models columns; an **Edit limits** action opens a dialog that calls
  the existing admin `updateAccessKey` (`api/access-keys.ts`) with `rpm_limit`,
  `cost_limit_rules`, `filters.models`, and `max_concurrent`. Reuse
  `AccessKeyQuotaEditor.vue` / `AccessKeyRestrictions.vue` where practical.

### i18n

New keys under `clientarea.keys.*` and `users.detail.*` in `en-US`, `zh-CN`,
`ja-JP` (limit labels, edit dialog, enable/disable, total usage, errors).

## Testing

Go:

- Migration `0023` test: column exists with correct type/default/check;
  `ValidateRecoverable` is idempotent (pre- and post-column).
- `AccessKeyConcurrency` unit test: unlimited passes without state; limit blocks
  the (limit+1)th acquire; `release` frees a slot; release is idempotent;
  zeroed entries are cleaned up.
- Gateway test: request over the concurrency limit -> 429 +
  `reasonAccessKeyConcurrentLimited`; slot is released on completion (a
  subsequent request succeeds).
- Client-area service tests: set limits with ownership enforcement (cannot edit
  another user's key -> 404); re-enable flips status; total-usage aggregate is
  lifetime and owned-key scoped; clearing credit removes the rule; clearing
  models empties the allowlist.
- Admin projection test: `GetAdminUser` returns the new per-key limit fields.

Web:

- `vue-tsc` typecheck, eslint, and production build must pass.
- Page-route manifest test unchanged (no new routes).

Manual Playwright: user sets RPM/concurrent/credit/models -> limits are enforced
-> admin user detail shows the same values -> admin edits -> user's client area
reflects the change; user disables then re-enables a key.

## Error handling

- `400` invalid body/values (negative limit, bad status enum, malformed models).
- `404` unknown key or key not owned by the caller.
- `429` concurrency limit exceeded (gateway).
- Existing errors for RPM (`reasonAccessKeyRateLimited`) and cost quota
  (`reasonAccessKeyCostLimitExceeded`) are unchanged.

## Change-impact surfaces

- Source: `internal/userdash`, `internal/gateway`, `internal/ratelimit`,
  `internal/control` (access-key DTO), `internal/storage` (model, migration),
  `internal/container`, `internal/state` (view field).
- API contract: client-area key responses (usage field change), admin
  `AdminUserKeyView`, admin access-key DTO (`max_concurrent`).
- DB: one additive column + check.
- Frontend: client-area keys view, admin user detail, API clients, i18n.
- Docs: this spec; README/architecture unchanged unless the access-key DTO doc
  lists fields.

## Skipped (add when needed)

- TPM limiting.
- Cross-instance concurrency enforcement.
- Per-model request/token quotas.
- Concurrent-limit persistence across restarts.
