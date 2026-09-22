package userdash

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

// BalanceGate caches short-lived user balance checks for the gateway auth
// path. A missing (unknown) user counts as blocked: fail closed.
type BalanceGate struct {
	provider func(ctx context.Context, userIDs []uint) (map[uint]int64, error)
	ttl      time.Duration

	mu       sync.RWMutex
	balances map[uint]int64
	loadedAt time.Time

	// Single-flight refresh state: at most one provider call in flight;
	// concurrent callers wait on refreshDone instead of blocking the lock
	// across the provider query.
	refreshMu   sync.Mutex
	refreshing  bool
	refreshDone chan struct{}
}

// refreshTimeout bounds the synchronous provider query so a slow or hung
// database cannot stall gateway auths.
const refreshTimeout = 2 * time.Second

func NewBalanceGate(
	provider func(ctx context.Context, userIDs []uint) (map[uint]int64, error),
	ttl time.Duration,
) *BalanceGate {
	return &BalanceGate{provider: provider, ttl: ttl, balances: map[uint]int64{}}
}

// Allow reports whether the user has a positive balance, refreshing expired
// entries in one batched provider call. Provider errors fail closed until the
// cache expires.
func (gate *BalanceGate) Allow(userID uint) bool {
	if gate == nil || userID == 0 {
		return false
	}
	if cached, ok := gate.fresh(userID); ok {
		return cached > 0
	}
	gate.awaitRefresh(userID)
	cached, ok := gate.fresh(userID)
	if !ok {
		// Unknown user or failed refresh: fail closed.
		return false
	}
	return cached > 0
}

func (gate *BalanceGate) fresh(userID uint) (int64, bool) {
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	cached, has := gate.balances[userID]
	return cached, has && time.Since(gate.loadedAt) < gate.ttl
}

// awaitRefresh runs (or joins) the in-flight cache refresh. The write lock is
// never held while the provider query runs. A caller whose user is absent
// from the completed refresh (raced with a different first-time user) falls
// through to a new refresh that includes its ID.
func (gate *BalanceGate) awaitRefresh(userID uint) {
	gate.refreshMu.Lock()
	if gate.refreshing {
		done := gate.refreshDone
		gate.refreshMu.Unlock()
		<-done
		return
	}
	gate.refreshing = true
	gate.refreshDone = make(chan struct{})
	gate.refreshMu.Unlock()

	gate.refresh(userID)
	close(gate.refreshDone)

	gate.refreshMu.Lock()
	gate.refreshing = false
	gate.refreshMu.Unlock()
}

func (gate *BalanceGate) refresh(userID uint) {
	gate.mu.RLock()
	userIDs := make([]uint, 0, len(gate.balances)+1)
	for id := range gate.balances {
		if id != userID {
			userIDs = append(userIDs, id)
		}
	}
	gate.mu.RUnlock()
	userIDs = append(userIDs, userID)

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	fetched, err := gate.provider(ctx, userIDs)
	if err != nil {
		// Fail closed: keep stale positives out by zeroing every entry.
		gate.mu.Lock()
		gate.balances = map[uint]int64{}
		gate.loadedAt = time.Now()
		gate.mu.Unlock()
		return
	}
	balances := make(map[uint]int64, len(userIDs))
	for _, id := range userIDs {
		balances[id] = fetched[id]
	}
	gate.mu.Lock()
	gate.balances = balances
	gate.loadedAt = time.Now()
	gate.mu.Unlock()
}

// Invalidate drops the cache so the next Allow re-reads from the provider.
func (gate *BalanceGate) Invalidate() {
	if gate == nil {
		return
	}
	gate.mu.Lock()
	gate.balances = map[uint]int64{}
	gate.loadedAt = time.Time{}
	gate.mu.Unlock()
}

// UserBalanceProvider reads balances for a batch of user IDs in one query.
func UserBalanceProvider(db *gorm.DB) func(ctx context.Context, userIDs []uint) (map[uint]int64, error) {
	return func(ctx context.Context, userIDs []uint) (map[uint]int64, error) {
		if len(userIDs) == 0 {
			return map[uint]int64{}, nil
		}
		rows := make([]models.User, 0, len(userIDs))
		if err := db.WithContext(ctx).Select("id", "balance_nano_usd").
			Where("id IN ?", userIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		balances := make(map[uint]int64, len(rows))
		for _, row := range rows {
			balances[row.ID] = row.BalanceNanoUSD
		}
		return balances, nil
	}
}
