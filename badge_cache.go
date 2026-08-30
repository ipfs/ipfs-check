package main

import (
	"sync"
	"time"
)

// BadgeCache provides a thread-safe in-memory cache for badge results.
// This prevents repeated expensive CID checks for frequently requested badges.
type BadgeCache struct {
	mu    sync.RWMutex
	items map[string]*BadgeCacheEntry
	ttl   time.Duration
}

// BadgeCacheEntry holds cached badge data for a CID.
type BadgeCacheEntry struct {
	ProviderCount int
	CIDSuffix     string
	SVG           []byte
	Timestamp     time.Time
	Pending       bool // true if check is in progress
}

// NewBadgeCache creates a new badge cache with the specified TTL.
func NewBadgeCache(ttl time.Duration) *BadgeCache {
	cache := &BadgeCache{
		items: make(map[string]*BadgeCacheEntry),
		ttl:   ttl,
	}
	// Start background cleanup goroutine
	go cache.cleanupLoop()
	return cache
}

// Get retrieves a cached entry if it exists and hasn't expired.
func (c *BadgeCache) Get(cid string) (*BadgeCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.items[cid]
	if !ok {
		return nil, false
	}

	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry, true
}

// Set stores a badge cache entry.
func (c *BadgeCache) Set(cid string, providerCount int, cidSuffix string, svg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[cid] = &BadgeCacheEntry{
		ProviderCount: providerCount,
		CIDSuffix:     cidSuffix,
		SVG:           svg,
		Timestamp:     time.Now(),
		Pending:       false,
	}
}

// SetPending marks a CID as having a check in progress.
func (c *BadgeCache) SetPending(cid string, cidSuffix string, svg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[cid] = &BadgeCacheEntry{
		CIDSuffix: cidSuffix,
		SVG:       svg,
		Timestamp: time.Now(),
		Pending:   true,
	}
}

// IsPending checks if a CID has a pending check in progress.
func (c *BadgeCache) IsPending(cid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.items[cid]
	if !ok {
		return false
	}
	return entry.Pending
}

// cleanupLoop periodically removes expired entries from the cache.
func (c *BadgeCache) cleanupLoop() {
	ticker := time.NewTicker(c.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup removes all expired entries from the cache.
func (c *BadgeCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for cid, entry := range c.items {
		if now.Sub(entry.Timestamp) > c.ttl {
			delete(c.items, cid)
		}
	}
}

// Size returns the current number of entries in the cache.
func (c *BadgeCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}
