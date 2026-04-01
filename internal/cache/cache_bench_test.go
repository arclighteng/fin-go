package cache_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/cache"
)

// prePopulate fills c with n entries keyed "key-000000" … "key-N".
func prePopulate(c *cache.Cache, n int) {
	for i := 0; i < n; i++ {
		c.Set(fmt.Sprintf("key-%06d", i), i, time.Minute)
	}
}

func BenchmarkCacheSet(b *testing.B) {
	c := cache.New(time.Minute, 10_000)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, time.Minute)
	}
}

func BenchmarkCacheGet(b *testing.B) {
	const n = 1000
	c := cache.New(time.Minute, n)
	prePopulate(c, n)

	b.ReportAllocs()
	b.ResetTimer()

	var hit bool
	for i := 0; i < b.N; i++ {
		_, hit = c.Get(fmt.Sprintf("key-%06d", i%n))
	}
	_ = hit
}

func BenchmarkCacheGetParallel(b *testing.B) {
	const n = 1000
	c := cache.New(time.Minute, n)
	prePopulate(c, n)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = c.Get(fmt.Sprintf("key-%06d", i%n))
			i++
		}
	})
}

func BenchmarkCacheSetParallel(b *testing.B) {
	// Use a large capacity so eviction does not dominate.
	c := cache.New(time.Minute, 100_000)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Set(fmt.Sprintf("key-parallel-%d", i), i, time.Minute)
			i++
		}
	})
}
