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
}

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
	gate.mu.RLock()
	cached, has := gate.balances[userID]
	fresh := time.Since(gate.loadedAt) < gate.ttl
	gate.mu.RUnlock()
	if has && fresh {
		return cached > 0
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	// Re-check under the write lock: another goroutine may have refreshed.
	if _, has = gate.balances[userID]; has && time.Since(gate.loadedAt) < gate.ttl {
		return gate.balances[userID] > 0
	}
	userIDs := make([]uint, 0, len(gate.balances)+1)
	if userID != 0 {
		userIDs = append(userIDs, userID)
	}
	for id := range gate.balances {
		if id != userID {
			userIDs = append(userIDs, id)
		}
	}
	fetched, err := gate.provider(context.Background(), userIDs)
	if err != nil {
		// Fail closed: keep stale positives out by zeroing every entry.
		gate.balances = map[uint]int64{}
		gate.loadedAt = time.Now()
		return false
	}
	balances := make(map[uint]int64, len(fetched))
	for _, id := range userIDs {
		balances[id] = fetched[id]
	}
	gate.balances = balances
	gate.loadedAt = time.Now()
	return balances[userID] > 0
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
