# Design: User Dashboard (Client Area) for gpt-load

Branch: `feat/dashboard-user` · Date: 2026-09-22 · Status: approved

## Goal

Separate user-facing dashboard ("client area") from the admin dashboard. Users can
monitor API-key usage, create API keys (quota-limited), and top up credits. Admins
manage users from a new sidebar group. Users authenticate at `/clientarea` with
email + password (signup included).

## Non-goals (MVP)

Payment gateway integration, email verification, password-reset via email,
multi-level roles, topup vouchers, per-key balance exemptions.

## Decisions (from Q&A)

- Users own their AccessKeys (`sk-gl-...`): `user_id` column; user creates/manages
  keys within a per-user quota set by admin (default 3).
- Credit unit: USD stored as nano-USD (same as `estimated_cost_nano_usd`), shown
  with 6 decimals.
- Deduction model: balance decremented by request cost; **hard block** when
  balance <= 0 (gateway rejects with 402).
- Signup: open, active immediately, balance 0. Admin tops up manually.
- Auth: DB-backed opaque session tokens + bcrypt (security > statelessness, since
  money is involved). Instant revoke on disable/logout.

## Data model — migration `0021_user_dashboard`

New tables:

```
users
  id, email (unique, lowercase), password_hash (bcrypt), status (active|disabled),
  balance_nano_usd BIGINT NOT NULL DEFAULT 0 (transactionally maintained),
  max_api_keys INT NOT NULL DEFAULT 3, created_at, updated_at

user_sessions
  id, user_id (index), token_hash (unique, sha256 hex), expires_at, created_at

user_balance_adjustments
  id, user_id (index), delta_nano_usd, reason (topup|correction),
  admin_note, created_by_admin, created_at
```

New columns:

- `access_keys.user_id` (nullable, indexed). NULL = unowned/admin-direct key.
- `request_logs`, `usage_stats`: unchanged — deduction joins via
  `access_key_id -> user_id`.

Rules:

- Emails stored lowercase, unique.
- One user owns N keys (N <= `users.max_api_keys`).
- No user deletion cascade: admin disables (`status=disabled`) instead; owned keys
  get disabled by the same action; logs are retained.

## Backend — new module `internal/userdash/`

Follows existing `internal/control/` patterns (httproute registration, service
layer, GORM).

### Client-area endpoints (session auth)

| Route | Method | Notes |
|---|---|---|
| `/api/clientarea/signup` | POST | no auth; email+password (min 8); bcrypt default cost; per-IP rate limit |
| `/api/clientarea/login` | POST | no auth; returns opaque token + profile; 7-day expiry |
| `/api/clientarea/logout` | POST | deletes session row |
| `/api/clientarea/me` | GET | profile + balance |
| `/api/clientarea/models` | GET | public model list + prices (reuses landing_public query) |
| `/api/clientarea/keys` | GET/POST | list/create own keys; enforces `max_api_keys`; reuses existing key generator (`sk-gl-`) |
| `/api/clientarea/keys/:id` | DELETE | disable own key |
| `/api/clientarea/usage` | GET | aggregate usage of own keys (reuses usage_query, scoped by user_id) |

### Admin endpoints (existing admin auth)

| Route | Method | Notes |
|---|---|---|
| `/api/admin/users` | GET/POST | list/create users |
| `/api/admin/users/:id` | PATCH | status, `max_api_keys` |
| `/api/admin/users/:id/adjustments` | POST/GET | topup/correction + history |

### Balance accuracy (money-critical)

- **Source of truth**: `users.balance_nano_usd`, maintained only inside
  transactions.
- **Topup/correction**: insert `user_balance_adjustments` row + update balance in
  one DB transaction.
- **Deduction**: performed in the requestlog worker, transactionally and
  idempotently alongside the existing `usage_aggregation_journal` commit path
  (journal is already request-idempotent; deduction executes only on first
  commit). SQL shape: `UPDATE users SET balance_nano_usd = balance_nano_usd - ?
  WHERE id = ?` where user_id resolved from `access_keys.user_id`.
- **Reconciliation**: balance must always equal
  `SUM(adjustments.delta) - SUM(deducted costs)`; a test asserts this invariant.
- **Gateway hard block**: `internal/gateway/auth.go` — after key match, if key has
  `user_id`, check in-memory balance cache (TTL 30s, invalidated on adjustment).
  Balance <= 0 -> 402 Payment Required JSON. The cache is a *gate only*; accuracy
  comes from transactional deduction. Max overdraw = in-flight requests at the
  moment balance hits zero.
- **Disabling a user**: effective immediately for sessions (session rows
  deleted) and within cache TTL for the gateway gate.

### Security

- bcrypt password hashing (default cost).
- Session token: 256-bit random, only sha256 hash stored; compared by hash
  lookup.
- Login/signup rate-limited per IP (existing `auth_limiter` pattern).
- Constant-time comparisons where applicable; no secrets logged.

## Frontend — `web/src/frontends/modern/`

### Router zones

| Zone | Paths | Layout | Guard |
|---|---|---|---|
| Public client area | `/clientarea` | `publicLayout: 'clientarea'` (landing navbar/footer) | redirect to dashboard if logged in |
| Private client area | `/clientarea/dashboard`, `/clientarea/keys`, `/clientarea/usage`, `/clientarea/models` | new user layout (own sidebar) | user-session guard; anonymous -> render 404 inline (existing AuthGate pattern, no path leakage) |
| Admin | `/users`, `/users/:id` | existing admin layout | existing admin guard |

Page-route manifests (`internal/webui/*_page_routes.json` +
`page_routes_test.go`) updated together with new routes.

### Admin pages

- Sidebar: new group **User Management** -> Users list (email, status, balance,
  key count, quota, last login).
- `/users/:id` detail: profile; balance with **Topup/Correction** dialog (USD
  amount + note); owned API keys table; adjustment history; disable/enable; edit
  quota.
- Reuse existing `ui/App*` components (DataTable, Dialog, forms).

### `/clientarea` page (login + signup)

- Tabbed sign-in / sign-up card styled like the Tokenizer landing.
- On success: token stored in `localStorage` (`modern.userSession`), redirect to
  `/clientarea/dashboard`.

### User dashboard (own layout: Overview, API Keys, Usage, Models)

- **Overview**: balance card (6-decimal USD), 30-day usage chart (reuse
  `features/usage` patterns), key status summary.
- **API Keys**: own keys (name, prefix, status, limits, 30-day usage); Create key
  (disabled + message when quota reached); one-time reveal dialog on create;
  disable action.
- **Usage**: aggregate table by model/key + period (scoped user usage query).
- **Models**: model + price cards (reuse `LandingModelCard`).

### API clients & i18n

- New `api/clientarea.ts` and `api/users.ts`; user-token client separate from
  admin `api-client.ts` (different storage key).
- New i18n keys in per-feature locale files (en-US first).

## Testing

- Go: migration test; userdash service tests (signup/login/session lifecycle);
  adjustment+balance transaction test; deduction idempotency test (journal
  replay does not double-deduct); reconciliation invariant test; gateway 402
  test with cache TTL.
- Web: existing `vue-tsc` + eslint + build must pass; page-route manifest test
  updated.
- Manual Playwright flow: signup -> login -> create key (quota enforcement) ->
  admin topup -> usage appears -> balance deducts after proxy request -> 402 at
  zero balance -> disable user kills session.

## Error handling

- 401 (invalid/expired session), 402 (insufficient balance, gateway), 403
  (disabled user or key not owned), 409 (email taken, quota reached), 429 (rate
  limited). Consistent with existing error envelope.

## Skipped (add when needed)

- Email flows, payment gateways, vouchers, per-key exemption from hard block,
  self-service password reset.
