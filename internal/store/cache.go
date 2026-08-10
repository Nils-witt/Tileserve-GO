// Package store implements PostgreSQL-backed persistence for tileserve-go:
// users, maps, map versions, permissions, geo objects, and refresh tokens.
package store

import (
	"sync"
	"time"
)

// ttlCache is a generic, short-lived read cache. It's used to keep
// frequently-read, rarely-changed rows (maps, permissions) out of the
// database on the tile-serving hot path, while still picking up changes
// within one ttl window.
type ttlCache[K comparable, V any] struct {
	ttl time.Duration

	mu sync.RWMutex
	m  map[K]ttlEntry[V]
}

type ttlEntry[V any] struct {
	value   V
	expires time.Time
}

func newTTLCache[K comparable, V any](ttl time.Duration) *ttlCache[K, V] {
	return &ttlCache[K, V]{ttl: ttl, m: make(map[K]ttlEntry[V])}
}

func (c *ttlCache[K, V]) get(key K) (V, bool) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(e.expires) {
		var zero V
		return zero, false
	}

	return e.value, true
}

func (c *ttlCache[K, V]) set(key K, value V) {
	c.mu.Lock()
	c.m[key] = ttlEntry[V]{value: value, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

func (c *ttlCache[K, V]) invalidate(key K) {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
}
