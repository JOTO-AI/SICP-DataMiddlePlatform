//go:build linux

package assembly

import "testing"

func TestT15_10WorkerVolumeLockExcludesLiveExecutorAndReleasesForRestart(t *testing.T) {
	dir := t.TempDir()
	closeFirst, err := lockWorker(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFirst()
	if closeSecond, err := lockWorker(dir); err == nil {
		closeSecond()
		t.Fatal("second worker acquired live lock")
	}
	if err = closeFirst(); err != nil {
		t.Fatal(err)
	}
	closeRestart, err := lockWorker(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRestart()
}
