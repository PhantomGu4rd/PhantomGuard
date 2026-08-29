// Package validator verifies registry candidates and persists definitive answers.
package validator

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/phantomguard/phantomguard/pkg/model"
)

type cacheEntry struct {
	Status    model.Status `json:"status"`
	CheckedAt time.Time    `json:"checked_at"`
}

// Cache is a thread-safe JSON cache with atomic on-disk replacement and
// cross-process write protection.
type Cache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]cacheEntry
	now     func() time.Time
}

// CacheStats is a privacy-preserving summary suitable for local status views.
// It deliberately contains no package names or cache path.
type CacheStats struct {
	Entries int
	Exists  int
	Phantom int
}

// DefaultCachePath returns ~/.cache/phantomguard/cache.json unless XDG_CACHE_HOME is set.
func DefaultCachePath() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home for cache: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "phantomguard", "cache.json"), nil
}

// NewCache loads a cache from path. A missing cache starts empty.
func NewCache(path string) (*Cache, error) {
	if path == "" {
		var err error
		path, err = DefaultCachePath()
		if err != nil {
			return nil, err
		}
	}
	entries, err := loadCacheEntries(path)
	if err != nil {
		return nil, err
	}
	return &Cache{path: path, entries: entries, now: time.Now}, nil
}

// loadCacheEntries treats malformed cache data as a cache miss. The cache is an
// optimization, never a reason to stop a security scan.
func loadCacheEntries(path string) (map[string]cacheEntry, error) {
	entries := make(map[string]cacheEntry)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, fmt.Errorf("read cache: %w", err)
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		// Preserve the corrupt file for diagnosis; the next successful Put replaces
		// it atomically with a valid cache.
		log.Printf("PhantomGuard: ignoring malformed cache %s: %v", path, err)
		return make(map[string]cacheEntry), nil
	}
	if entries == nil {
		entries = make(map[string]cacheEntry)
	}
	return entries, nil
}

func cacheKey(ecosystem model.Ecosystem, name string) string { return string(ecosystem) + ":" + name }

// Get retrieves only a fresh definitive answer. Unknown responses are never eligible.
func (c *Cache) Get(ecosystem model.Ecosystem, name string, positiveTTL, negativeTTL time.Duration) (model.Status, bool) {
	c.mu.RLock()
	entry, ok := c.entries[cacheKey(ecosystem, name)]
	c.mu.RUnlock()
	if !ok || (entry.Status != model.Exists && entry.Status != model.Phantom) {
		return "", false
	}
	ttl := positiveTTL
	if entry.Status == model.Phantom {
		ttl = negativeTTL
	}
	now := c.now()
	// A future timestamp can result from clock skew or a manually altered
	// cache. Treat it as a miss rather than extending a definitive answer
	// indefinitely past its intended TTL.
	if entry.CheckedAt.After(now) || now.Sub(entry.CheckedAt) > ttl {
		return "", false
	}
	return entry.Status, true
}

// Stats returns a consistent snapshot of the cache's definitive outcomes.
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stats := CacheStats{Entries: len(c.entries)}
	for _, entry := range c.entries {
		switch entry.Status {
		case model.Exists:
			stats.Exists++
		case model.Phantom:
			stats.Phantom++
		}
	}
	return stats
}

// Put saves Exists and Phantom outcomes only, using a temp file and rename to avoid partial JSON files.
func (c *Cache) Put(ecosystem model.Ecosystem, name string, status model.Status) error {
	if status != model.Exists && status != model.Phantom {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.withProcessLockLocked(func() error {
		// A second PhantomGuard process may have updated the cache since this
		// instance was created. Reload while holding the process lock so its
		// entries survive this read-modify-write cycle.
		if err := c.reloadLocked(); err != nil {
			return fmt.Errorf("reload cache: %w", err)
		}
		c.entries[cacheKey(ecosystem, name)] = cacheEntry{Status: status, CheckedAt: c.now().UTC()}
		return c.writeLocked()
	})
}

// withProcessLockLocked serializes cache mutations across PhantomGuard
// processes. Callers must hold c.mu before calling it.
func (c *Cache) withProcessLockLocked(operation func() error) (err error) {
	lock, err := acquireCacheFileLock(c.path)
	if err != nil {
		return fmt.Errorf("lock cache: %w", err)
	}
	defer func() {
		if unlockErr := lock.Unlock(); unlockErr != nil && err == nil {
			err = fmt.Errorf("unlock cache: %w", unlockErr)
		}
	}()
	return operation()
}

// reloadLocked replaces the in-memory snapshot with the authoritative on-disk
// cache. Callers must hold c.mu and the process lock.
func (c *Cache) reloadLocked() error {
	entries, err := loadCacheEntries(c.path)
	if err != nil {
		return err
	}
	c.entries = entries
	return nil
}

// writeLocked uses a temp file and rename to avoid partial JSON files.
// Callers must hold both c.mu and the process lock.
func (c *Cache) writeLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	raw, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(c.path), "cache-*.json")
	if err != nil {
		return fmt.Errorf("create cache temp file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write cache temp file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cache temp file: %w", err)
	}
	if err := os.Rename(temporaryName, c.path); err != nil {
		return fmt.Errorf("replace cache atomically: %w", err)
	}
	return nil
}

// Clear removes all cache data after taking the same locks used by concurrent scans.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.withProcessLockLocked(func() error {
		c.entries = make(map[string]cacheEntry)
		err := os.Remove(c.path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear cache: %w", err)
		}
		temporaryFiles, err := filepath.Glob(filepath.Join(filepath.Dir(c.path), "cache-*.json"))
		if err != nil {
			return fmt.Errorf("find cache temp files: %w", err)
		}
		for _, temporary := range temporaryFiles {
			if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove cache temp file: %w", err)
			}
		}
		return nil
	})
}
