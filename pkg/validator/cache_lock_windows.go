//go:build windows

package validator

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const lockfileExclusiveLock = 0x00000002

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	lockFileExProc   = kernel32.NewProc("LockFileEx")
	unlockFileExProc = kernel32.NewProc("UnlockFileEx")
)

func acquirePlatformCacheFileLock(lockPath string) (*cacheFileLock, error) {
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache lock file: %w", err)
	}
	overlapped := &syscall.Overlapped{}
	if err := lockWindowsFile(file, overlapped); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire cache lock: %w", err)
	}
	return &cacheFileLock{unlock: func() error {
		unlockErr := unlockWindowsFile(file, overlapped)
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

func lockWindowsFile(file *os.File, overlapped *syscall.Overlapped) error {
	result, _, callErr := lockFileExProc.Call(
		file.Fd(),
		uintptr(lockfileExclusiveLock),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(overlapped)),
	)
	if result != 0 {
		return nil
	}
	return windowsLockError("LockFileEx", callErr)
}

func unlockWindowsFile(file *os.File, overlapped *syscall.Overlapped) error {
	result, _, callErr := unlockFileExProc.Call(
		file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(overlapped)),
	)
	if result != 0 {
		return nil
	}
	return windowsLockError("UnlockFileEx", callErr)
}

func windowsLockError(operation string, callErr error) error {
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("%s: %w", operation, callErr)
	}
	return fmt.Errorf("%s failed", operation)
}
