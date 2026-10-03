package content

import (
	"container/list"
	"crypto/sha256"
	"sync"
)

const excerptCacheCapacity = 256
const excerptCacheMaxRunes = 512

type excerptKey struct {
	Hash  [32]byte
	Limit int
}
type excerptEntry struct {
	Key  excerptKey
	Text string
}

// Cache only a bounded plain-text result, never Markdown, ASTs or permissions.
// Content hashes (not article IDs) make edits and reader-specific inputs distinct.
// Authorization/redaction still happens before the caller invokes Excerpt.
type excerptLRU struct {
	mu      sync.Mutex
	entries map[excerptKey]*list.Element
	order   list.List
}

var excerpts = excerptLRU{entries: make(map[excerptKey]*list.Element)}

func (c *excerptLRU) get(key excerptKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item := c.entries[key]; item != nil {
		c.order.MoveToFront(item)
		return item.Value.(excerptEntry).Text, true
	}
	return "", false
}

func (c *excerptLRU) put(key excerptKey, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item := c.entries[key]; item != nil {
		c.order.MoveToFront(item)
		return
	}
	if len(c.entries) >= excerptCacheCapacity {
		oldest := c.order.Back()
		delete(c.entries, oldest.Value.(excerptEntry).Key)
		c.order.Remove(oldest)
	}
	c.entries[key] = c.order.PushFront(excerptEntry{Key: key, Text: text})
}

func cachedExcerpt(markdown string, limit int) string {
	if limit > excerptCacheMaxRunes {
		return excerpt(markdown, limit)
	}
	key := excerptKey{Hash: sha256.Sum256([]byte(markdown)), Limit: limit}
	if value, ok := excerpts.get(key); ok {
		return value
	}
	value := excerpt(markdown, limit)
	excerpts.put(key, value)
	return value
}
