package auth

import (
	"sync"
	"time"

	"kestrix/internal/domain"
)

// InMemoryIdentityCache is a bounded, TTL-based domain.IdentityCache. It is
// process-local: see design.md D7's equivalent limit on the login throttle -
// this cache carries the same limit for the same reason, and design.md D4
// documents the trade-off.
//
// A zero TTL disables the cache: Get always misses and Set is a no-op, so a
// deployment that disagrees with caching a privilege decision at all can set
// KESTRIX_IDENTITY_CACHE_TTL=0 and every request resolves fresh from the
// store.
type InMemoryIdentityCache struct {
	ttl        time.Duration
	maxEntries int
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	value     domain.IdentityCacheEntry
	expiresAt time.Time
}

// NewInMemoryIdentityCache constructs a cache with the given TTL and a
// maximum number of held entries. now is the clock seam: production wiring
// passes time.Now, tests inject a controllable clock. maxEntries <= 0 means
// unbounded.
func NewInMemoryIdentityCache(ttl time.Duration, maxEntries int, now func() time.Time) *InMemoryIdentityCache {
	if now == nil {
		now = time.Now
	}
	return &InMemoryIdentityCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		now:        now,
		entries:    make(map[string]cacheEntry),
	}
}

var _ domain.IdentityCache = (*InMemoryIdentityCache)(nil)

// Get reports a miss both when name has never been cached and when its
// entry's TTL has elapsed; the caller cannot distinguish the two and does
// not need to - both mean "resolve from the store."
func (c *InMemoryIdentityCache) Get(name string) (domain.IdentityCacheEntry, bool) {
	if c.ttl <= 0 {
		return domain.IdentityCacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.entries[name]
	if !ok {
		return domain.IdentityCacheEntry{}, false
	}
	if !c.now().Before(item.expiresAt) {
		delete(c.entries, name)
		return domain.IdentityCacheEntry{}, false
	}
	return item.value, true
}

// Set stores entry for name, evicting one arbitrary existing entry first if
// the cache is at its bound and name is not already present. A zero TTL
// makes this a no-op.
func (c *InMemoryIdentityCache) Set(name string, entry domain.IdentityCacheEntry) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[name]; !exists && c.maxEntries > 0 && len(c.entries) >= c.maxEntries {
		for evict := range c.entries {
			delete(c.entries, evict)
			break
		}
	}
	c.entries[name] = cacheEntry{value: entry, expiresAt: c.now().Add(c.ttl)}
}

// Invalidate drops any cached entry for name, so the next Get misses and the
// caller resolves fresh from the store.
func (c *InMemoryIdentityCache) Invalidate(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, name)
}
