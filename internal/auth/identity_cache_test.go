package auth

import (
	"testing"
	"time"

	"kestrix/internal/domain"
)

// stoppedClock returns a fixed time that the test advances manually,
// exercising InMemoryIdentityCache's injected-clock seam without a real
// sleep.
type stoppedClock struct{ t time.Time }

func (c *stoppedClock) now() time.Time { return c.t }

func TestInMemoryIdentityCache_HitAfterSet(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(5*time.Second, 0, clock.now)
	entry := domain.IdentityCacheEntry{Exists: true, Role: domain.RoleOperator, TokenVersion: 3}
	c.Set("alice", entry)

	got, ok := c.Get("alice")
	if !ok {
		t.Fatal("Get() ok = false, want true after Set")
	}
	if got != entry {
		t.Fatalf("Get() = %+v, want %+v", got, entry)
	}
}

func TestInMemoryIdentityCache_MissBeforeSet(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(5*time.Second, 0, clock.now)
	if _, ok := c.Get("nobody"); ok {
		t.Fatal("Get() ok = true, want false for a name never cached")
	}
}

func TestInMemoryIdentityCache_ExpiresAtTTL(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(5*time.Second, 0, clock.now)
	c.Set("bob", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleViewer})

	clock.t = clock.t.Add(4 * time.Second)
	if _, ok := c.Get("bob"); !ok {
		t.Fatal("Get() ok = false before TTL elapsed, want true")
	}

	clock.t = clock.t.Add(2 * time.Second) // total 6s > 5s TTL
	if _, ok := c.Get("bob"); ok {
		t.Fatal("Get() ok = true after TTL elapsed, want false")
	}
}

func TestInMemoryIdentityCache_NegativeResultIsCached(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(5*time.Second, 0, clock.now)
	c.Set("ghost", domain.IdentityCacheEntry{Exists: false})

	got, ok := c.Get("ghost")
	if !ok {
		t.Fatal("Get() ok = false, want true for a cached negative result")
	}
	if got.Exists {
		t.Fatal("Get().Exists = true, want false")
	}
}

func TestInMemoryIdentityCache_EvictsAtBound(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(time.Minute, 2, clock.now)
	c.Set("a", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleViewer})
	c.Set("b", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleViewer})
	c.Set("c", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleViewer})

	if got := len(c.entries); got > 2 {
		t.Fatalf("cache holds %d entries, want at most 2 (bound)", got)
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("Get(\"c\") ok = false, want true - the entry that triggered eviction must itself be present")
	}
}

func TestInMemoryIdentityCache_ZeroTTLDisablesCaching(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(0, 0, clock.now)
	c.Set("dana", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleAdmin})
	if _, ok := c.Get("dana"); ok {
		t.Fatal("Get() ok = true with TTL=0, want caching disabled entirely")
	}
	if len(c.entries) != 0 {
		t.Fatalf("entries stored = %d with TTL=0, want 0 (Set must be a no-op)", len(c.entries))
	}
}

func TestInMemoryIdentityCache_Invalidate(t *testing.T) {
	clock := &stoppedClock{t: time.Now()}
	c := NewInMemoryIdentityCache(time.Minute, 0, clock.now)
	c.Set("erin", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleOperator})
	c.Invalidate("erin")
	if _, ok := c.Get("erin"); ok {
		t.Fatal("Get() ok = true after Invalidate, want false")
	}
}

func TestInMemoryIdentityCache_InvalidateUnknownNameIsSafe(t *testing.T) {
	c := NewInMemoryIdentityCache(time.Minute, 0, time.Now)
	c.Invalidate("never-cached") // must not panic
}

func TestInMemoryIdentityCache_NilClockDefaultsToRealTime(t *testing.T) {
	c := NewInMemoryIdentityCache(time.Minute, 0, nil)
	c.Set("frank", domain.IdentityCacheEntry{Exists: true, Role: domain.RoleViewer})
	if _, ok := c.Get("frank"); !ok {
		t.Fatal("Get() ok = false with default clock, want true")
	}
}
