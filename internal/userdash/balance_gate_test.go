package userdash

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestGate(provider func(ctx context.Context, userIDs []uint) (map[uint]int64, error), ttl time.Duration) *BalanceGate {
	return NewBalanceGate(provider, ttl)
}

func TestBalanceGateSlowProviderSingleFlight(t *testing.T) {
	var calls atomic.Int64
	gate := newTestGate(func(ctx context.Context, userIDs []uint) (map[uint]int64, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		balances := make(map[uint]int64, len(userIDs))
		for _, id := range userIDs {
			balances[id] = 100
		}
		return balances, nil
	}, time.Minute)

	var wg sync.WaitGroup
	results := make([]bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = gate.Allow(7)
		}(i)
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 (single-flight refresh)", got)
	}
	for i, ok := range results {
		if !ok {
			t.Fatalf("results[%d] = false, want true for all 20 concurrent callers", i)
		}
	}
}

func TestBalanceGateInvalidateRefreshesBeforeTTL(t *testing.T) {
	balance := int64(0)
	gate := newTestGate(func(ctx context.Context, userIDs []uint) (map[uint]int64, error) {
		balances := make(map[uint]int64, len(userIDs))
		for _, id := range userIDs {
			balances[id] = balance
		}
		return balances, nil
	}, time.Minute)

	if gate.Allow(7) {
		t.Fatal("Allow(7) = true, want false at zero balance")
	}
	balance = 500
	if gate.Allow(7) {
		t.Fatal("Allow(7) = true before Invalidate, want cached false")
	}
	gate.Invalidate()
	if !gate.Allow(7) {
		t.Fatal("Allow(7) = false after Invalidate+topup, want true before TTL")
	}
}
