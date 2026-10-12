//go:build linux

package observability

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockJournal(root *os.Root) (*os.File, error) {
	f, err := root.OpenFile(".writer.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
