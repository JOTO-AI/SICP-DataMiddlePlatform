//go:build !linux

package assembly

import "errors"

func lockWorker(string) (func() error, error) {
	return nil, errors.New("worker requires Linux process locking")
}
