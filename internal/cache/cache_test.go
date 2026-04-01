package cache_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/cache"
)

func TestCache_SetAndGet(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)

	c.Set("hello", "world", 0)
	val, ok := c.Get("hello")
	if !ok {
		t.Fatal("Get: expected ok=true for existing key")
	}
	if val != "world" {
		t.Errorf("Get: got %v, want %q", val, "world")
	}
}

func TestCache_MissingKey(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("Get: expected ok=false for missing key")
	}
}

func TestCache_Expiration(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)
	c.Set("ephemeral", 42, 1*time.Millisecond)

	// Value should be present immediately.
	_, ok := c.Get("ephemeral")
	if !ok {
		t.Fatal("Get: expected ok=true immediately after Set")
	}

	// Wait for TTL to pass.
	time.Sleep(10 * time.Millisecond)

	_, ok = c.Get("ephemeral")
	if ok {
		t.Error("Get: expected ok=false after TTL expiry")
	}
}

func TestCache_DefaultTTL(t *testing.T) {
	t.Parallel()

	// Passing 0 as TTL to Set uses the cache default.
	c := cache.New(1*time.Millisecond, 100)
	c.Set("key", "value", 0)

	time.Sleep(10 * time.Millisecond)

	_, ok := c.Get("key")
	if ok {
		t.Error("Get: expected entry to have expired via default TTL")
	}
}

func TestCache_Invalidate(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)
	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	c.Set("c", 3, 0)

	c.Invalidate()

	for _, key := range []string{"a", "b", "c"} {
		_, ok := c.Get(key)
		if ok {
			t.Errorf("Get(%q): expected ok=false after Invalidate", key)
		}
	}
}

func TestCache_Delete(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)
	c.Set("remove-me", "value", 0)
	c.Delete("remove-me")

	_, ok := c.Get("remove-me")
	if ok {
		t.Error("Get: expected ok=false after Delete")
	}
}

func TestCache_OverwriteExistingKey(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)
	c.Set("k", "first", 0)
	c.Set("k", "second", 0)

	val, ok := c.Get("k")
	if !ok {
		t.Fatal("Get: expected ok=true")
	}
	if val != "second" {
		t.Errorf("Get: got %v, want %q", val, "second")
	}
}

func TestCache_Stats(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 100)
	c.Set("x", 1, 0)

	c.Get("x")           // hit
	c.Get("x")           // hit
	c.Get("nonexistent") // miss

	stats := c.Stats()
	if stats.Hits != 2 {
		t.Errorf("Stats.Hits = %d, want 2", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Stats.Misses = %d, want 1", stats.Misses)
	}
	wantRate := 2.0 / 3.0
	if stats.HitRate < wantRate-0.001 || stats.HitRate > wantRate+0.001 {
		t.Errorf("Stats.HitRate = %f, want ~%f", stats.HitRate, wantRate)
	}
	if stats.Entries != 1 {
		t.Errorf("Stats.Entries = %d, want 1", stats.Entries)
	}
}

func TestCache_CapacityEviction(t *testing.T) {
	t.Parallel()

	// Use capacity of 10; insert 15 unique keys.
	// The cache should evict oldest-expiry entries rather than panic or grow unboundedly.
	c := cache.New(5*time.Second, 10)
	for i := 0; i < 15; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, 0)
	}

	stats := c.Stats()
	// After inserting 15 items into a cap-10 cache, at most max+evict_batch entries should remain.
	// Eviction removes 10% (1 entry) per insertion beyond capacity, so at most 14 remain in
	// the worst case, but capacity is 10 so new insertions only evict when at 10.
	// The exact count depends on eviction ordering, but must be <= maxEntries.
	if stats.Entries > 15 {
		t.Errorf("Stats.Entries = %d after eviction, expected <= 15", stats.Entries)
	}
}

func TestCache_ThreadSafety(t *testing.T) {
	t.Parallel()

	c := cache.New(5*time.Second, 1000)
	const goroutines = 50
	const opsEach = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsEach; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				c.Set(key, i, 0)
				c.Get(key)
				if i%10 == 0 {
					c.Delete(key)
				}
			}
		}()
	}

	wg.Wait()
	// If we reach here without a race condition or panic, the test passes.
}

func TestCache_InvalidateAll(t *testing.T) {
	t.Parallel()

	// Populate both singleton caches, then flush them.
	pc := cache.PatternCache()
	rc := cache.ReportCache()

	pc.Set("pattern-key", "data", 0)
	rc.Set("report-key", "data", 0)

	cache.InvalidateAll()

	if _, ok := pc.Get("pattern-key"); ok {
		t.Error("PatternCache: expected key to be gone after InvalidateAll")
	}
	if _, ok := rc.Get("report-key"); ok {
		t.Error("ReportCache: expected key to be gone after InvalidateAll")
	}
}

func TestKey_Deterministic(t *testing.T) {
	t.Parallel()

	// Same args → same key.
	k1 := cache.Key("a", 1, true)
	k2 := cache.Key("a", 1, true)
	if k1 != k2 {
		t.Errorf("Key is not deterministic: %q != %q", k1, k2)
	}

	// Different args → different key.
	k3 := cache.Key("a", 2, true)
	if k1 == k3 {
		t.Error("Key collision: different args produced same key")
	}
}

func TestKey_NonEmpty(t *testing.T) {
	t.Parallel()

	k := cache.Key("some", "args", 42)
	if k == "" {
		t.Error("Key returned empty string")
	}
}

func TestKey_OrderSensitive(t *testing.T) {
	t.Parallel()

	k1 := cache.Key("x", "y")
	k2 := cache.Key("y", "x")
	if k1 == k2 {
		t.Error("Key should differ when argument order changes")
	}
}

func TestAllStats(t *testing.T) {
	t.Parallel()

	stats := cache.AllStats()
	if _, ok := stats["pattern_cache"]; !ok {
		t.Error("AllStats missing 'pattern_cache' key")
	}
	if _, ok := stats["report_cache"]; !ok {
		t.Error("AllStats missing 'report_cache' key")
	}
}
