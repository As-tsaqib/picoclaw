//go:build !android && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris

package fileutil

// LockFile provides the same API on platforms without the Unix advisory-lock
// primitive. Callers still retain their in-process mutex; unsupported targets
// therefore keep the previous behavior without introducing a platform-specific
// dependency or changing their build surface.
func LockFile(_ string) (func() error, error) {
	return func() error { return nil }, nil
}
