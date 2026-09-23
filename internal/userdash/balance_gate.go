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
	// lastRefreshOK records whether the most recent refresh reached the
	// provider. It gates retries so a provider outage fails closed on the first
	// attempt instead of hammering the backend maxRefreshJoinRetries times.
	lastRefreshOK bool
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

// maxRefreshAttempts bounds the total refreshes one Allow call may trigger: the
// initial attempt plus at most one extra for a joiner whose ID was absent from
// the batch it joined (raced with a different first-time user). The bound keeps
// adversarial churn from spinning forever while still guaranteeing a funded
// caller racing another first-time user gets its own ID into a batch.
const maxRefreshAttempts = 2

// awaitRefresh runs (or joins) the in-flight cache refresh. The write lock is
// never held while the provider query runs. A caller whose user is absent from
// the completed refresh (raced with a different first-time user) runs one more
// refresh that includes its own ID. A provider error fails closed immediately
// rather than retrying, so a backend outage is not amplified.
func (gate *BalanceGate) awaitRefresh(userID uint) {
	for attempt := 0; attempt < maxRefreshAttempts; attempt++ {
		gate.refreshMu.Lock()
		if gate.refreshing {
			done := gate.refreshDone
			gate.refreshMu.Unlock()
			<-done
		} else {
			gate.refreshing = true
			done := make(chan struct{})
			gate.refreshDone = done
			gate.refreshMu.Unlock()

			gate.refresh(userID)

			// Reset the single-flight slot before waking joiners so a woken
			// joiner whose ID was absent becomes the next leader instead of
			// observing a closed channel and burning an attempt.
			gate.refreshMu.Lock()
			gate.refreshing = false
			gate.refreshDone = nil
			close(done)
			gate.refreshMu.Unlock()
		}
		if _, ok := gate.fresh(userID); ok {
			return
		}
		gate.refreshMu.Lock()
		reachedProvider := gate.lastRefreshOK
		gate.refreshMu.Unlock()
		if !reachedProvider {
			// Provider error: fail closed now, do not retry into a failing DB.
			return
		}
	}
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
		gate.refreshMu.Lock()
		gate.lastRefreshOK = false
		gate.refreshMu.Unlock()
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
	gate.refreshMu.Lock()
	gate.lastRefreshOK = true
	gate.refreshMu.Unlock()
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
