package ratelimit

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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
	const (
		key      = 9
		workers  = 64
		attempts = 20000
	)
	// Oracle: an external holder gauge. A goroutine increments the gauge
	// immediately after Acquire returns true and decrements it as the FIRST
	// action inside release (before the limiter is touched). This ordering is
	// what makes the gauge sound: every counted holder still owns its slot, so
	// the gauge can never over-count a freed slot. A limiter that hands out
	// more than `limit` slots (e.g. via two live counters for one key) will
	// push the gauge above `limit`.
	for _, limit := range []int64{1, 2, 3} {
		limit := limit
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			c := NewAccessKeyConcurrency()
			var holders, maxHolders, overLimit atomic.Int64
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < attempts; j++ {
						release, ok := c.Acquire(key, limit)
						if !ok {
							continue
						}
						held := holders.Add(1)
						for {
							previous := maxHolders.Load()
							if held <= previous || maxHolders.CompareAndSwap(previous, held) {
								break
							}
						}
						if held > limit {
							overLimit.Add(1)
						}
						runtime.Gosched()
						wrapped := release
						release = func() {
							holders.Add(-1)
							wrapped()
						}
						release()
					}
				}()
			}
			wg.Wait()
			if got := maxHolders.Load(); got > limit {
				t.Fatalf("invariant violated: maxHolders=%d limit=%d over=%d", got, limit, overLimit.Load())
			}
			if got := c.InFlight(key); got != 0 {
				t.Fatalf("InFlight = %d, want 0", got)
			}
			t.Logf("limit=%d maxHolders=%d", limit, maxHolders.Load())
		})
	}
}
