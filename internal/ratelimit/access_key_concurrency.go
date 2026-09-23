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
	entries sync.Map // uint -> *keyCounter
}

// keyCounter pairs a per-key mutex with the in-flight count. The mutex makes
// the check-increment and decrement-and-maybe-delete critical sections atomic
// with respect to each other, so the limit can never be exceeded and a live
// entry is never deleted out from under a concurrent Acquire. The atomic
// counter is retained only so InFlight can read without taking the lock.
type keyCounter struct {
	mu    sync.Mutex
	value atomic.Int64
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
	counter.mu.Lock()
	if counter.value.Load() >= limit {
		counter.mu.Unlock()
		return func() {}, false
	}
	counter.value.Add(1)
	counter.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			counter.mu.Lock()
			counter.value.Add(-1)
			if counter.value.Load() == 0 {
				c.entries.CompareAndDelete(accessKeyID, counter)
			}
			counter.mu.Unlock()
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
	return value.(*keyCounter).value.Load()
}

func (c *AccessKeyConcurrency) counter(accessKeyID uint) *keyCounter {
	if value, ok := c.entries.Load(accessKeyID); ok {
		return value.(*keyCounter)
	}
	created := &keyCounter{}
	actual, _ := c.entries.LoadOrStore(accessKeyID, created)
	return actual.(*keyCounter)
}
