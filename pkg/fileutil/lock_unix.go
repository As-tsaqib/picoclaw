//go:build android || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// LockFile obtains an owner-only advisory lock shared by processes that use
// the same lock path. The returned function releases the lock and is safe to
// call more than once. The lock file is intentionally retained so a crashed
// process cannot leave a stale pathname that blocks the next writer; the
// kernel releases the advisory lock when the descriptor closes.
func LockFile(path string) (func() error, error) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return nil, fmt.Errorf("lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("secure lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	var once sync.Once
	var releaseErr error
	release := func() error {
		once.Do(func() {
			if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
				releaseErr = err
			}
			if err := f.Close(); releaseErr == nil {
				releaseErr = err
			}
		})
		return releaseErr
	}
	return release, nil
}
