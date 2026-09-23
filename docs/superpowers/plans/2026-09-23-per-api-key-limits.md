# Per-API-Key Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users set RPM, concurrent, total-credit, and model limits on their own API keys from the client area, with admins able to view and edit the same values from the admin dashboard.

**Architecture:** Reuse the existing per-key machinery (RPM column + limiter, `access_key_cost_limit_rules` + `accessquota` runtime, `filters.models` allowlist). Add exactly one new column (`access_keys.max_concurrent`) and one new runtime limiter (`ratelimit.AccessKeyConcurrency`) enforced in the gateway request/turn lifecycle. Expose client-area PATCH endpoints (ownership-scoped) and extend the admin user-detail projection. The client-area key list switches to all-time usage and gains a status toggle.

**Tech Stack:** Go 1.x + Gin + GORM (sqlite/mysql/postgres), DI via `internal/container`, Vue 3 + TypeScript (modern frontend), vue-i18n.

## Global Constraints

- The spec is `docs/superpowers/specs/2026-09-23-per-api-key-limits-design.md`. Follow it exactly.
- Limit value conventions: `0` = unlimited for RPM, concurrent, and credit. Model allowlist empty = allow all.
- Concurrency is process-local (per instance). Add a `ponytail:` comment naming the ceiling and the upgrade path.
- Never log or return plaintext credentials. Never expose secrets in tests.
- New migration ID must be exactly `0023_access_key_concurrency_limit`, registered in `internal/storage/migration.go` in contiguous order.
- Every mutation that changes gateway enforcement inputs must call `s.refreshRuntime(ctx)`.
- Do not introduce new dependencies. Do not add TPM.
- Go tests: run with `go test ./internal/<pkg>/...`. Frontend checks: `npm run` targets under `web/` (verify exact scripts in `web/package.json` before using).

---

### Task 1: Migration 0023 — `access_keys.max_concurrent`

**Files:**
- Create: `internal/storage/migrations/0023_access_key_concurrency_limit.go`
- Create: `internal/storage/migrations/0023_access_key_concurrency_limit_test.go`
- Modify: `internal/storage/migration.go:115-116` (append registry entry)
- Modify: `internal/storage/models/access_key.go` (add field)

**Interfaces:**
- Consumes: existing migration registry (`internal/storage/migration.go`), the ALTER pattern in `0009_price_multipliers.go`.
- Produces: column `access_keys.max_concurrent BIGINT NOT NULL DEFAULT 0`, constraint `chk_access_key_max_concurrent`; exported `ID0023`, `Up0023`, `Validate0023`, `ValidateRecoverable0023`; `models.AccessKey.MaxConcurrent int64`.

- [ ] **Step 1: Add the model field**

In `internal/storage/models/access_key.go`, add after `RPMLimit`:

```go
	MaxConcurrent           int64   `gorm:"column:max_concurrent;not null;default:0;check:chk_access_key_max_concurrent,max_concurrent >= 0"`
```

- [ ] **Step 2: Write the failing migration test**

Create `internal/storage/migrations/0023_access_key_concurrency_limit_test.go`:

```go
package migrations

import (
	"testing"

	"gorm.io/gorm"
)

func TestUp0023AddsMaxConcurrent(t *testing.T) {
	db := newMigrationTestDB(t) // helper used by sibling migration tests; verify its exact name in an existing migration test and reuse it.
	if err := Up0001(db); err != nil {
		t.Fatalf("Up0001: %v", err)
	}
	if err := Up0023(db); err != nil {
		t.Fatalf("Up0023: %v", err)
	}
	if !db.Migrator().HasColumn("access_keys", "max_concurrent") {
		t.Fatal("max_concurrent column missing")
	}
	if err := Validate0023(db); err != nil {
		t.Fatalf("Validate0023: %v", err)
	}
	// Idempotent: running again must not error.
	if err := Up0023(db); err != nil {
		t.Fatalf("Up0023 second run: %v", err)
	}
	// Recoverable accepts a not-yet-migrated table (simulate by dropping the column).
	if err := db.Migrator().DropColumn("access_keys", "max_concurrent"); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if err := ValidateRecoverable0023(db); err != nil {
		t.Fatalf("ValidateRecoverable0023 pre-column: %v", err)
	}
	var _ *gorm.DB = db
}
```

Note: confirm the exact DB-fixture helper name by reading a sibling test (`internal/storage/migrations/0009_price_multipliers_test.go` or `0002_..._test.go`) and use that helper verbatim.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/storage/migrations/ -run TestUp0023 -v`
Expected: FAIL (undefined: ID0023/Up0023/Validate0023/ValidateRecoverable0023).

- [ ] **Step 4: Implement the migration**

Create `internal/storage/migrations/0023_access_key_concurrency_limit.go` modeled on `0009_price_multipliers.go`:

```go
package migrations

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const ID0023 = "0023_access_key_concurrency_limit"

const (
	concurrencyTable0023      = "access_keys"
	concurrencyColumn0023     = "max_concurrent"
	concurrencyConstraint0023 = "chk_access_key_max_concurrent"
)

func Up0023(db *gorm.DB) error {
	if err := ValidateRecoverable0023(db); err != nil {
		return err
	}
	if !db.Migrator().HasColumn(concurrencyTable0023, concurrencyColumn0023) {
		statement := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s BIGINT NOT NULL DEFAULT 0 CONSTRAINT %s CHECK (%s >= 0)",
			quoteConcurrencyIdentifier0023(db, concurrencyTable0023),
			quoteConcurrencyIdentifier0023(db, concurrencyColumn0023),
			quoteConcurrencyIdentifier0023(db, concurrencyConstraint0023),
			quoteConcurrencyIdentifier0023(db, concurrencyColumn0023),
		)
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("add %s.%s: %w", concurrencyTable0023, concurrencyColumn0023, err)
		}
	}
	return Validate0023(db)
}

func ValidateRecoverable0023(db *gorm.DB) error {
	if !db.Migrator().HasTable(concurrencyTable0023) {
		return fmt.Errorf("table %q is missing", concurrencyTable0023)
	}
	if !db.Migrator().HasColumn(concurrencyTable0023, concurrencyColumn0023) {
		return nil
	}
	return validateConcurrencyColumn0023(db)
}

func Validate0023(db *gorm.DB) error {
	if !db.Migrator().HasColumn(concurrencyTable0023, concurrencyColumn0023) {
		return fmt.Errorf("%s.%s is missing", concurrencyTable0023, concurrencyColumn0023)
	}
	return validateConcurrencyColumn0023(db)
}

func validateConcurrencyColumn0023(db *gorm.DB) error {
	if err := validateConcurrencyType0023(db); err != nil {
		return err
	}
	if !db.Migrator().HasConstraint(concurrencyTable0023, concurrencyConstraint0023) {
		return fmt.Errorf("%s constraint is missing", concurrencyConstraint0023)
	}
	return validateConcurrencyValues0023(db)
}

func validateConcurrencyType0023(db *gorm.DB) error {
	columns, err := db.Migrator().ColumnTypes(concurrencyTable0023)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", concurrencyColumn0023, err)
	}
	for _, column := range columns {
		if !strings.EqualFold(column.Name(), concurrencyColumn0023) {
			continue
		}
		if !strings.Contains(strings.ToLower(column.DatabaseTypeName()), "int") {
			return fmt.Errorf("%s is not integer", concurrencyColumn0023)
		}
		if nullable, known := column.Nullable(); !known || nullable {
			return fmt.Errorf("%s is nullable", concurrencyColumn0023)
		}
		defaultValue, known := column.DefaultValue()
		if strings.EqualFold(db.Dialector.Name(), "sqlite") {
			if err := db.Raw("SELECT dflt_value FROM pragma_table_info(?) WHERE name = ?", concurrencyTable0023, concurrencyColumn0023).Scan(&defaultValue).Error; err != nil {
				return fmt.Errorf("inspect %s default: %w", concurrencyColumn0023, err)
			}
			known = defaultValue != ""
		}
		defaultValue = strings.Trim(strings.Split(defaultValue, "::")[0], "()' \"")
		value, parseErr := strconv.ParseInt(defaultValue, 10, 64)
		if !known || parseErr != nil || value != 0 {
			return fmt.Errorf("%s has invalid default %q (known %t)", concurrencyColumn0023, defaultValue, known)
		}
		return nil
	}
	return fmt.Errorf("%s is missing", concurrencyColumn0023)
}

func validateConcurrencyValues0023(db *gorm.DB) error {
	var invalid int64
	if err := db.Table(concurrencyTable0023).
		Where(concurrencyColumn0023 + " IS NULL OR " + concurrencyColumn0023 + " < 0").
		Count(&invalid).Error; err != nil {
		return fmt.Errorf("validate %s values: %w", concurrencyColumn0023, err)
	}
	if invalid != 0 {
		return fmt.Errorf("%s contains invalid values", concurrencyColumn0023)
	}
	return nil
}

func quoteConcurrencyIdentifier0023(db *gorm.DB, value string) string {
	if strings.EqualFold(db.Dialector.Name(), "mysql") {
		return "`" + value + "`"
	}
	return `"` + value + `"`
}
```

- [ ] **Step 5: Register the migration**

In `internal/storage/migration.go`, append after the `ID0022` entry (before the closing `}`):

```go
	{ID: migrationfiles.ID0023, Up: migrationfiles.Up0023, Validate: migrationfiles.Validate0023, ValidateRecoverable: migrationfiles.ValidateRecoverable0023},
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/storage/... -run 'TestUp0023|Migration' -v`
Expected: PASS. Then run the full storage suite: `go test ./internal/storage/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/storage/models/access_key.go internal/storage/migrations/0023_access_key_concurrency_limit.go internal/storage/migrations/0023_access_key_concurrency_limit_test.go internal/storage/migration.go
git commit -m "feat(storage): add access_keys.max_concurrent column (migration 0023)"
```

---

### Task 2: Runtime concurrency limiter

**Files:**
- Create: `internal/ratelimit/access_key_concurrency.go`
- Create: `internal/ratelimit/access_key_concurrency_test.go`

**Interfaces:**
- Consumes: stdlib only (`sync`, `sync/atomic`).
- Produces:

```go
type AccessKeyConcurrency struct { /* unexported */ }
func NewAccessKeyConcurrency() *AccessKeyConcurrency
func (c *AccessKeyConcurrency) Acquire(accessKeyID uint, limit int64) (release func(), allowed bool)
func (c *AccessKeyConcurrency) InFlight(accessKeyID uint) int64 // test/inspection helper
```

Semantics: `limit <= 0` or `accessKeyID == 0` -> `allowed=true`, `release` is a no-op. Otherwise increment; if the new value exceeds `limit`, decrement and return `allowed=false`. `release` decrements exactly once (idempotent) and removes zeroed entries.

- [ ] **Step 1: Write the failing test**

Create `internal/ratelimit/access_key_concurrency_test.go`:

```go
package ratelimit

import (
	"sync"
	"testing"
)

func TestAccessKeyConcurrencyUnlimited(t *testing.T) {
	c := NewAccessKeyConcurrency()
	for i := 0; i < 100; i++ {
		release, ok := c.Acquire(1, 0)
		if !ok {
			t.Fatalf("unlimited acquire %d blocked", i)
		}
		release()
	}
	if got := c.InFlight(1); got != 0 {
		t.Fatalf("InFlight = %d, want 0", got)
	}
}

func TestAccessKeyConcurrencyBlocksOverLimit(t *testing.T) {
	c := NewAccessKeyConcurrency()
	release1, ok1 := c.Acquire(7, 2)
	release2, ok2 := c.Acquire(7, 2)
	if !ok1 || !ok2 {
		t.Fatalf("first two acquires = %v/%v, want true/true", ok1, ok2)
	}
	if _, ok3 := c.Acquire(7, 2); ok3 {
		t.Fatal("third acquire allowed, want blocked")
	}
	release1()
	release1() // idempotent
	if got := c.InFlight(7); got != 1 {
		t.Fatalf("InFlight after release = %d, want 1", got)
	}
	if _, ok4 := c.Acquire(7, 2); !ok4 {
		t.Fatal("acquire after release blocked, want allowed")
	}
	release2()
}

func TestAccessKeyConcurrencyIsolationAndCleanup(t *testing.T) {
	c := NewAccessKeyConcurrency()
	releaseA, _ := c.Acquire(1, 1)
	releaseB, _ := c.Acquire(2, 1)
	if c.InFlight(1) != 1 || c.InFlight(2) != 1 {
		t.Fatalf("isolation broken: %d/%d", c.InFlight(1), c.InFlight(2))
	}
	releaseA()
	releaseB()
	if c.InFlight(1) != 0 || c.InFlight(2) != 0 {
		t.Fatalf("entries not cleaned: %d/%d", c.InFlight(1), c.InFlight(2))
	}
}

func TestAccessKeyConcurrencyParallel(t *testing.T) {
	c := NewAccessKeyConcurrency()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, ok := c.Acquire(9, 10)
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
			release()
		}()
	}
	wg.Wait()
	if got := c.InFlight(9); got != 0 {
		t.Fatalf("InFlight = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit/ -run TestAccessKeyConcurrency -v`
Expected: FAIL (undefined: NewAccessKeyConcurrency).

- [ ] **Step 3: Implement the limiter**

Create `internal/ratelimit/access_key_concurrency.go`:

```go
package ratelimit

import (
	"sync"
	"sync/atomic"
)

// AccessKeyConcurrency bounds simultaneous in-flight requests per access key.
//
// ponytail: process-local counters. In a multi-instance deployment each
// instance enforces the limit independently, so the effective global limit is
// `limit * instances`. Upgrade path: back the counter with a shared store
// (Redis/DB atomic) if cross-instance enforcement is required.
type AccessKeyConcurrency struct {
	entries sync.Map // uint -> *atomic.Int64
}

func NewAccessKeyConcurrency() *AccessKeyConcurrency {
	return &AccessKeyConcurrency{}
}

// Acquire reserves one in-flight slot for the key. It returns a release
// function that must be called exactly once when the request finishes, even on
// error paths. A limit <= 0 means unlimited and never touches the counter.
func (c *AccessKeyConcurrency) Acquire(accessKeyID uint, limit int64) (func(), bool) {
	if c == nil || accessKeyID == 0 || limit <= 0 {
		return func() {}, true
	}
	counter := c.counter(accessKeyID)
	if counter.Add(1) > limit {
		counter.Add(-1)
		return func() {}, false
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			if counter.Add(-1) == 0 {
				c.entries.CompareAndDelete(accessKeyID, counter)
			}
		})
	}, true
}

func (c *AccessKeyConcurrency) InFlight(accessKeyID uint) int64 {
	if c == nil {
		return 0
	}
	value, ok := c.entries.Load(accessKeyID)
	if !ok {
		return 0
	}
	return value.(*atomic.Int64).Load()
}

func (c *AccessKeyConcurrency) counter(accessKeyID uint) *atomic.Int64 {
	if value, ok := c.entries.Load(accessKeyID); ok {
		return value.(*atomic.Int64)
	}
	created := &atomic.Int64{}
	actual, _ := c.entries.LoadOrStore(accessKeyID, created)
	return actual.(*atomic.Int64)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ratelimit/ -run TestAccessKeyConcurrency -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ratelimit/access_key_concurrency.go internal/ratelimit/access_key_concurrency_test.go
git commit -m "feat(ratelimit): per-key concurrent request limiter"
```

---

### Task 3: Gateway enforcement of concurrency

**Files:**
- Modify: `internal/gateway/handler.go` (`Handler` struct, `NewHandler` signature/variadic, request flow around lines 440-530)
- Modify: `internal/gateway/reason.go` (add reason)
- Modify: `internal/gateway/websocket_turn.go:150-164`
- Modify: `internal/gateway/websocket.go:519`
- Modify: `internal/container/container.go:78-81`
- Test: `internal/gateway/access_key_concurrency_gateway_test.go` (new)

**Interfaces:**
- Consumes: `ratelimit.AccessKeyConcurrency` (Task 2), `models.AccessKey.MaxConcurrent` (Task 1), `state.AccessKeyView` (add `MaxConcurrent`).
- Produces: gateway `AccessKeyConcurrencyLimiter` interface, `reasonAccessKeyConcurrentLimited`.

- [ ] **Step 1: Add `MaxConcurrent` to the state view**

In `internal/state/snapshot.go`, in `AccessKeyView` (near `RPMLimit`), add:

```go
	MaxConcurrent    int64
```

In `internal/state/loader/loader.go`, find where `RPMLimit` is copied into the view (search `RPMLimit:`), and add `MaxConcurrent: row.MaxConcurrent,` alongside. Also update the access-key **config** struct at `internal/state/snapshot.go` (`AccessKeyConfig`, near line 89) and the compile mapping (search `RPMLimit:`) so the value flows from config → snapshot. Verify with: `go build ./...` after editing.

- [ ] **Step 2: Write the failing gateway test**

Create `internal/gateway/access_key_concurrency_gateway_test.go`. Reuse the existing test runtime helper `newRequestLogHandlerTestRuntime` (see `request_log_test.go:2200`) and the `recordingAccessKeyRPMLimiter`. Because `NewHandler` will gain a new optional parameter, call it the same way the helper does (the helper is the source of truth; read it first).

The test must:
1. Build a handler, set `handler.concurrency = ratelimit.NewAccessKeyConcurrency()`.
2. Configure an access key with `MaxConcurrent = 1` in the snapshot used by the test helper.
3. Fire one request that blocks in the forwarder (use a channel-gated scripted forwarder already available in the gateway tests).
4. While it is in flight, fire a second request and assert `429` with the concurrency reason code.
5. Release the first request, then assert a third request succeeds.

If the existing test harness cannot easily gate the forwarder, instead unit-test the gate function directly: assert `handler.acquireConcurrency(view)` returns `release=nil/ok=false` for a second acquire at limit 1 and that `release()` restores capacity. Keep the test small and deterministic.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/gateway/ -run Concurrency -v`
Expected: FAIL (field/method undefined).

- [ ] **Step 4: Add the reason**

In `internal/gateway/reason.go`, near `reasonAccessKeyRateLimited`, add:

```go
var reasonAccessKeyConcurrentLimited = reason{http.StatusTooManyRequests, "access_key_concurrent_limit_exceeded", "Concurrent request limit exceeded for this API key. Retry shortly."}
```

Match the exact `reason` struct literal shape used by sibling reasons (read the file first — field names/order).

- [ ] **Step 5: Implement the gateway gate**

In `internal/gateway/handler.go`:

Add the interface near `AccessKeyRPMLimiter` (line ~64):

```go
type AccessKeyConcurrencyLimiter interface {
	Acquire(accessKeyID uint, limit int64) (release func(), allowed bool)
}
```

Add a field to `Handler`:

```go
	concurrency AccessKeyConcurrencyLimiter
```

Set it in `NewHandler`. The constructor currently takes variadic `accessQuotas ...*accessquota.Runtime`; add a second variadic or a setter. Prefer a nil-tolerant setter to avoid changing the many call sites:

```go
func (handler *Handler) SetConcurrencyLimiter(limiter AccessKeyConcurrencyLimiter) {
	if handler == nil {
		return
	}
	handler.concurrency = limiter
}
```

In the request flow, immediately after the RPM check at `handler.go:513-521`, add:

```go
	var releaseConcurrency func()
	if handler.concurrency != nil {
		release, allowed := handler.concurrency.Acquire(accessKey.ID, accessKey.MaxConcurrent)
		if !allowed {
			ginContext.Writer.Header().Set("Retry-After", "1")
			handler.completeReason(ginContext, recorder, reasonAccessKeyConcurrentLimited)
			return
		}
		if release != nil {
			releaseConcurrency = release
			defer releaseConcurrency()
		}
	}
```

Place the `defer` in the same function scope that owns the existing `defer recorder.emit()` block (handler.go:476-489) so release runs on every exit path. `accessKey.MaxConcurrent` must be available on the `state.AccessKeyView` used here (Step 1).

- [ ] **Step 6: Wire the WebSocket paths**

In `internal/gateway/websocket_turn.go`, after the RPM check (line ~161-164), add:

```go
	releaseConcurrency := func() {}
	concurrencyAcquired := false
	if h.concurrency != nil {
		release, allowed := h.concurrency.Acquire(key.ID, key.MaxConcurrent)
		if !allowed {
			reject(reasonAccessKeyConcurrentLimited)
			return
		}
		releaseConcurrency = release
		concurrencyAcquired = true
	}
	if concurrencyAcquired {
		defer releaseConcurrency()
	}
```

Confirm the turn function's lifetime matches the request (release must run when the turn finishes). If the turn is long-lived across the streaming loop, confirm the defer's enclosing function returns at turn completion; if not, store the release on the turn/session struct and call it at turn finish. Read the surrounding function signature before finalizing.

In `internal/gateway/websocket.go:519`, this is a rejection path that does not proceed to forward, so no acquire/release is needed there — but extend the existing `else if` chain to also report the concurrency reason if applicable. If the RPM check already rejects, leave this path as-is. (Minimal change: no acquire here.)

- [ ] **Step 7: Wire DI**

In `internal/container/container.go`, register the limiter after `ratelimit.NewAccessKeyRPM`:

```go
		ratelimit.NewAccessKeyConcurrency,
```

Then find where the `gateway.Handler` is constructed (search `NewHandler(` in container.go) and add a call `handler.SetConcurrencyLimiter(concurrency)` in the constructor closure. Read the exact construction site and adapt.

- [ ] **Step 8: Run tests**

Run: `go build ./... && go test ./internal/gateway/ ./internal/container/ ./internal/ratelimit/ -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/gateway/handler.go internal/gateway/reason.go internal/gateway/websocket_turn.go internal/gateway/websocket.go internal/gateway/access_key_concurrency_gateway_test.go internal/state/snapshot.go internal/state/loader/loader.go internal/container/container.go
git commit -m "feat(gateway): enforce per-key concurrent request limit"
```

---

### Task 4: Client-area — limits, status toggle, all-time usage

**Files:**
- Modify: `internal/userdash/keys.go`
- Modify: `internal/userdash/http_module.go`
- Test: `internal/userdash/keys_limits_test.go` (new)

**Interfaces:**
- Consumes: `models.AccessKey.MaxConcurrent`, `models.AccessKeyCostLimitRule`, `accessquota`, `Service.refreshRuntime`.
- Produces: `SetOwnedKeyLimits`, `SetOwnedKeyStatus`, extended `OwnedKeyView`; routes `clientarea.keys.limits`, `clientarea.keys.status`.

- [ ] **Step 1: Write the failing service test**

Create `internal/userdash/keys_limits_test.go` following the `newHTTPTestEnv(t)` pattern in `keys_test.go`. Assert:
1. Create a key, `PATCH /api/clientarea/keys/:id/limits` with `{"rpm_limit":5,"max_concurrent":2,"credit_limit_nano_usd":1000000000,"models":["gpt-4o"]}` -> 200; reload the row and assert `RPMLimit == 5`, `MaxConcurrent == 2`, one `total` cost rule with `LimitNanoUSD == 1000000000`, and `filters.models` contains `gpt-4o`.
2. `GET /api/clientarea/keys` returns the new limit fields and `usage_total_nano_usd`.
3. PATCH another user's key id -> 404.
4. `PATCH .../status {"status":"disabled"}` then `{"status":"active"}` -> key status flips and re-enable works.
5. `credit_limit_nano_usd: 0` clears the total rule; `models: []` clears the allowlist.

Use the test env's `do(...)` helper and `decodeEnvelope(t, resp)` as in `keys_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userdash/ -run TestOwnedKeyLimits -v`
Expected: FAIL (routes/fields undefined).

- [ ] **Step 3: Implement the service methods**

In `internal/userdash/keys.go`:

Extend `OwnedKeyView`:

```go
	RPMLimit             int64    `json:"rpm_limit"`
	MaxConcurrent        int64    `json:"max_concurrent"`
	CreditLimitNanoUSD   int64    `json:"credit_limit_nano_usd"`
	Models               []string `json:"models"`
	UsageTotalNanoUSD    int64    `json:"usage_total_nano_usd"`
```

(Remove `Usage30dNanoUSD`/`RequestCount30d` from the client-area contract or keep them only if a consumer needs them — the client-area UI switches to total; per the spec, drop the 30-day fields from this view.)

Replace `ownedKeyUsage` (time-windowed) with a lifetime aggregate:

```go
type ownedKeyUsageAggregate struct {
	AccessKeyID       uint  `gorm:"column:access_key_id"`
	UsageTotalNanoUSD int64 `gorm:"column:usage_total_nano_usd"`
}

func (s *Service) ownedKeyUsage(ctx context.Context, keyIDs []uint) (map[uint]ownedKeyUsageAggregate, error) {
	aggregates := make(map[uint]ownedKeyUsageAggregate, len(keyIDs))
	if len(keyIDs) == 0 {
		return aggregates, nil
	}
	rows := make([]ownedKeyUsageAggregate, 0, len(keyIDs))
	if err := s.db.WithContext(ctx).Model(&models.UsageStat{}).
		Select("access_key_id, COALESCE(SUM(estimated_cost_nano_usd), 0) AS usage_total_nano_usd").
		Where("access_key_id IN ?", keyIDs).
		Group("access_key_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		aggregates[row.AccessKeyID] = row
	}
	return aggregates, nil
}
```

In `ListOwnedKeys`, select the new columns (`rpm_limit`, `max_concurrent`, `filters`) and populate the view fields (`CreditLimitNanoUSD` from the key's cost rules; `Models` from decoded filters).

Add input type and setter:

```go
type OwnedKeyLimitsInput struct {
	RPMLimit           *int64
	MaxConcurrent      *int64
	CreditLimitNanoUSD *int64
	Models             *[]string
}

func (s *Service) SetOwnedKeyLimits(ctx context.Context, userID, keyID uint, input OwnedKeyLimitsInput) error
```

Implementation rules:
- Validate: negative values -> `ErrValidation`; models trimmed/deduped, each non-empty, bounded count.
- Load the key with `WHERE id = ? AND user_id = ?`; missing -> `gorm.ErrRecordNotFound`.
- In one transaction: update `rpm_limit`/`max_concurrent`/`filters` when their pointers are set; for credit, replace the key's `total` cost rule (create/replace/delete) using the existing cost-limit helpers in `internal/control/access_key_cost_limits.go` where reusable, or the model directly (`models.AccessKeyCostLimitRule`).
- After commit: `return s.refreshRuntime(ctx)`.

Add status setter (generalize `DisableOwnedKey`):

```go
func (s *Service) SetOwnedKeyStatus(ctx context.Context, userID, keyID uint, status string) error
```

Validate `status` is `"active"` or `"disabled"`; update with `WHERE id = ? AND user_id = ?`; `RowsAffected == 0` -> `gorm.ErrRecordNotFound`; then `refreshRuntime`.

- [ ] **Step 4: Add HTTP handlers and routes**

In `internal/userdash/http_module.go`:
- Add routes `PATCH /keys/:id/limits` -> `m.handleKeyLimits` and `PATCH /keys/:id/status` -> `m.handleKeyStatus`, both with `m.sessionAuth()`.
- Handlers parse the key id (existing `strconv.ParseUint` pattern), bind JSON into a request struct with pointer fields (set-aware), call the service, map `ErrValidation` -> 400, `gorm.ErrRecordNotFound` -> 404, success -> existing response envelope.

Keep the existing `DELETE /keys/:id` working (delegate to `SetOwnedKeyStatus(..., "disabled")`) for backward compatibility.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/userdash/ -v`
Expected: PASS (fix any existing tests that referenced the removed 30-day fields).

- [ ] **Step 6: Commit**

```bash
git add internal/userdash/keys.go internal/userdash/http_module.go internal/userdash/keys_limits_test.go
git commit -m "feat(userdash): per-key limits, status toggle, lifetime usage"
```

---

### Task 5: Admin visibility + edit of per-key limits

**Files:**
- Modify: `internal/userdash/admin.go` (`AdminUserKeyView`, `GetAdminUser`)
- Modify: `internal/control/access_keys.go` (add `MaxConcurrent` to create/update request, metadata response, row query, update closure)
- Modify: `internal/control/access_key_collection.go` (add `MaxConcurrent` to the collection projection)
- Test: `internal/userdash/admin_keys_limits_test.go` (new), `internal/control/access_keys_test.go` (extend)

**Interfaces:**
- Consumes: Task 1 model field, Task 4 cost-rule storage.
- Produces: `AdminUserKeyView` with `rpm_limit`, `max_concurrent`, `credit_limit_nano_usd`, `models`, `usage_total_nano_usd`; admin access-key DTO `max_concurrent`.

- [ ] **Step 1: Extend admin access-key DTO**

In `internal/control/access_keys.go`:
- Add `MaxConcurrent OptionalRPMLimit \`json:"max_concurrent"\`` to `AccessKeyCreateRequest` and `AccessKeyUpdateRequest` (reuse the set-aware optional int type; if a clearer name is warranted, add `OptionalInt64` mirroring `OptionalRPMLimit`).
- Add `MaxConcurrent int64 \`json:"max_concurrent"\`` to `AccessKeyMetadata`.
- Add `MaxConcurrent int64` to `accessKeyMetadataRow`.
- Add the column to every `Select(...)` list that fetches access-key metadata (search `"rpm_limit"` in the file).
- In the create/update closure, normalize `max_concurrent` (>= 0) and set `updates["max_concurrent"]` when the field is set (mirror the `rpm_limit` handling at lines ~517-519).
- Set `MaxConcurrent` in the metadata mapping (search `RPMLimit: row.RPMLimit`).

- [ ] **Step 2: Extend the collection projection**

In `internal/control/access_key_collection.go`: add `MaxConcurrent int64` to the item struct, the select list, and the mapping (mirror `RPMLimit` at lines ~63/128/168).

- [ ] **Step 3: Write the failing tests**

- `internal/control/access_keys_test.go`: extend `TestAccessKeyServiceRPMLimit`-style coverage — create a key with `rpm_limit:12` and `max_concurrent:3`, assert both persist and appear in the response; update `max_concurrent` to `0` and assert it clears.
- `internal/userdash/admin_keys_limits_test.go`: create a user + owned key, set limits via the service, then call `GetAdminUser` and assert the returned `AdminUserKeyView` includes `rpm_limit`, `max_concurrent`, `credit_limit_nano_usd`, `models`, `usage_total_nano_usd`.

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/control/ ./internal/userdash/ -run 'AccessKey|Admin' -v`
Expected: FAIL (undefined fields).

- [ ] **Step 5: Implement admin view extension**

In `internal/userdash/admin.go`:
- Extend `AdminUserKeyView` with `RPMLimit int64`, `MaxConcurrent int64`, `CreditLimitNanoUSD int64`, `Models []string`, `UsageTotalNanoUSD int64`.
- In `GetAdminUser`, select `rpm_limit`, `max_concurrent`, `filters`; decode models; load the key's total cost rule for `CreditLimitNanoUSD`; reuse the lifetime usage aggregate from Task 4 (`s.ownedKeyUsage`) for `UsageTotalNanoUSD`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/control/ ./internal/userdash/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/control/access_keys.go internal/control/access_key_collection.go internal/control/access_keys_test.go internal/userdash/admin.go internal/userdash/admin_keys_limits_test.go
git commit -m "feat(admin): expose and edit per-key limits"
```

---

### Task 6: Frontend client area

**Files:**
- Modify: `web/src/frontends/modern/api/clientarea.ts`
- Modify: `web/src/frontends/modern/features/clientarea/KeysView.vue`
- Create: `web/src/frontends/modern/features/clientarea/KeyLimitsDialog.vue`
- Modify: `web/src/frontends/modern/i18n/locales/{en-US,zh-CN,ja-JP}.ts`

**Interfaces:**
- Consumes: Task 4 endpoints.
- Produces: `OwnedKeyView` with new fields; `updateKeyLimits`, `updateKeyStatus` API functions; `KeyLimitsDialog` component.

- [ ] **Step 1: Extend the API client**

In `web/src/frontends/modern/api/clientarea.ts`:
- Extend `OwnedKeyView` with `rpm_limit: number`, `max_concurrent: number`, `credit_limit_nano_usd: number`, `models: string[]`, `usage_total_nano_usd: number`.
- Add:

```ts
export interface KeyLimitsInput {
  rpm_limit?: number
  max_concurrent?: number
  credit_limit_nano_usd?: number
  models?: string[]
}
export function updateKeyLimits(client: ClientAreaClient, id: number, input: KeyLimitsInput): Promise<void>
export function updateKeyStatus(client: ClientAreaClient, id: number, status: 'active' | 'disabled'): Promise<void>
```

Follow the existing `deleteKey` request/response handling pattern in the file.

- [ ] **Step 2: Create the limits dialog component**

Create `KeyLimitsDialog.vue` with props `{ open: boolean; key: OwnedKeyView | null }` and emits `update:open`, `saved`. Fields: RPM (number), Concurrent (number), Usage credit USD (number, converted to nano-USD `Math.round(usd * 1e9)`), Models (multi-select/text list). Reuse `AppDialogContent`, `AppDialogHeader`, `AppTextField`, `AppButton`, `AppNotice` from `@modern/components/ui`. Show `t('clientarea.keys.limits.*')` labels. On submit call `updateKeyLimits`.

- [ ] **Step 3: Update `KeysView.vue`**

- Replace the usage column header/body to render `formatNanoUSD(row.usage_total_nano_usd, locale)` and `t('clientarea.keys.totalUsage')`.
- Replace the single Disable action with: a **Limits** button (opens `KeyLimitsDialog`) and an **Enable/Disable** toggle button. Disable calls `updateKeyStatus(client, row.id, 'disabled')`; enable calls `updateKeyStatus(client, row.id, 'active')`. Refresh on success.
- Keep the create dialog unchanged.

- [ ] **Step 4: Add i18n keys**

Add to `clientarea.keys` in `en-US.ts`, `zh-CN.ts`, `ja-JP.ts`: `totalUsage`, `limits.action`, `limits.title`, `limits.description`, `limits.rpm`, `limits.concurrent`, `limits.credit`, `limits.models`, `limits.save`, `limits.saveFailed`, `enable`, `enableFailed`, `statuses.active`, `statuses.disabled` (if not already present).

- [ ] **Step 5: Run frontend checks**

Run (verify exact script names in `web/package.json` first):
`npm --prefix web run typecheck` and `npm --prefix web run lint` and `npm --prefix web run build`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/frontends/modern/api/clientarea.ts web/src/frontends/modern/features/clientarea/KeysView.vue web/src/frontends/modern/features/clientarea/KeyLimitsDialog.vue web/src/frontends/modern/i18n/locales/en-US.ts web/src/frontends/modern/i18n/locales/zh-CN.ts web/src/frontends/modern/i18n/locales/ja-JP.ts
git commit -m "feat(web): client-area per-key limits and status toggle"
```

---

### Task 7: Frontend admin user detail

**Files:**
- Modify: `web/src/frontends/modern/api/admin-users.ts`
- Modify: `web/src/frontends/modern/features/users/UserDetailView.vue`
- Modify: `web/src/frontends/modern/i18n/locales/{en-US,zh-CN,ja-JP}.ts`

**Interfaces:**
- Consumes: Task 5 `AdminUserKeyView` fields; existing admin `updateAccessKey` from `api/access-keys.ts`.
- Produces: admin user-detail key table with limit columns and an edit dialog.

- [ ] **Step 1: Extend `AdminUserKeyView`**

In `web/src/frontends/modern/api/admin-users.ts`: add `rpm_limit: number`, `max_concurrent: number`, `credit_limit_nano_usd: number`, `models: string[]`, `usage_total_nano_usd: number`, and update `readAdminUserKey` to parse them (mirror existing readers).

- [ ] **Step 2: Update `UserDetailView.vue`**

- Add columns to the owned-keys table: RPM, Concurrent, Credit, Models.
- Add an **Edit limits** action per row opening a dialog that reuses `AccessKeyQuotaEditor.vue` for credit and the scope multi-select for models, and calls `updateAccessKey` with `{ rpm_limit, max_concurrent, cost_limit_rules, filters: { models } }`. Read `features/access-keys/AccessKeyPanel.vue` first and reuse its input-building helpers (`access-key-draft.ts` `inputFor`/`patchFor`) rather than duplicating.

- [ ] **Step 3: Add i18n keys**

Add `users.detail.keyRpm`, `users.detail.keyConcurrent`, `users.detail.keyCredit`, `users.detail.keyModels`, `users.detail.editLimits`, `users.detail.limitsSaved`, `users.detail.limitsFailed` to the three locale files.

- [ ] **Step 4: Run frontend checks**

Run: `npm --prefix web run typecheck && npm --prefix web run lint && npm --prefix web run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/frontends/modern/api/admin-users.ts web/src/frontends/modern/features/users/UserDetailView.vue web/src/frontends/modern/i18n/locales/en-US.ts web/src/frontends/modern/i18n/locales/zh-CN.ts web/src/frontends/modern/i18n/locales/ja-JP.ts
git commit -m "feat(web): admin user-detail per-key limit visibility and edit"
```

---

### Task 8: End-to-end verification

**Files:**
- Test: `internal/gateway/access_key_concurrency_gateway_test.go` (extend), `internal/userdash/keys_limits_test.go` (extend if needed)

- [ ] **Step 1: Run the full backend test suite**

Run: `go build ./... && go test ./...`
Expected: PASS. Report any pre-existing failures separately (do not fix unrelated failures).

- [ ] **Step 2: Run frontend checks**

Run: `npm --prefix web run typecheck && npm --prefix web run lint && npm --prefix web run build`
Expected: PASS.

- [ ] **Step 3: Verify no obsolete references**

Run: `grep -rn "usage_30d_nano_usd\|request_count_30d\|ownedKeyUsageWindow" internal/ web/src --include=*.go --include=*.ts --include=*.vue`
Expected: no client-area references remain (dashboard heatmap uses a different path; confirm).

- [ ] **Step 4: Commit any test additions**

```bash
git add -A
git commit -m "test: end-to-end coverage for per-key limits"
```

---

## Self-Review

**Spec coverage:**
- Usage credit limit -> Tasks 4, 5, 6, 7 (cost-rule reuse).
- RPM limit -> existing enforcement; exposed in Tasks 4-7.
- Concurrent limit -> Tasks 1, 2, 3, then exposed in 4-7.
- Model limit -> existing `filters.models`; exposed in Tasks 4-7.
- TPM skipped -> no task (correct).
- Total usage instead of 30-day -> Task 4 (backend), Task 6 (client UI).
- Disable/enable toggle -> Task 4 (service), Task 6 (UI).
- Admin sees + edits -> Task 5 (backend), Task 7 (UI).
- Migration conventions -> Task 1.
- Testing -> each task + Task 8.

**Known ambiguities to resolve during implementation (documented, not placeholders):**
- Exact test-fixture helper names (`newMigrationTestDB`, `newHTTPTestEnv`) and the gateway test runtime helper: the implementer MUST read the sibling test files and reuse the real names. This is intentional because the helpers exist but their precise signatures must be copied, not guessed.
- Whether the WebSocket turn's `defer` scope matches turn lifetime: Task 3 Step 6 requires reading the function and, if needed, storing the release on the turn/session struct.

**Type consistency:** limit fields use one naming across layers — `RPMLimit`/`rpm_limit`, `MaxConcurrent`/`max_concurrent`, credit `CreditLimitNanoUSD`/`credit_limit_nano_usd`, `Models`/`models`, usage `UsageTotalNanoUSD`/`usage_total_nano_usd`.
