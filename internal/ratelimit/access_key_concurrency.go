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
