package mailcloak

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCacheConcurrentAccess(t *testing.T) {
	t.Parallel()

	cache := NewCache(time.Minute)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := range 500 {
				key := fmt.Sprintf("worker-%d-key-%d", worker, j%8)
				cache.Put(key, "value", true)
				_, _, _ = cache.Get(key)
				_, _, _ = cache.Get("shared")
			}
		}(i)
	}

	wg.Wait()

	cache.Put("shared", "final", true)
	val, ok, hit := cache.Get("shared")
	if !hit || !ok || val != "final" {
		t.Fatalf("expected final cached value, got val=%q ok=%v hit=%v", val, ok, hit)
	}
}

func TestCacheExpiresEntries(t *testing.T) {
	t.Parallel()

	cache := NewCache(10 * time.Millisecond)
	cache.Put("alice", "alice@example.com", true)

	if val, ok, hit := cache.Get("alice"); !hit || !ok || val != "alice@example.com" {
		t.Fatalf("expected cached entry before expiration, got val=%q ok=%v hit=%v", val, ok, hit)
	}

	time.Sleep(20 * time.Millisecond)

	if _, _, hit := cache.Get("alice"); hit {
		t.Fatal("expected cached entry to expire")
	}
}
