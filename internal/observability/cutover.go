package observability

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

var ErrCutover = errors.New("legacy log cutover incomplete; original files retained")

// ModeJournal is exclusively owned before any producers start. The durable mode
// record binds the target topology and correlation key, not credentials.
type ModeJournal interface {
	Journal
	Mode(context.Context) (string, error)
	SetMode(context.Context, string) error
	LegacyTarget(context.Context) (string, error)
	SetLegacyTarget(context.Context, string) error
}

func (d *Directory) Mode(ctx context.Context) (string, error) {
	return d.readMarker(ctx, ".storage-mode")
}
func (d *Directory) LegacyTarget(ctx context.Context) (string, error) {
	return d.readMarker(ctx, ".legacy-target")
}
func (d *Directory) readMarker(ctx context.Context, name string) (string, error) {
	if err := d.lock(ctx); err != nil {
		return "", err
	}
	defer d.unlock()
	f, err := d.root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", ErrCutover
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil || !digestID(string(b), false) {
		return "", ErrCutover
	}
	return string(b), ctx.Err()
}
func (d *Directory) SetMode(ctx context.Context, mode string) error {
	return d.writeMarker(ctx, ".storage-mode", mode)
}
func (d *Directory) SetLegacyTarget(ctx context.Context, target string) error {
	return d.writeMarker(ctx, ".legacy-target", target)
}
func (d *Directory) writeMarker(ctx context.Context, name, mode string) error {
	if !digestID(mode, false) {
		return ErrCutover
	}
	if err := d.lock(ctx); err != nil {
		return err
	}
	defer d.unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f, err := d.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrCutover
	}
	n, writeErr := f.Write([]byte(mode))
	syncErr := f.Sync()
	closeErr := f.Close()
	if n != len(mode) || writeErr != nil || syncErr != nil || closeErr != nil {
		return ErrCutover
	}
	return d.sync(ctx)
}

// RegisterLegacy pins the original destination before this version admits any
// producer. Existing installations first deploy in single_legacy, then stop all
// writers before switching; unmarked nonempty journals cannot be guessed.
func RegisterLegacy(ctx context.Context, j ModeJournal, s *Sanitizer, r storage.Registry) error {
	if j == nil || s == nil || r.LogMode() != storage.SingleLegacy {
		return ErrCutover
	}
	mode, err := j.Mode(ctx)
	if err != nil || mode != "" {
		return ErrCutover
	}
	expected := s.Fingerprint("legacy-log-target-v1", r.LogTarget(storage.PlatformLog))
	old, err := j.LegacyTarget(ctx)
	if err != nil {
		return ErrCutover
	}
	if old == expected {
		return nil
	}
	if old != "" {
		return ErrCutover
	}
	return j.SetLegacyTarget(ctx, expected)
}

// PrepareDual runs only during startup, under journal ownership and before any
// recorder is installed. The closed legacy dispatcher uses the original
// platform target even for call/attempt. It must acknowledge every file and
// finish its transaction before a durable cutover record permits new routing.
func PrepareDual(ctx context.Context, j ModeJournal, s *Sanitizer, capacity CapacityConfig, r Repository) error {
	if j == nil || s == nil || r.Registry.LogMode() != storage.DualLogs {
		return ErrCutover
	}
	mode := s.Fingerprint("dual-log-v1", r.Registry.LogTopology())
	old, err := j.Mode(ctx)
	if err != nil {
		return ErrCutover
	}
	if old != "" {
		if old != mode {
			return ErrCutover
		}
		return nil
	}
	usage, err := j.Usage(ctx)
	if err != nil {
		return ErrCutover
	}
	legacy, err := j.LegacyTarget(ctx)
	if err != nil {
		return ErrCutover
	}
	expected := s.Fingerprint("legacy-log-target-v1", r.Registry.LogTarget(storage.PlatformLog))
	if legacy != "" && legacy != expected || usage.Files > 0 && legacy != expected {
		return ErrCutover
	}
	q, err := NewQuota(j, capacity)
	if err != nil {
		return ErrCutover
	}
	b, err := NewBuffer(q, s, 100, 1<<20)
	if err != nil {
		return ErrCutover
	}
	r.Source = storage.PlatformLog
	r.legacyReplay = true
	d, err := NewDispatcher(b, r, SystemTimer{}, DispatchConfig{MaxWait: time.Second, RetryWait: time.Second, Timeout: 5 * time.Second, BatchBytes: 1 << 20})
	if err != nil {
		return ErrCutover
	}
	if d.Close(ctx) != nil {
		return ErrCutover
	}
	u, err := j.Usage(ctx)
	if err != nil || u.Files != 0 || u.Bytes != 0 {
		return ErrCutover
	}
	if j.SetMode(ctx, mode) != nil {
		return ErrCutover
	}
	return nil
}
