//go:build !linux

package observability

import "os"

func lockJournal(*os.Root) (*os.File, error) { return nil, ErrBuffer }
