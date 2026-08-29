//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package validator

import (
	"fmt"
	"os"
	"syscall"
)

func acquirePlatformCacheFileLock(lockPath string) (*cacheFileLock, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache lock file: %w", err)
	}
	if err := flock(file, syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire cache lock: %w", err)
	}
	return &cacheFileLock{unlock: func() error {
		unlockErr := flock(file, syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			if closeErr != nil {
				return fmt.Errorf("unlock cache lock: %v; close cache lock: %w", unlockErr, closeErr)
			}
			return fmt.Errorf("unlock cache lock: %w", unlockErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close cache lock: %w", closeErr)
		}
		return nil
	}}, nil
}

func flock(file *os.File, operation int) error {
	for {
		err := syscall.Flock(int(file.Fd()), operation)
		if err != syscall.EINTR {
			return err
		}
	}
}
