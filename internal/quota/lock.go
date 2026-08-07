package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockPath is the per-profile lock guarding one cache file's read-modify-write.
//
// Deliberately a separate file from the cache itself: saveEntry publishes by
// rename, which swaps the inode out from under anyone holding a lock on the old
// one. A lock file nobody replaces is the only stable thing to hold.
func lockPath(profileName string) string {
	return filepath.Join(cacheDir(), "quota-"+profileName+".lock")
}

// lockProfile takes the exclusive lock for one profile's cache and returns its
// release.
//
// flock rather than a mutex because the writers are separate processes: the
// desktop app's refresh loop and a terminal running `acs quota` write the same
// file. A mutex would only order goroutines inside one of them.
//
// The lock spans the fetch, not just the write, so load-decide-save is atomic as a
// whole. That is bounded rather than open-ended -- requestTimeout caps the HTTP
// call -- so a hanging endpoint delays the next reader by seconds, not forever.
//
// A lock failure is returned rather than swallowed, but it is not fatal to a read:
// it means the cache directory is unusable, and saveEntry is about to fail on the
// same directory. There is no cache left to corrupt, so callers may proceed and
// still hand back live numbers.
func lockProfile(profileName string) (func(), error) {
	noop := func() {}
	if err := os.MkdirAll(cacheDir(), 0o700); err != nil {
		return noop, fmt.Errorf("create %s: %w", cacheDir(), err)
	}
	path := lockPath(profileName)
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path built from Home()
	if err != nil {
		return noop, fmt.Errorf("open %s: %w", path, err)
	}
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX); err != nil {
		_ = fh.Close()
		return noop, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(fh.Fd()), syscall.LOCK_UN)
		_ = fh.Close()
	}, nil
}
