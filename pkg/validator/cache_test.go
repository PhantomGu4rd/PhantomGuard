package validator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/phantomguard/phantomguard/pkg/model"
)

const (
	cacheLockHelperEnv    = "PHANTOMGUARD_CACHE_LOCK_HELPER"
	cacheLockPathEnv      = "PHANTOMGUARD_CACHE_LOCK_PATH"
	cacheLockReadyEnv     = "PHANTOMGUARD_CACHE_LOCK_READY"
	cacheLockReleaseEnv   = "PHANTOMGUARD_CACHE_LOCK_RELEASE"
	cacheLockHelperWait   = 5 * time.Second
	cacheLockPollInterval = 10 * time.Millisecond
)

func TestDefaultCachePathUsesXDGOrHomeFallback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := DefaultCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "phantomguard", "cache.json"); path != want {
		t.Fatalf("XDG cache path = %q; want %q", path, want)
	}

	t.Setenv("XDG_CACHE_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path, err = DefaultCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".cache", "phantomguard", "cache.json"); path != want {
		t.Fatalf("home fallback path = %q; want %q", path, want)
	}
}

func TestCacheStoresDefinitiveResultsAndExpiresNegativeEntries(t *testing.T) {
	cache, err := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	if err := cache.Put(model.NPM, "real", model.Exists); err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Get(model.NPM, "real", 7*24*time.Hour, time.Hour); !ok || got != model.Exists {
		t.Fatalf("positive cache got %q, %v", got, ok)
	}
	if err := cache.Put(model.NPM, "ghost", model.Phantom); err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Get(model.NPM, "ghost", 7*24*time.Hour, time.Hour); !ok || got != model.Phantom {
		t.Fatalf("negative cache got %q, %v", got, ok)
	}
	now = now.Add(2 * time.Hour)
	if _, ok := cache.Get(model.NPM, "ghost", 7*24*time.Hour, time.Hour); ok {
		t.Fatal("expired phantom remained cached")
	}
	if got, ok := cache.Get(model.NPM, "real", 7*24*time.Hour, time.Hour); !ok || got != model.Exists {
		t.Fatalf("positive entry expired too early: %q, %v", got, ok)
	}
}

func TestCacheRejectsFutureTimestamp(t *testing.T) {
	cache, err := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	cache.entries[cacheKey(model.NPM, "react")] = cacheEntry{
		Status:    model.Exists,
		CheckedAt: now.Add(time.Minute),
	}
	if _, ok := cache.Get(model.NPM, "react", 7*24*time.Hour, time.Hour); ok {
		t.Fatal("future-dated cache entry was treated as fresh")
	}
}

func TestCacheNeverStoresUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.NPM, "network-failure", model.Unknown); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get(model.NPM, "network-failure", time.Hour, time.Hour); ok {
		t.Fatal("unknown was cached")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unknown result wrote cache file: %v", err)
	}
}

func TestCacheStatsCountsOnlyDefinitiveOutcomes(t *testing.T) {
	cache, err := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.NPM, "react", model.Exists); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.PyPI, "reqeusts", model.Phantom); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.NPM, "offline", model.Unknown); err != nil {
		t.Fatal(err)
	}
	stats := cache.Stats()
	if stats.Entries != 2 || stats.Exists != 1 || stats.Phantom != 1 {
		t.Fatalf("unexpected cache stats: %#v", stats)
	}
}

func TestCacheConcurrentGetPutIsSafe(t *testing.T) {
	cache, err := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errs := make(chan error, 50)
	for index := 0; index < 50; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			name := "package-" + string(rune('a'+index))
			if err := cache.Put(model.NPM, name, model.Exists); err != nil {
				errs <- err
				return
			}
			cache.Get(model.NPM, name, 7*24*time.Hour, time.Hour)
		}(index)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCacheMergesUpdatesFromSeparateInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	first, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put(model.NPM, "react", model.Exists); err != nil {
		t.Fatal(err)
	}
	if err := second.Put(model.PyPI, "requests", model.Exists); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Get(model.NPM, "react", time.Hour, time.Hour); !ok {
		t.Fatal("first cache instance entry was lost")
	}
	if _, ok := loaded.Get(model.PyPI, "requests", time.Hour, time.Hour); !ok {
		t.Fatal("second cache instance entry was not saved")
	}
}

func TestCacheProcessLockBlocksOtherProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	ready := filepath.Join(t.TempDir(), "ready")
	release := filepath.Join(t.TempDir(), "release")
	command := exec.Command(os.Args[0], "-test.run=^TestCacheFileLockHelper$")
	command.Env = append(os.Environ(),
		cacheLockHelperEnv+"=1",
		cacheLockPathEnv+"="+path,
		cacheLockReadyEnv+"="+ready,
		cacheLockReleaseEnv+"="+release,
	)
	if err := command.Start(); err != nil {
		t.Fatalf("start lock helper: %v", err)
	}
	helperDone := make(chan error, 1)
	go func() { helperDone <- command.Wait() }()
	released := false
	helperExited := false
	defer func() {
		if !released {
			_ = os.WriteFile(release, []byte("release"), 0o600)
		}
		if !helperExited {
			select {
			case <-helperDone:
			case <-time.After(cacheLockHelperWait):
				_ = command.Process.Kill()
				<-helperDone
			}
		}
	}()
	if err := waitForCacheLockFile(ready, cacheLockHelperWait); err != nil {
		t.Fatalf("wait for lock helper: %v", err)
	}

	cache, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	putDone := make(chan error, 1)
	putStarted := make(chan struct{})
	go func() {
		close(putStarted)
		putDone <- cache.Put(model.NPM, "react", model.Exists)
	}()
	<-putStarted
	select {
	case err := <-putDone:
		t.Fatalf("cache Put completed before the other process released its lock: %v", err)
	case <-time.After(150 * time.Millisecond):
		// The helper owns the OS-level lock, so Put must still be blocked.
	}

	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatalf("release lock helper: %v", err)
	}
	released = true
	select {
	case err := <-helperDone:
		helperExited = true
		if err != nil {
			t.Fatalf("lock helper failed: %v", err)
		}
	case <-time.After(cacheLockHelperWait):
		t.Fatal("lock helper did not exit after release")
	}
	select {
	case err := <-putDone:
		if err != nil {
			t.Fatalf("cache Put after lock release: %v", err)
		}
	case <-time.After(cacheLockHelperWait):
		t.Fatal("cache Put remained blocked after lock release")
	}
}

// TestCacheFileLockHelper is run in a separate test process by
// TestCacheProcessLockBlocksOtherProcesses.
func TestCacheFileLockHelper(t *testing.T) {
	if os.Getenv(cacheLockHelperEnv) != "1" {
		return
	}
	path := os.Getenv(cacheLockPathEnv)
	ready := os.Getenv(cacheLockReadyEnv)
	release := os.Getenv(cacheLockReleaseEnv)
	if path == "" || ready == "" || release == "" {
		t.Fatal("cache lock helper environment is incomplete")
	}
	lock, err := acquireCacheFileLock(path)
	if err != nil {
		t.Fatalf("acquire cache lock: %v", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			t.Errorf("unlock cache lock: %v", err)
		}
	}()
	if err := os.WriteFile(ready, []byte("locked"), 0o600); err != nil {
		t.Fatalf("signal cache lock readiness: %v", err)
	}
	if err := waitForCacheLockFile(release, cacheLockHelperWait); err != nil {
		t.Fatalf("wait for cache lock release: %v", err)
	}
}

func waitForCacheLockFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return os.ErrDeadlineExceeded
		}
		time.Sleep(cacheLockPollInterval)
	}
}

func TestCacheIgnoresMalformedFileAndPreservesAtomicPreRenameState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.NPM, "react", model.Exists); err != nil {
		t.Fatal(err)
	}
	partial, err := os.CreateTemp(filepath.Dir(path), "cache-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := partial.WriteString("{"); err != nil {
		t.Fatal(err)
	}
	if err := partial.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := loaded.Get(model.NPM, "react", 7*24*time.Hour, time.Hour); !ok || got != model.Exists {
		t.Fatalf("pre-rename cache was not preserved: %q, %v", got, ok)
	}

	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = NewCache(path)
	if err != nil {
		t.Fatalf("malformed cache stopped scan setup: %v", err)
	}
	if _, ok := loaded.Get(model.NPM, "react", time.Hour, time.Hour); ok {
		t.Fatal("malformed cache produced a stale result")
	}
	if err := loaded.Put(model.NPM, "react", model.Exists); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]cacheEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("cache was not repaired atomically: %v", err)
	}
}

func TestCacheClearRemovesDataAndStaleTemporaryFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache, err := NewCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(model.NPM, "react", model.Exists); err != nil {
		t.Fatal(err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "cache-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache file remained after clear: %v", err)
	}
	if temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), "cache-*.json")); err != nil || len(temporary) != 0 {
		t.Fatalf("cache temp files remained: %v, %v", temporary, err)
	}
}
