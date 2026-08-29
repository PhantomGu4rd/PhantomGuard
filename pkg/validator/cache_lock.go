package validator

import (
	"fmt"
	"os"
	"path/filepath"
)

// cacheFileLock protects one cache file across operating-system processes.
// The platform implementation keeps the lock file in place; its OS-managed
// lock is released automatically if a process exits unexpectedly.
type cacheFileLock struct {
	unlock func() error
}

func (l *cacheFileLock) Unlock() error {
	if l == nil || l.unlock == nil {
		return nil
	}
	unlock := l.unlock
	l.unlock = nil
	return unlock()
}

func acquireCacheFileLock(cachePath string) (*cacheFileLock, error) {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	return acquirePlatformCacheFileLock(cachePath + ".lock")
}
