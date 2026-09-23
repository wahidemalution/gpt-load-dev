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
