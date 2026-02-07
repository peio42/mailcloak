package mailcloak

import "time"

type Cache struct {
	ttl time.Duration
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
	it, ok := c.m[key]
	if !ok || time.Now().After(it.expires) {
		return "", false, false
	}
	return it.val, it.ok, true
}

func (c *Cache) Put(key, val string, ok bool) {
	c.m[key] = cacheItem{val: val, ok: ok, expires: time.Now().Add(c.ttl)}
}
