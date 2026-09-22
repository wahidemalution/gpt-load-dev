# User Dashboard (Client Area) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate user-facing dashboard (client area at `/clientarea`) from the admin dashboard: users sign up/sign in, own API keys within a quota, monitor usage, and have a nano-USD balance that is deducted per request and hard-blocks the gateway at zero.

**Architecture:** New Go module `internal/userdash` (sessions, users, balance ledger) + new HTTP modules (client-area session auth, admin users). Balance deduction piggybacks on the requestlog worker's idempotent journal-commit transaction. Gateway rejects keys whose owner has balance <= 0 via a 30s-TTL cache gate. Frontend adds a third zone to the modern SPA (`publicLayout: 'clientarea'` + user layout) and an admin "User Management" sidebar group.

**Tech Stack:** Go 1.x + GORM (SQLite/MySQL/Postgres), gin + httproute registry, golang.org/x/crypto/bcrypt; Vue 3 + vue-router + TanStack vue-query + reka-ui `App*` components + Tailwind 4 + vue-i18n.

**Spec:** `docs/superpowers/specs/2026-09-22-user-dashboard-design.md`

## Global Constraints

- Never break existing behavior: admin (AUTH_KEY) and access-key auth flows unchanged.
- Money paths must be transactional and idempotent; balance invariant: `balance == SUM(adjustments) - SUM(deducted costs)`.
- Build web with `pnpm --dir web run build` (NOT `make build` — corepack pnpm version mismatch).
- Go tests: `go test ./...`; web checks: `pnpm --dir web run build` (runs vue-tsc + eslint via build pipeline; if a lint script exists use it too).
- Follow existing patterns: migrations in `internal/storage/migrations/` registered in `internal/storage/migration.go`; HTTP modules in `internal/platform/httproute`; errors via `app_errors`; responses via `internal/platform/response`.
- i18n: every user-facing string needs keys in `web/src/frontends/modern/i18n/locales/` (en-US.ts mandatory, id-ID, zh-CN, ja-JP follow).
- Page routes: new SPA paths must be added to `internal/webui/modern_page_routes.json` AND `internal/webui/page_routes_test.go` fixtures.
- Do not log secrets (passwords, session tokens, plaintext keys).
- Commits per task, conventional-commit style (`feat(control): ...`, `feat(web): ...`).

---

### Task 1: Storage models + migration 0021

**Files:**
- Create: `internal/storage/migrations/0021_user_dashboard.go`
- Modify: `internal/storage/models/user.go` (new file)
- Modify: `internal/storage/models/access_key.go` (add UserID column)
- Modify: `internal/storage/migration.go` (register 0021)
- Test: `internal/storage/migrations/0021_user_dashboard_test.go`

**Interfaces:**
- Produces: `models.User`, `models.UserSession`, `models.UserBalanceAdjustment` structs; `models.AccessKey.UserID *uint`.

- [ ] **Step 1: Write the failing test**

Create `internal/storage/migrations/0021_user_dashboard_test.go` modeled on `0001_initial_test.go` / `0020` test style: open an in-memory SQLite via the storage test helper (see how `migration_test.go` helpers open `gorm.Open(sqlite...)` in existing migration tests), run `Up0021`, assert tables `users`, `user_sessions`, `user_balance_adjustments` exist, `access_keys` gains nullable `user_id` column, then run `Validate0021` and assert it passes; run `Up0021` twice (idempotent re-run) and assert no error.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/migrations/ -run TestUserDashboard -v`
Expected: FAIL (Up0021 undefined).

- [ ] **Step 3: Write models + migration**

`internal/storage/models/user.go`:

```go
package models

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

// User is a client-area account with a transactionally-maintained nano-USD balance.
type User struct {
	ID              uint   `gorm:"primaryKey;autoIncrement"`
	Email           string `gorm:"type:varchar(255);not null;uniqueIndex"`
	PasswordHash    string `gorm:"type:varchar(255);not null"`
	Status          string `gorm:"type:varchar(32);not null;default:'active';check:chk_user_status,status IN ('active','disabled')"`
	BalanceNanoUSD  int64  `gorm:"column:balance_nano_usd;not null;default:0"`
	MaxAPIKeys      int    `gorm:"column:max_api_keys;not null;default:3;check:chk_user_max_api_keys,max_api_keys >= 0"`
	CreatedAtMS     int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:chk_user_created_at,created_at_ms >= 0"`
	UpdatedAtMS     int64  `gorm:"column:updated_at_ms;not null;autoUpdateTime:milli;check:chk_user_updated_at,updated_at_ms >= 0"`
}

func (User) TableName() string { return "users" }

// UserSession stores only the sha256 of the opaque bearer token.
type UserSession struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	UserID    uint   `gorm:"not null;index"`
	TokenHash string `gorm:"type:varchar(64);not null;uniqueIndex"`
	ExpiresAtMS int64 `gorm:"column:expires_at_ms;not null;check:chk_user_session_expires,expires_at_ms >= 0"`
	CreatedAtMS int64 `gorm:"column:created_at_ms;not null;autoCreateTime:milli"`
}

func (UserSession) TableName() string { return "user_sessions" }

// UserBalanceAdjustment is the ledger of manual balance changes (topup/correction).
type UserBalanceAdjustment struct {
	ID             uint   `gorm:"primaryKey;autoIncrement"`
	UserID         uint   `gorm:"not null;index"`
	DeltaNanoUSD   int64  `gorm:"column:delta_nano_usd;not null"`
	Reason         string `gorm:"type:varchar(32);not null;check:chk_user_adj_reason,reason IN ('topup','correction')"`
	AdminNote      string `gorm:"type:varchar(500);not null;default:''"`
	CreatedByAdmin string `gorm:"type:varchar(255);not null;default:''"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms;not null;autoCreateTime:milli;check:chk_user_adj_created,created_at_ms >= 0"`
}

func (UserBalanceAdjustment) TableName() string { return "user_balance_adjustments" }
```

In `internal/storage/models/access_key.go` add to the `AccessKey` struct:

```go
	UserID *uint `gorm:"index"`
```

`internal/storage/migrations/0021_user_dashboard.go` — follow the exact pattern of `0020_client_model_overrides.go`: define local mirror structs (`user0021` etc. with matching gorm tags), `Up0021` creates tables via `db.Migrator().CreateTable` for the three new tables and `db.Migrator().AddColumn(&accessKey0021{}, "user_id")` when missing, guard with `HasColumn`; then `Validate0021` checks columns exist with expected nullability (follow 0020's ColumnTypes loop style). Export `ID0021 = "0021_user_dashboard"`, `Up0021`, `Validate0021`, `ValidateRecoverable0021`.

Register in `internal/storage/migration.go` after the 0020 entry:

```go
	{ID: migrationfiles.ID0021, Up: migrationfiles.Up0021, Validate: migrationfiles.Validate0021, ValidateRecoverable: migrationfiles.ValidateRecoverable0021},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/... -v`
Expected: PASS (existing migration ledger test counts migrations — update its expected count if it asserts one).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/ && git commit -m "feat(storage): add users, sessions, balance ledger and access_keys.user_id (migration 0021)"
```

---

### Task 2: userdash service — sessions, signup, login

**Files:**
- Create: `internal/userdash/service.go`
- Create: `internal/userdash/service_test.go`

**Interfaces:**
- Consumes: `models.User`, `models.UserSession` (Task 1); `*gorm.DB`.
- Produces:
  - `type Service struct` + `func NewService(db *gorm.DB, random io.Reader, now func() time.Time) *Service`
  - `type Profile struct { ID uint; Email string; Status string; BalanceNanoUSD int64; MaxAPIKeys int; CreatedAtMS int64 }`
  - `(s *Service) Signup(ctx, email, password string) (Profile, error)`
  - `(s *Service) Login(ctx, email, password string) (token string, profile Profile, expiresAt time.Time, err error)`
  - `(s *Service) Authenticate(ctx, token string) (Profile, error)` — returns `ErrUnauthenticated` wrapping for invalid/expired/disabled
  - `(s *Service) Logout(ctx, token string) error`
  - Errors: `var ErrEmailTaken, ErrInvalidCredentials, ErrUnauthenticated, ErrUserDisabled, ErrValidation = ...` (wrap or alias `app_errors` styles; plain sentinel errors are fine here, handlers map them to HTTP).
  - Session TTL: 7 days; token = 32 random bytes hex; stored sha256-hashed.

- [ ] **Step 1: Write the failing test**

`service_test.go` with in-memory SQLite (open gorm sqlite as in requestlog tests). Cover: signup creates active user balance 0, duplicate email -> ErrEmailTaken, short password (<8) -> ErrValidation; login valid -> non-empty token + profile, wrong password -> ErrInvalidCredentials; Authenticate round-trip works, garbage token -> ErrUnauthenticated, expired session -> ErrUnauthenticated, disabled user -> ErrUserDisabled; Logout deletes row so re-authenticate fails; tokens are not stored in plaintext (row token_hash != token, len 64 hex). Inject fake `random` (deterministic) and `now` for expiry test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userdash/ -v`
Expected: FAIL (package missing).

- [ ] **Step 3: Implement**

```go
package userdash

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

const (
	sessionTTL      = 7 * 24 * time.Hour
	minPasswordLen  = 8
	bcryptCost      = bcrypt.DefaultCost
)

var (
	ErrEmailTaken          = errors.New("userdash: email already taken")
	ErrInvalidCredentials  = errors.New("userdash: invalid credentials")
	ErrUnauthenticated     = errors.New("userdash: unauthenticated")
	ErrUserDisabled        = errors.New("userdash: user disabled")
	ErrValidation          = errors.New("userdash: validation failed")
)

type Service struct {
	db     *gorm.DB
	random io.Reader
	now    func() time.Time
}

func NewService(db *gorm.DB, random io.Reader, now func() time.Time) *Service {
	if random == nil { random = rand.Reader }
	if now == nil { now = time.Now }
	return &Service{db: db, random: random, now: now}
}

type Profile struct {
	ID             uint   `json:"id"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	BalanceNanoUSD int64  `json:"balance_nano_usd"`
	MaxAPIKeys     int    `json:"max_api_keys"`
	CreatedAtMS    int64  `json:"created_at_ms"`
}
```

Implement:
- `normalizeEmail` (trim, lowercase; require one `@`, no spaces) and `validatePassword` (>= minPasswordLen, must contain letter + digit) -> ErrValidation.
- `Signup`: hash password with bcrypt, `tx.Create(&models.User{...})` with `Email: normalized`, catch unique violation via `errors.Is(err, gorm.ErrDuplicatedKey)` (GORM translates for sqlite/mysql/postgres when `TranslateError: true` is enabled in the existing storage open config — check `internal/storage/database.go`; if not enabled, match on driver string containing "UNIQUE constraint failed" / "Duplicate entry" like existing code does — search `ParseDBError` in `internal/platform/errors` for the established pattern) -> ErrEmailTaken. Return `profileFromRow`.
- `Login`: find user by email (case-insensitive via normalized), `bcrypt.CompareHashAndPassword`; on mismatch return ErrInvalidCredentials **after** doing a dummy compare against a fixed hash to keep timing uniform (constant-time discipline); disabled -> ErrUserDisabled; else mint session.
- `mintSession`: 32 bytes from `s.random` -> hex token; store `sha256 hex(token)` + `ExpiresAtMS`; return plaintext token.
- `Authenticate`: hash token, load session by `token_hash`, check `ExpiresAtMS > now.UnixMilli()`, load user, check `Status == active` (else ErrUserDisabled), return profile. Use `subtle.ConstantTimeCompare` when comparing hashes (hash lookup by unique index is fine; keep compare for defense-in-depth).
- `Logout`: `DELETE FROM user_sessions WHERE token_hash = ?` — always nil error.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userdash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userdash/ && git commit -m "feat(userdash): user signup/login with bcrypt and opaque DB sessions"
```

---

### Task 3: Balance ledger — adjustments, atomic deduction, reconciliation

**Files:**
- Modify: `internal/userdash/service.go`
- Create: `internal/userdash/balance.go`
- Test: `internal/userdash/balance_test.go`

**Interfaces:**
- Produces:
  - `(s *Service) AdjustBalance(ctx, userID uint, deltaNanoUSD int64, reason, adminNote, createdByAdmin string) (Profile, error)` — ledger insert + balance update in ONE transaction.
  - `(s *Service) ListAdjustments(ctx, userID uint, limit, offset int) ([]models.UserBalanceAdjustment, error)`
  - `(s *Service) GetBalance(ctx, userID uint) (int64, error)`
  - `func DeductCosts(ctx context.Context, tx *gorm.DB, costs []AccessKeyCost) error` — pure transactional helper used by requestlog worker (Task 7):
    - `type AccessKeyCost struct { AccessKeyID uint; CostNanoUSD int64 }`
  - `func ReconcileBalance(ctx, db *gorm.DB, userID uint) error` — test/ops helper asserting invariant; returns error on drift.

- [ ] **Step 1: Write the failing test**

`balance_test.go`: seed user balance 0. (a) AdjustBalance(+1000) -> balance 1000 and one ledger row; AdjustBalance(-300) -> 700, two rows. (b) `DeductCosts` with costs summing 250 -> balance 450; call `DeductCosts` again with SAME rows on a rolled-back tx then commit once — assert balance deducted once (idempotency lives in the caller's journal-applied guard, this test just proves the arithmetic is additive and correct inside the tx); assert negative balance is allowed (overdraw in-flight) but balance never goes below `math.MinInt64` overflow (checked add — reuse `pricing.CheckedAddNanoUSD`-style pattern: subtract via `pricing.NanoUSD` checked ops from `internal/pricing`). (c) `ReconcileBalance` passes after the above; corrupt balance manually -> returns error. (d) Adjustment with unknown userID -> error. (e) Concurrency smoke: 10 goroutines each AdjustBalance(+1) -> balance == 10 (SQLite serializes; on sqlite in-memory use a mutex-wrapped db if flaky — follow existing concurrency test patterns in `internal/storage`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userdash/ -run TestBalance -v`
Expected: FAIL (AdjustBalance undefined).

- [ ] **Step 3: Implement**

`balance.go`:

```go
func (s *Service) AdjustBalance(ctx context.Context, userID uint, deltaNanoUSD int64, reason, adminNote, createdByAdmin string) (Profile, error) {
	var profile Profile
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.User{}).Where("id = ?", userID).
			Update("balance_nano_usd", gorm.Expr("balance_nano_usd + ?", deltaNanoUSD))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		row := models.UserBalanceAdjustment{
			UserID: userID, DeltaNanoUSD: deltaNanoUSD, Reason: reason,
			AdminNote: adminNote, CreatedByAdmin: createdByAdmin,
			CreatedAtMS: s.now().UnixMilli(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var user models.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}
		profile = profileFromRow(&user)
		return nil
	})
	return profile, err
}

// DeductCosts subtracts per-key request costs from owner balances. The caller
// guarantees idempotency (only invoked on first journal commit).
func DeductCosts(ctx context.Context, tx *gorm.DB, costs []AccessKeyCost) error {
	perUser := make(map[uint]int64, len(costs))
	keyOwner := make(map[uint]uint, len(costs))
	var keys []models.AccessKey
	ids := make([]uint, 0, len(costs))
	for _, cost := range costs {
		ids = append(ids, cost.AccessKeyID)
	}
	if err := tx.WithContext(ctx).Where("id IN ? AND user_id IS NOT NULL", ids).Find(&keys).Error; err != nil {
		return err
	}
	for _, key := range keys {
		keyOwner[key.ID] = *key.UserID
	}
	for _, cost := range costs {
		userID, ok := keyOwner[cost.AccessKeyID]
		if !ok {
			continue
		}
		perUser[userID] += cost.CostNanoUSD // caller pre-checked overflow on journals
	}
	for userID, nano := range perUser {
		if nano == 0 {
			continue
		}
		if err := tx.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).
			Update("balance_nano_usd", gorm.Expr("balance_nano_usd - ?", nano)).Error; err != nil {
			return err
		}
	}
	return nil
}
```

`ReconcileBalance`: compute `expected := SUM(adjustments) - SUM(deducted)` where deducted = requestlog costs joined `request_logs -> access_keys(user_id = user)` using `request_logs.estimated_cost_nano_usd` (query `SELECT COALESCE(SUM(r.estimated_cost_nano_usd),0) FROM request_logs r JOIN access_keys k ON k.id = r.access_key_id WHERE k.user_id = ?`); compare to `users.balance_nano_usd`; mismatch -> descriptive error.

Use checked arithmetic with `pricing.NanoUSD`/`pricing.CheckedAddNanoUSD` where overflow is plausible (adjustment deltas come from admin input).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userdash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userdash/ && git commit -m "feat(userdash): transactional balance ledger, atomic deduction, reconciliation"
```

---

### Task 4: Client-area HTTP module (signup/login/logout/me/models)

**Files:**
- Create: `internal/userdash/http_module.go`
- Create: `internal/userdash/http_test.go`
- Modify: `internal/container/container.go` (wire module + service)

**Interfaces:**
- Consumes: `userdash.Service` (Tasks 2-3); `httproute.Module` pattern (see `LandingPublicHTTPModule` in `internal/control/landing_public.go`); per-IP limiter pattern from `internal/control/auth_limiter.go`.
- Produces:
  - `type HTTPModule struct` + `func NewHTTPModule(service *Service, logger ...) *HTTPModule`
  - `(m *HTTPModule) Module() httproute.Module` — `Auth: httproute.AuthNone`, `Prefix: "/api/clientarea"`, with a `SessionAuth` gin middleware inside the module chain that reads `Authorization: Bearer`, calls `service.Authenticate`, stores profile in gin context key `userdash.ProfileContextKey`, aborts 401 on failure.
  - Routes: `POST /signup`, `POST /login`, `POST /logout` (session), `GET /me` (session), `GET /models` (session; reuses landing public models query — extract the models query from `internal/control/landing_public.go` into a shared helper or duplicate the small query in userdash importing `control`'s exported function if one exists; prefer exporting `func (s *Service) PublicModels(ctx)` from control and calling it via a function field injected in container to avoid import cycles).
  - Rate limit: reuse `authFailureLimiter` semantics — copy minimal per-IP limiter into userdash (login+signup share one limiter, 10 failures/15min).

- [ ] **Step 1: Write the failing test**

`http_test.go`: build registry from the module (`httproute.NewRegistry(m.Module())` as in `landing_public_test.go`), serve gin, then:
- POST /signup valid -> 200/201 with `{id,email,balance_nano_usd:0}`; duplicate -> 409; bad password -> 400.
- POST /login valid -> 200 with `{token, profile}`; wrong password -> 401 (not 500).
- GET /me without token -> 401; with token -> 200 profile.
- POST /logout with token -> 204; then GET /me -> 401.
- GET /models with token -> 200 array (seed a group/model row or assert empty array shape).
- Rate limit: 11 failed logins from same IP -> 429.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userdash/ -run TestHTTP -v`
Expected: FAIL (NewHTTPModule undefined).

- [ ] **Step 3: Implement**

Follow `internal/platform/response` helpers for JSON envelopes (read `landing_public.go` handler for exact response shape helpers). Errors map: ErrValidation->400, ErrEmailTaken->409, ErrInvalidCredentials->401, ErrUnauthenticated->401, ErrUserDisabled->403, rate-limit->429. Session token returned as `{"token": "...", "profile": {...}, "expires_at_ms": ...}`. Never log tokens/passwords.

Wiring in `internal/container/container.go` (follow how `controlServer.LandingPublicHTTPModule()` is registered around line 330): construct `userdash.NewService(db, nil, nil)`, inject into `userdash.NewHTTPModule(...)`, add `userdashModule.Module()` to `httproute.NewRegistry(...)`. For models reuse: add an injectable `modelsProvider func(ctx) ([]ModelEntry, error)` field; in container pass a closure that calls the existing landing models query (export it from control if needed: `controlServer.PublicLandingModels(ctx)`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userdash/ ./internal/container/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userdash/ internal/container/ && git commit -m "feat(userdash): client-area HTTP module with session auth and rate limiting"
```

---

### Task 5: User-owned API keys (quota-enforced) + usage endpoints

**Files:**
- Create: `internal/userdash/keys.go`
- Create: `internal/userdash/usage.go`
- Test: `internal/userdash/keys_test.go`

**Interfaces:**
- Consumes: `models.AccessKey.UserID` (Task 1); key credential generation from `internal/control` — export a minimal helper from control `func (s *Service) GenerateAccessKeyCredentialForUser()` OR (simpler, recommended) move/duplicate the 16-random-bytes + `sk-gl-` + hex + encryption flow: userdash gets injected `encryptor` (encryption.Service) + `random io.Reader` and implements `newOwnedKeyRow` locally (see `internal/control/access_keys.go:249-284` for the exact flow including hash computation via the shared hasher — find the hasher type in `internal/control` and mirror it; the hash function is likely in `internal/platform/utils` or encryption service).
- Produces:
  - `(m *HTTPModule) registerKeyRoutes(...)` adds: `GET /keys`, `POST /keys`, `DELETE /keys/:id` (all session-auth).
  - `type OwnedKeyView struct { ID uint; Name string; KeyPrefix string; KeySuffix string; Status string; CreatedAtMS int64; LastRequestAtMS *int64; Usage30dNanoUSD int64; RequestCount30d int64 }`
  - `POST /keys` body `{name}`; returns `{key: "sk-gl-...", profile: OwnedKeyView}` (plaintext only once).
  - Usage: `GET /usage?from_ms&to_ms&grouping=model|key` -> reuse `internal/requestlog` usage query functions scoped by `access_key_id IN (owned ids)` — inject a `usageReader` function from container (control already exposes `UsageStatReader`).

- [ ] **Step 1: Write the failing test**

Seed user (max_api_keys=2) + 2 owned keys + 1 unowned key. Assert: GET /keys lists only owned; POST /keys creates (plaintext starts `sk-gl-`, row has user_id, hash stored not plaintext); third create -> 409 quota; DELETE own key -> 200 and key status disabled; DELETE unowned key id -> 404 (not 403 — don't leak existence); GET /usage returns aggregates only from owned keys (seed request_logs/usage_stats rows for owned + unowned, assert sums).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userdash/ -run TestKeys -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Quota check inside the create transaction: `SELECT COUNT(*) FROM access_keys WHERE user_id = ?` (count all, including disabled) >= `users.max_api_keys` -> ErrQuotaReached (add sentinel; handler maps 409). Create row with `Status: active`, `KeyValue` encrypted via injected encryptor (mirror control's encrypt-at-rest), `KeyHash` via the same hash function control uses (grep `KeyHash` assignment in `internal/control/access_keys.go`). DELETE = status flip to `disabled` (keep logs referential). Usage: parameterize the existing usage query with an `access_key_id IN (...)` filter — if the existing reader doesn't accept key filters, add a small dedicated query in `userdash/usage.go` against `usage_stats` joined to owned keys (simplest: `SELECT model, SUM(request_count), SUM(estimated_cost_nano_usd) ... WHERE access_key_id IN ? AND bucket_start_ms BETWEEN ? AND ? GROUP BY model`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userdash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userdash/ && git commit -m "feat(userdash): quota-enforced user-owned API keys and scoped usage"
```

---

### Task 6: Admin users endpoints

**Files:**
- Create: `internal/userdash/admin_http.go`
- Test: `internal/userdash/admin_http_test.go`

**Interfaces:**
- Consumes: existing admin `httproute.AuthControl` module pattern (control `HTTPModule()`); userdash Service.
- Produces: new routes registered in the EXISTING control module (`internal/control/http_routes.go`) OR a sibling module with `Auth: httproute.AuthControl, Prefix: "/api/admin"` — choose the sibling module `internal/userdash/admin_http.go` returning `httproute.Module` and register in container. Routes:
  - `GET /api/admin/users?search=&page=&page_size=` -> `{items: [AdminUserView], total}`
  - `POST /api/admin/users` `{email, password?, max_api_keys?, balance_nano_usd?}` (admin sets initial)
  - `PATCH /api/admin/users/:id` `{status?, max_api_keys?}` — disabling deletes all `user_sessions` rows for the user in the same tx.
  - `POST /api/admin/users/:id/adjustments` `{delta_nano_usd, reason, note}` -> AdjustBalance
  - `GET /api/admin/users/:id/adjustments?page=`
  - `GET /api/admin/users/:id` -> full view incl. owned keys list (id, name, prefix, suffix, status) + counts.
- `AdminUserView`: `{id, email, status, balance_nano_usd, max_api_keys, key_count, created_at_ms, last_login_at_ms}` (last_login = MAX(user_sessions.created_at_ms) or 0).

- [ ] **Step 1: Write the failing test**

Admin-auth setup: mirror an existing control route test that stubs `Authenticate` (see `server_test.go` auth stubbing pattern). Tests: list users paginated; create user with balance; patch status disabled -> sessions table emptied for that user; post adjustment updates balance + ledger; invalid reason -> 400.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userdash/ -run TestAdmin -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Straight handlers over Service methods. Disabling user: within one tx, `users.status='disabled'` + `DELETE FROM user_sessions WHERE user_id = ?`. Re-enabling does NOT restore sessions (user must log in again). Also disable owned keys? No — spec: owned keys disabled by the same action: also `UPDATE access_keys SET status='disabled' WHERE user_id = ? AND status='active'` in that tx (per spec "owned keys get disabled by the same action").

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userdash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/userdash/ && git commit -m "feat(userdash): admin user management endpoints with topup ledger"
```

---

### Task 7: Gateway hard block (402) + requestlog deduction

**Files:**
- Modify: `internal/state/snapshot.go` (AccessKeyConfig + AccessKeyView gain `UserID uint`)
- Modify: `internal/state/loader/loader.go:801` (map `row.UserID` into AccessKeyConfig)
- Modify: `internal/gateway/auth.go` + `internal/gateway/reason.go` + `internal/gateway/http_routes.go`
- Modify: `internal/requestlog/worker.go` (deduct in journal-commit tx)
- Modify: `internal/container/container.go` (inject balance provider into gateway + requestlog)
- Create: `internal/userdash/balance_gate.go`
- Test: `internal/gateway/user_balance_test.go`, `internal/requestlog/user_deduct_test.go`

**Interfaces:**
- Consumes: `userdash.Service` (Task 3); `models.AccessKey.UserID`.
- Produces:
  - `type BalanceGate struct` + `func NewBalanceGate(provider func(ctx context.Context, userIDs []uint) (map[uint]int64, error), ttl time.Duration) *BalanceGate` with `(g *BalanceGate) Allow(userID uint) bool` and `(g *BalanceGate) Invalidate()`. In-memory map+RWMutex, TTL 30s, single refresh query batched per call window.
  - Gateway: new failure reason `accessKeyAuthFailureNoBalance` + `reasonInsufficientBalance = {Status: 402, Code: "insufficient_balance", Message: "Account balance is exhausted."}`; `AccessKeyView.UserID uint` (0 = unowned).
  - requestlog: writer calls `userdash.DeductCosts(tx, costs)` inside the existing journal transaction after `applyUsageJournalBatch` succeeds — only for rows in THIS commit (idempotency from `prepareNewRequestLogRows` already filtering existing IDs + journal applied-guard).

- [ ] **Step 1: Write the failing tests**

Gateway test (`user_balance_test.go`, mirror `auth_test.go` snapshot-building helpers): access key with `UserID: 7`; BalanceGate provider returns `{7: 0}` -> `authenticate` returns `(key, false, accessKeyAuthFailureNoBalance)`; provider `{7: 500}` -> ok; key without user -> unaffected. HTTP-level test: `writeReason` produces 402 with code `insufficient_balance`.
requestlog test (`user_deduct_test.go`): seed user balance 1000, owned key, write one request-log batch with `EstimatedCostNanoUSD: 400` through the real `gormBatchWriter.WriteBatch` path; assert balance 600 and usage journal applied; run WriteBatch AGAIN with the same rows (simulating retry) -> balance still 600 (deduction skipped because rows already exist). Unowned key cost -> balance unchanged.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/gateway/ -run TestUserBalance -v && go test ./internal/requestlog/ -run TestUserDeduct -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

1. `snapshot.go`: add `UserID uint` to both `AccessKeyConfig` and `AccessKeyView`; `newAccessKeyView` copies it.
2. `loader.go`: add `UserID: row.UserID` deref (nil-safe: `derefUserID(row.UserID)`).
3. `balance_gate.go` in userdash as above.
4. Gateway `auth.go`: after CIDR check, `if accessKey.UserID != 0 && balanceGate != nil && !balanceGate.Allow(accessKey.UserID) { return accessKey, false, accessKeyAuthFailureNoBalance }`. Thread `*userdash.BalanceGate` through `authenticate` via the Handler (add field, set in container; keep nil-tolerant so existing tests pass without it). `http_routes.go`: map the new failure reason to `reasonInsufficientBalance` in `authenticateDataPlaneRequest`.
5. `worker.go`: inside the `dbtx.Run` transaction (after `writeRequestLogBatch` succeeds), build `costs []userdash.AccessKeyCost` from `newRows` (`AccessKeyID`, `EstimatedCostNanoUSD`) and call the injected `userDeductor func(ctx, tx, costs) error` (nil = disabled; container wires `userdash.DeductCosts`). Place AFTER journal staging so a deduction failure rolls back the whole batch (same-transaction atomicity).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gateway/ ./internal/requestlog/ ./internal/state/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ && git commit -m "feat(gateway): hard-block zero-balance user keys (402) and deduct costs in requestlog tx"
```

---

### Task 8: Frontend — user session store, API client, router zone, page routes

**Files:**
- Create: `web/src/frontends/modern/features/clientarea/user-session.ts`
- Create: `web/src/frontends/modern/api/clientarea.ts`
- Create: `web/src/frontends/modern/api/admin-users.ts`
- Modify: `web/src/frontends/modern/router.ts`
- Modify: `App.vue` (layout switch for `publicLayout: 'clientarea'` + `userLayout` meta)
- Modify: `internal/webui/modern_page_routes.json` + `internal/webui/page_routes_test.go`

**Interfaces:**
- Produces:
  - `user-session.ts`: `useUserSession()` — `getToken()`, `getProfile()`, `setSession(token, profile)`, `clear()`, `isExpired()` (stores `{token, profile, expiresAtMS}` in `localStorage['modern.userSession']`).
  - `api/clientarea.ts`: `createClientAreaClient()` — like `createSessionApiClient` but reads the user token; functions: `signup`, `login`, `logout`, `me`, `listModels`, `listKeys`, `createKey`, `deleteKey`, `usage(params)`.
  - `api/admin-users.ts`: admin-plane client (existing admin api-client) — `listUsers`, `createUser`, `updateUser`, `listAdjustments`, `createAdjustment`, `getUser`.
  - Router routes:
    - `path: '/clientarea'`, name `modern-clientarea-home`, component `features/clientarea/ClientAreaAuthView.vue`, `meta: { titleKey: 'clientarea.auth.title', publicLayout: 'clientarea' }`
    - `/clientarea/dashboard|keys|usage|models` -> `features/clientarea/ClientAreaShell.vue` with child routes, `meta: { requiresUserAuth: true, publicLayout: 'clientarea' }`
  - `page_routes.json` routes array gains: `{"name":"clientarea-auth","path":"/clientarea"}`, `{"name":"clientarea-dashboard","path":"/clientarea/dashboard"}`, `{"name":"clientarea-keys","path":"/clientarea/keys"}`, `{"name":"clientarea-usage","path":"/clientarea/usage"}`, `{"name":"clientarea-models","path":"/clientarea/models"}`, `{"name":"admin-users","path":"/users"}`, `{"name":"admin-user-detail","path":"/users/:id"}` — AND update `page_routes_test.go` expected fixture list accordingly (read the test to see the exact expected slice).

- [ ] **Step 1: Write the failing test**

`page_routes_test.go` update is the test: run `go test ./internal/webui/ -v` after editing fixtures.
Web side: no unit test infra for router beyond build — rely on vue-tsc + build.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/webui/ -v`
Expected: FAIL (manifest/test mismatch).

- [ ] **Step 3: Implement**

Model `user-session.ts` on existing `features/auth/auth-session.ts` (read it first and mirror its revision-guard pattern). `App.vue`: extend the existing `publicLayout` switch to render a new `ClientAreaPublicLayout` (landing navbar + footer, same components as landing layout) when `meta.publicLayout === 'clientarea'`, and the new `ClientAreaShell` handles its own layout for child routes. Router guard: in `beforeEach`, `if (to.meta.requiresUserAuth && !userSession.hasSession()) return true` (let the shell render 404-style inline like AuthGate does — no path leakage, consistent with the admin decision).

- [ ] **Step 4: Verify**

Run: `go test ./internal/webui/ -v && pnpm --dir web run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/ internal/webui/ && git commit -m "feat(web): client-area session store, API clients, router zone and page routes"
```

---

### Task 9: Frontend — /clientarea auth page (login + signup tabs)

**Files:**
- Create: `web/src/frontends/modern/features/clientarea/ClientAreaAuthView.vue`
- Create: `web/src/frontends/modern/features/clientarea/ClientAreaPublicLayout.vue`

**Interfaces:**
- Consumes: `api/clientarea.ts` (Task 8), landing design tokens/components (`LandingNavbar` etc.).
- Produces: routed view for `modern-clientarea-home`.

- [ ] **Step 1: Implement**

Two-tab card (Sign in / Sign up) styled like `LoginView.vue` + landing tokens. Sign up fields: email, password, confirm password (client check), submit -> `signup()` then auto-login (`login()`) then `router.push({name:'modern-clientarea-dashboard'})`. Sign in: email+password -> store session -> same redirect. Error surface: map 409 -> "email already registered", 401 -> "wrong email or password", 429 -> "too many attempts". If already logged in on mount -> redirect to dashboard. i18n keys under `clientarea.*` (en-US first, then id-ID, zh-CN, ja-JP minimal translations).

- [ ] **Step 2: Verify**

Run: `pnpm --dir web run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/ && git commit -m "feat(web): client-area login/signup page"
```

---

### Task 10: Frontend — user dashboard shell + Overview/Keys/Usage/Models

**Files:**
- Create: `web/src/frontends/modern/features/clientarea/ClientAreaShell.vue` (sidebar layout + inline-404 when anonymous)
- Create: `features/clientarea/OverviewView.vue`, `features/clientarea/KeysView.vue`, `features/clientarea/UsageView.vue`, `features/clientarea/ModelsView.vue`
- Modify: i18n locale files

**Interfaces:**
- Consumes: Task 8 clients; `ui/App*` components; `LandingModelCard` for models page.

- [ ] **Step 1: Implement**

Shell: left sidebar (Overview/API Keys/Usage/Models + balance chip + logout), content `<RouterView>`. Anonymous -> inline 404 (copy the pattern from `AuthGate.vue` anonymous phase; "Sign in" link goes to `/clientarea`). Overview: balance card (format nano-USD -> `$0.000000` via a `formatNanoUSD` helper — create `app/format.ts` if not existing, check first), 30-day usage chart (reuse chart component used in `features/usage`), quick key count. KeysView: table of owned keys + create dialog (name field; on success show one-time reveal dialog with copy button using `AppCopyValue`); quota-reached -> disable button + i18n message. UsageView: period picker (reuse `dateRangePresets`) + grouped table (model rows: requests, tokens, cost). ModelsView: grid of `LandingModelCard` fed by `listModels()`.

- [ ] **Step 2: Verify**

Run: `pnpm --dir web run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/ && git commit -m "feat(web): client-area dashboard shell with overview, keys, usage, models"
```

---

### Task 11: Frontend — admin User Management (sidebar group + list + detail)

**Files:**
- Modify: `web/src/frontends/modern/app/navigation.ts` (new group "User Management" -> Users item)
- Create: `web/src/frontends/modern/features/users/UsersView.vue`, `features/users/UserDetailView.vue`
- Modify: router (routes `/users`, `/users/:id`, `requiresAuth: true, adminOnly: true`), i18n

**Interfaces:**
- Consumes: `api/admin-users.ts`; existing admin layout/sidebar rendering from `navigationItems`.

- [ ] **Step 1: Implement**

Read `app/navigation.ts` — add a group entry (follow the existing group/item shape). UsersView: `AppDataTable` with columns email/status/balance/keys/quota/created; search box; create-user dialog (email, password optional, initial balance). UserDetailView: profile card, edit quota (`AppInlineNumber`), disable/enable (`AppConfirmDialog`), topup dialog (USD amount input — convert to nano-USD client-side via `* 1e6` round, reason select topup/correction, note field), adjustments history table, owned keys table.

- [ ] **Step 2: Verify**

Run: `pnpm --dir web run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/ && git commit -m "feat(web): admin user management pages with topup and quota controls"
```

---

### Task 12: Integration verification + Docker rebuild

**Files:**
- None (verification task)

**Interfaces:**
- Consumes: everything.

- [ ] **Step 1: Full test suite**

Run: `go test ./... && pnpm --dir web run build`
Expected: all PASS.

- [ ] **Step 2: Docker rebuild**

```bash
docker compose up -d --build
```

- [ ] **Step 3: Playwright end-to-end smoke** (app at `http://127.0.0.1:3001`, admin key from `/app/data/auth.key` in container)

1. `/clientarea` -> signup tab -> create user -> lands on dashboard, balance $0.000000.
2. Create API key (reveal dialog, copy).
3. Admin: login at `/login` with auth key -> `/users` shows new user -> open detail -> topup $1.00 -> balance updates.
4. Back in client area: balance $1.000000; make a proxy request via the user key (`curl http://127.0.0.1:3001/v1/chat/completions -H "Authorization: Bearer sk-gl-..." -d '{"model":"...","messages":[...]}'` against a configured group) -> usage appears in client Usage page, balance decremented.
5. Admin disables user -> client session dead (401 -> inline 404 or redirected to /clientarea) and proxy request -> 402.
6. Admin tops user again -> 402 gone after cache TTL (<= 30s).

- [ ] **Step 4: Commit (if any fixes) + report**

```bash
git add -A && git commit -m "fix: integration fixes from e2e smoke"
```

---

## Self-Review

- **Spec coverage:** users/sessions/bcrypt (T2), quota keys (T5), usage (T5), models (T4/T10), admin list/create/patch/adjustments (T6), topup ledger (T3/T6), hard block 402 (T7), deduct idempotent (T7), reconciliation (T3), frontend zones + admin pages (T8-T11), no-path-leak guard (T8-T10). Gaps deliberately deferred per spec: email flows, gateway, vouchers.
- **Placeholders:** none — every task has concrete file paths, signatures, and test descriptions; frontend tasks reference exact existing components/patterns to mirror rather than invented APIs.
- **Type consistency:** `BalanceGate.Allow/Invalidate`, `DeductCosts(tx, costs)`, `AccessKeyCost`, `Profile`, `OwnedKeyView`, `AdminUserView` used consistently across T3/T5/T6/T7; frontend `useUserSession`/client functions consistent between T8/T9/T10.
