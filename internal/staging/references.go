package staging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
)

var (
	ErrReferenced = errors.New("sealed acquisition still has durable references")
	ErrReleased   = errors.New("sealed acquisition reference has been released")
	ErrRetired    = errors.New("sealed acquisition has been retired")
)

// Lease pins one immutable acquisition for a build or recovery owner. The owner
// is a stable unique task/role ID. Release is terminal for that ID, preventing
// an old executor from releasing a newly acquired reference with the same name.
type Lease struct {
	store  *Store
	owner  string
	sealed *Sealed
}

func (s *Store) notRetired(ctx context.Context) error {
	if _, err := s.files.Read(ctx, s.runID+".retired.json", receiptLimit); err == nil {
		return ErrRetired
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fileError(err)
	}
	return nil
}

func (s *Store) refName(owner string) string     { return s.runID + ".ref-" + owner + ".json" }
func (s *Store) releaseName(owner string) string { return s.runID + ".released-" + owner + ".json" }

func (s *Store) Retain(ctx context.Context, ref SealRef, owner string) (*Lease, error) {
	if !runName.MatchString(owner) {
		return nil, ErrInvalid
	}
	q := s.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	if err := s.notRetired(ctx); err != nil {
		return nil, err
	}
	if _, err := s.files.Read(ctx, s.releaseName(owner), receiptLimit); err == nil {
		return nil, ErrReleased
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fileError(err)
	}
	sealed, err := s.OpenSealed(ctx, ref)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(ref)
	if err := s.ensure(ctx, s.refName(owner), raw, receiptLimit); err != nil {
		return nil, err
	}
	return &Lease{store: s, owner: owner, sealed: sealed}, nil
}

func (l *Lease) Reference() SealRef {
	if l == nil || l.sealed == nil {
		return SealRef{}
	}
	return l.sealed.Reference()
}
func (l *Lease) Pages() []Entry {
	if l == nil || l.sealed == nil {
		return nil
	}
	return l.sealed.Pages()
}
func (l *Lease) active(ctx context.Context) error {
	if err := l.store.notRetired(ctx); err != nil {
		return err
	}
	want, _ := json.Marshal(l.sealed.Reference())
	raw, err := l.store.files.Read(ctx, l.store.refName(l.owner), receiptLimit)
	if err != nil || !bytes.Equal(raw, want) {
		return ErrCorrupt
	}
	if _, err := l.store.files.Read(ctx, l.store.releaseName(l.owner), receiptLimit); err == nil {
		return ErrReleased
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fileError(err)
	}
	return nil
}

// Validate reopens all sealed evidence while the durable owner reference is
// active. It must precede constructing a stream from the acquisition.
func (l *Lease) Validate(ctx context.Context) error {
	if l == nil || l.store == nil || l.sealed == nil {
		return ErrInvalid
	}
	q := l.store.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	if err := l.active(ctx); err != nil {
		return err
	}
	_, err := l.store.OpenSealed(ctx, l.sealed.Reference())
	return err
}

// Bounded creates an equivalent lease view whose verification and future page
// reads enforce the caller's page byte limit at the Files.Read boundary, before
// allocating a corrupt oversized payload. The durable owner identity is shared.
func (l *Lease) Bounded(ctx context.Context, maxPageBytes int64, maxPageRows int) (*Lease, error) {
	if l == nil || l.store == nil || l.sealed == nil || maxPageBytes < 1 || maxPageBytes > 64<<20 || maxPageRows < 1 {
		return nil, ErrInvalid
	}
	q := l.store.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	if err := l.active(ctx); err != nil {
		return nil, err
	}
	for _, entry := range l.sealed.entries {
		if entry.PayloadBytes > maxPageBytes || entry.Summary.Count > maxPageRows {
			return nil, ErrCapacity
		}
	}
	if l.store.maxPageBytes < maxPageBytes {
		maxPageBytes = l.store.maxPageBytes
	}
	limited := &Store{runID: l.store.runID, files: l.store.files, maxPageBytes: maxPageBytes}
	sealed, err := limited.OpenSealed(ctx, l.sealed.Reference())
	if err != nil {
		return nil, err
	}
	return &Lease{store: limited, owner: l.owner, sealed: sealed}, nil
}

func (l *Lease) ReadPage(ctx context.Context, index int) (ingest.Page, error) {
	if l == nil || l.store == nil || l.sealed == nil {
		return ingest.Page{}, ErrInvalid
	}
	q := l.store.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	if err := l.active(ctx); err != nil {
		return ingest.Page{}, err
	}
	return l.sealed.ReadPage(ctx, index)
}

// Release writes a durable tombstone before the reference stops protecting
// files. A lost acknowledgement can be retried without changing its identity.
func (l *Lease) Release(ctx context.Context) error {
	if l == nil || l.store == nil || l.sealed == nil {
		return ErrInvalid
	}
	q := l.store.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	if err := l.store.notRetired(ctx); err != nil {
		return err
	}
	want, _ := json.Marshal(l.sealed.Reference())
	raw, err := l.store.files.Read(ctx, l.store.refName(l.owner), receiptLimit)
	if err != nil || !bytes.Equal(raw, want) {
		return ErrCorrupt
	}
	return l.store.ensure(ctx, l.store.releaseName(l.owner), want, receiptLimit)
}

// Cleanup is a low-level retirement capability, not a GC policy. P14 must first
// prove its task/reader/recovery eligibility. All Stores on a volume must share
// QuotaFiles; process ownership is additionally fenced by the control protocol.
// Retirement and reference checks share one lock. A durable retirement marker
// prevents new retain/writes before any deletion, and supports interrupted retry.
func (s *Store) Cleanup(ctx context.Context, ref SealRef) error {
	q := s.files.(*QuotaFiles)
	q.lifecycle.Lock()
	defer q.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref.RunID != s.runID {
		return ErrInvalid
	}
	marker := s.runID + ".retired.json"
	want, _ := json.Marshal(ref)
	raw, err := s.files.Read(ctx, marker, receiptLimit)
	if err == nil {
		if !bytes.Equal(raw, want) {
			return ErrConflict
		}
		// A previous publication may be visible after a failed directory sync.
		// Confirm retirement durability again before deleting even the first file.
		if err := s.ensure(ctx, marker, want, receiptLimit); err != nil {
			return err
		}
	} else {
		if !errors.Is(err, fs.ErrNotExist) {
			return fileError(err)
		}
		if _, err := s.OpenSealed(ctx, ref); err != nil {
			return err
		}
		names, err := s.files.List(ctx, s.runID+".ref-")
		if err != nil {
			return fileError(err)
		}
		for _, name := range names {
			if !strings.HasSuffix(name, ".json") {
				return ErrCorrupt
			}
			owner := strings.TrimSuffix(strings.TrimPrefix(name, s.runID+".ref-"), ".json")
			if !runName.MatchString(owner) {
				return ErrCorrupt
			}
			pinned, err := s.files.Read(ctx, name, receiptLimit)
			if err != nil || !bytes.Equal(pinned, want) {
				return ErrCorrupt
			}
			released, err := s.files.Read(ctx, s.releaseName(owner), receiptLimit)
			if errors.Is(err, fs.ErrNotExist) {
				return ErrReferenced
			}
			if err != nil || !bytes.Equal(released, want) {
				return ErrCorrupt
			}
		}
		if err := s.ensure(ctx, marker, want, receiptLimit); err != nil {
			return err
		}
	}
	names, err := s.files.List(ctx, s.runID+".")
	if err != nil {
		return fileError(err)
	}
	for _, name := range names {
		if name != marker {
			if err := q.remove(ctx, name); err != nil {
				return err
			}
		}
	}
	return s.files.Sync(ctx)
}
