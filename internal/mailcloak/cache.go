package mailcloak

import (
	"sync"
	"time"
)

type Cache struct {
	ttl time.Duration
	mu  sync.RWMutex
	m   map[string]cacheItem
}

type cacheItem struct {
	val     string
	expires time.Time
	ok      bool
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, m: make(map[string]cacheItem)}
}

func (c *Cache) Get(key string) (string, bool, bool) {
	c.mu.RLock()
	it, ok := c.m[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(it.expires) {
		return "", false, false
	}
	return it.val, it.ok, true
}

func (c *Cache) Put(key, val string, ok bool) {
	c.mu.Lock()
	c.m[key] = cacheItem{val: val, ok: ok, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}
