//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package validator

import (
	"fmt"
	"runtime"
)

// Unsupported platforms fail cache updates safely rather than risk concurrent
// cache corruption. Registry validation itself continues without a cache.
func acquirePlatformCacheFileLock(string) (*cacheFileLock, error) {
	return nil, fmt.Errorf("cross-process cache locking is unavailable on %s", runtime.GOOS)
}
