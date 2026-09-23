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

// TestBalanceGateConcurrentColdRefreshIncludesEveryCaller is the regression
// test for a single-flight race: when two different first-time users race a
// cold cache, the joiner's ID is absent from the in-flight refresh, so it must
// retry with a refresh that includes its own ID instead of failing closed and
// handing a funded user a spurious 402.
func TestBalanceGateConcurrentColdRefreshIncludesEveryCaller(t *testing.T) {
	entered := make(chan struct{}, 1)
	joinerReady := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	gate := newTestGate(func(ctx context.Context, userIDs []uint) (map[uint]int64, error) {
		// The first (leader) refresh blocks so the second caller must join the
		// in-flight batch, whose IDs do not contain its own.
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		balances := make(map[uint]int64, len(userIDs))
		for _, id := range userIDs {
			balances[id] = 100
		}
		return balances, nil
	}, time.Minute)

	var wg sync.WaitGroup
	results := make([]bool, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0] = gate.Allow(11) // leader: starts the cold refresh
	}()
	<-entered // leader is now inside the provider, holding the single-flight slot

	wg.Add(1)
	go func() {
		defer wg.Done()
		close(joinerReady)
		results[1] = gate.Allow(22) // joiner: must wait on the leader's refresh
	}()
	<-joinerReady
	// Give the joiner time to reach awaitRefresh and block on the in-flight
	// batch before the leader's provider call returns.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, ok := range results {
		if !ok {
			t.Fatalf("results[%d] = false, want true: a funded user must not 402 after a cold concurrent refresh", i)
		}
	}
}

// TestBalanceGateProviderErrorFailsClosedOnce pins the fail-closed contract:
// a provider error denies access and, crucially, is not retried (one call), so
// a database outage is not amplified by the joiner loop.
func TestBalanceGateProviderErrorFailsClosedOnce(t *testing.T) {
	var calls atomic.Int64
	gate := newTestGate(func(ctx context.Context, userIDs []uint) (map[uint]int64, error) {
		calls.Add(1)
		return nil, context.DeadlineExceeded
	}, time.Minute)

	if gate.Allow(7) {
		t.Fatal("Allow(7) = true, want false when the provider errors")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 (fail closed without retrying a failing backend)", got)
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
