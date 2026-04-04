package bridge

import "sync"

// EpochCache is an in-memory cache of session epochs backed by DB reads.
type EpochCache struct {
	mu    sync.RWMutex
	cache map[string]int
}

func NewEpochCache() *EpochCache {
	return &EpochCache{cache: make(map[string]int)}
}

// Get returns the cached epoch for a session. ok=false if not cached.
func (c *EpochCache) Get(sessionID string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.cache[sessionID]
	return v, ok
}

// Set stores the epoch for a session.
func (c *EpochCache) Set(sessionID string, epoch int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[sessionID] = epoch
}

// Invalidate removes the cached epoch for a session.
// Called after epoch bump so the next validation reads from DB.
func (c *EpochCache) Invalidate(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, sessionID)
}
