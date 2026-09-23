package ratelimit

import (
	"sync"
)

// AccessKeyConcurrency bounds simultaneous in-flight requests per access key.
//
// ponytail: process-local counters under a single mutex. In a multi-instance
// deployment each instance enforces the limit independently, so the effective
// global limit is `limit * instances`. Upgrade path: back the counter with a
// shared store (Redis/DB atomic) if cross-instance enforcement is required.
// A single mutex is deliberate: the map lookup, the count check/increment, and
// the count-zero cleanup must all be atomic with respect to each other, and at
// this scale one lock is simpler and cheap enough. Shard it only if contention
// is measured.
type AccessKeyConcurrency struct {
	mu      sync.Mutex
	entries map[uint]int64
}

func NewAccessKeyConcurrency() *AccessKeyConcurrency {
	return &AccessKeyConcurrency{entries: make(map[uint]int64)}
}

// Acquire reserves one in-flight slot for the key. It returns a release
// function that must be called exactly once when the request finishes, even on
// error paths. A limit <= 0 means unlimited and never touches the counter.
func (c *AccessKeyConcurrency) Acquire(accessKeyID uint, limit int64) (func(), bool) {
	if c == nil || accessKeyID == 0 || limit <= 0 {
		return func() {}, true
	}

	c.mu.Lock()
	if c.entries[accessKeyID] >= limit {
		c.mu.Unlock()
		return func() {}, false
	}
	c.entries[accessKeyID]++
	c.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.entries[accessKeyID]--
			if c.entries[accessKeyID] == 0 {
				delete(c.entries, accessKeyID)
			}
			c.mu.Unlock()
		})
	}, true
}

func (c *AccessKeyConcurrency) InFlight(accessKeyID uint) int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[accessKeyID]
}
