// Package cache provides a thread-safe, TTL-based in-memory cache for
// expensive report and pattern-detection computations.
//
// The cache is safe for concurrent use via a sync.RWMutex.
// Entries expire after their individual TTL; expired entries are evicted
// lazily on Get and proactively when the cache is full.
// Call Invalidate to flush all entries (e.g. after a sync that introduces
// new transactions).
package cache

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// entry holds a single cached value with its expiry time and a hit counter.
type entry struct {
	value     any
	expiresAt time.Time
	hits      int64
}

// Cache is a thread-safe TTL cache with a configurable capacity limit.
type Cache struct {
	mu         sync.RWMutex
	entries    map[string]*entry
	defaultTTL time.Duration
	maxEntries int

	// Stats counters; updated under mu.
	hits   int64
	misses int64
}

// New creates a Cache with the given default TTL and capacity limit.
// Zero or negative values are replaced by sensible defaults (60 s / 100
// entries).
func New(defaultTTL time.Duration, maxEntries int) *Cache {
	if defaultTTL <= 0 {
		defaultTTL = 60 * time.Second
	}
	if maxEntries <= 0 {
		maxEntries = 100
	}
	return &Cache{
		entries:    make(map[string]*entry, maxEntries),
		defaultTTL: defaultTTL,
		maxEntries: maxEntries,
	}
}

// Get returns the cached value for key and true if the entry exists and has
// not expired. Returns nil, false otherwise.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		c.misses++
		return nil, false
	}
	e.hits++
	c.hits++
	return e.value, true
}

// Set stores val under key with the given TTL.
// Pass 0 to use the cache's default TTL.
// When the cache is at capacity, the 10% oldest-expiry entries are evicted
// before inserting.
func (c *Cache) Set(key string, val any, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Only evict if we are adding a new key, not updating one that exists.
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evictOldest()
	}

	c.entries[key] = &entry{
		value:     val,
		expiresAt: time.Now().Add(ttl),
	}
}

// Delete removes key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Invalidate removes all entries from the cache.
// Call this after a sync run that may have introduced new transactions.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.entries = make(map[string]*entry, c.maxEntries)
	c.mu.Unlock()
}

// Stats returns a snapshot of the cache's operational metrics.
func (c *Cache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}
	return Stats{
		Entries: len(c.entries),
		Hits:    c.hits,
		Misses:  c.misses,
		HitRate: hitRate,
	}
}

// Stats holds a snapshot of cache operational metrics.
type Stats struct {
	Entries int
	Hits    int64
	Misses  int64
	HitRate float64 // 0.0–1.0
}

// evictOldest removes the earliest-expiring 10% of entries.
// Caller must hold c.mu.
func (c *Cache) evictOldest() {
	toEvict := len(c.entries) / 10
	if toEvict < 1 {
		toEvict = 1
	}

	type keyExpiry struct {
		key       string
		expiresAt time.Time
	}
	ranked := make([]keyExpiry, 0, len(c.entries))
	for k, e := range c.entries {
		ranked = append(ranked, keyExpiry{k, e.expiresAt})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].expiresAt.Before(ranked[j].expiresAt)
	})
	for i := 0; i < toEvict && i < len(ranked); i++ {
		delete(c.entries, ranked[i].key)
	}
}

// ---------------------------------------------------------------------------
// Package-level singletons (mirrors Python's module-level cache instances)
// ---------------------------------------------------------------------------

var (
	patternCacheOnce sync.Once
	patternCache     *Cache

	reportCacheOnce sync.Once
	reportCache     *Cache
)

// PatternCache returns the shared pattern-detection cache
// (TTL 5 min, 50 entries).
func PatternCache() *Cache {
	patternCacheOnce.Do(func() {
		patternCache = New(5*time.Minute, 50)
	})
	return patternCache
}

// ReportCache returns the shared report-result cache
// (TTL 60 s, 20 entries).
func ReportCache() *Cache {
	reportCacheOnce.Do(func() {
		reportCache = New(60*time.Second, 20)
	})
	return reportCache
}

// InvalidateAll flushes both the pattern cache and the report cache.
// Call after a sync run.
func InvalidateAll() {
	PatternCache().Invalidate()
	ReportCache().Invalidate()
}

// AllStats returns a map of cache name → Stats for observability.
func AllStats() map[string]Stats {
	return map[string]Stats{
		"pattern_cache": PatternCache().Stats(),
		"report_cache":  ReportCache().Stats(),
	}
}

// ---------------------------------------------------------------------------
// Cache key helpers
// ---------------------------------------------------------------------------

// Key generates a deterministic cache key from an arbitrary set of
// positional and named arguments. Arguments are JSON-serialised, so
// any JSON-marshallable value is acceptable.
func Key(args ...any) string {
	// Build a stable representation: positional args + sorted kwargs are not
	// applicable in Go, so we hash the JSON encoding of the args slice.
	data, err := json.Marshal(args)
	if err != nil {
		// Fallback: use fmt representation.
		data = []byte(fmt.Sprint(args...))
	}
	sum := md5.Sum(data) //nolint:gosec // MD5 is fine for cache keys; not security-sensitive.
	return fmt.Sprintf("%x", sum)
}
