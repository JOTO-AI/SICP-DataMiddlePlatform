//go:build linux

package assembly

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func lockWorker(directory string) (func() error, error) {
	f, e := os.OpenFile(filepath.Join(directory, ".worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, errors.New("worker process lock unavailable")
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		_ = f.Close()
		return nil, errors.New("another worker still owns the persistent volume")
	}
	// Closing this descriptor releases the OS lock even after SIGKILL. Never
	// unlink the inode: that could let two live processes lock different files.
	return f.Close, nil
}
