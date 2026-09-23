package observability

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
)

// Directory uses a private, service-specific persistent directory. One process
// owns it. Linux storage must support hard links and directory fsync.
type Directory struct {
	gate          chan struct{}
	root          *os.Root
	random        ports.Random
	maxEventBytes int
}

func OpenDirectory(path string, random ports.Random, maxEventBytes int) (*Directory, error) {
	if random == nil || maxEventBytes < 512 || maxEventBytes > 1<<20 {
		return nil, ErrBuffer
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrBuffer
	}
	return &Directory{gate: make(chan struct{}, 1), root: root, random: random, maxEventBytes: maxEventBytes}, nil
}
func (d *Directory) Close() error    { return d.root.Close() }
func (d Directory) String() string   { return "[log directory redacted]" }
func (d Directory) GoString() string { return d.String() }
func (d *Directory) lock(ctx context.Context) error {
	select {
	case d.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (d *Directory) unlock() { <-d.gate }
func (d *Directory) sync(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	dir, err := d.root.Open(".")
	if err != nil {
		return ErrBuffer
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			return ErrCapacity
		}
		return ErrBuffer
	}
	return ctx.Err()
}
func (d *Directory) Flush(ctx context.Context) error {
	if err := d.lock(ctx); err != nil {
		return err
	}
	defer d.unlock()
	return d.sync(ctx)
}
func (d *Directory) read(ctx context.Context, id string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !digestID(id, false) {
		return nil, ErrPayload
	}
	f, err := d.root.Open(id + ".json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(d.maxEventBytes) {
		return nil, ErrPayload
	}
	body, err := io.ReadAll(io.LimitReader(f, int64(d.maxEventBytes)+1))
	if err != nil || len(body) > d.maxEventBytes {
		return nil, ErrBuffer
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return body, nil
}
func (d *Directory) Put(ctx context.Context, id string, body []byte) error {
	if err := d.lock(ctx); err != nil {
		return err
	}
	defer d.unlock()
	if !digestID(id, false) || len(body) < 1 || len(body) > d.maxEventBytes {
		return ErrPayload
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	old, err := d.read(ctx, id)
	if err == nil {
		if !bytes.Equal(old, body) {
			return ErrConflict
		}
		return d.sync(ctx)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return ErrBuffer
	}
	var nonce [16]byte
	if n, err := d.random.Read(nonce[:]); err != nil || n != len(nonce) {
		return ErrBuffer
	}
	name := "pending-" + hex.EncodeToString(nonce[:]) + ".tmp"
	f, err := d.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			return ErrCapacity
		}
		return ErrBuffer
	}
	n, writeErr := f.Write(body)
	if writeErr == nil && n != len(body) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		if errors.Is(writeErr, syscall.ENOSPC) || errors.Is(closeErr, syscall.ENOSPC) {
			return ErrCapacity
		}
		return ErrBuffer
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := d.root.Link(name, id+".json"); err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			return ErrCapacity
		}
		return ErrBuffer
	}
	if err := d.sync(ctx); err != nil {
		return err
	}
	_ = d.root.Remove(name)
	return nil
}
func (d *Directory) Lookup(ctx context.Context, id string) ([]byte, error) {
	if err := d.lock(ctx); err != nil {
		return nil, err
	}
	defer d.unlock()
	body, err := d.read(ctx, id)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fs.ErrNotExist
	}
	if err != nil {
		return nil, ErrBuffer
	}
	return body, nil
}
func (d *Directory) Usage(ctx context.Context) (Usage, error) {
	if err := d.lock(ctx); err != nil {
		return Usage{}, err
	}
	defer d.unlock()
	dir, err := d.root.Open(".")
	if err != nil {
		return Usage{}, ErrBuffer
	}
	defer dir.Close()
	var total Usage
	for {
		if ctx.Err() != nil {
			return Usage{}, ctx.Err()
		}
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > math.MaxInt64-total.Bytes {
				return Usage{}, ErrBuffer
			}
			total.Bytes += info.Size()
			total.Files++
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return Usage{}, ErrBuffer
		}
	}
}
func (d *Directory) Batch(ctx context.Context, maxCount, maxBytes int) ([]Stored, error) {
	if err := d.lock(ctx); err != nil {
		return nil, err
	}
	defer d.unlock()
	if maxCount < 1 || maxCount > 1000 || maxBytes < d.maxEventBytes || maxBytes > 64<<20 {
		return nil, ErrLimit
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	dir, err := d.root.Open(".")
	if err != nil {
		return nil, ErrBuffer
	}
	defer dir.Close()
	names := []string{}
	for len(names) < maxCount {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				id := strings.TrimSuffix(entry.Name(), ".json")
				if !digestID(id, false) {
					return nil, ErrPayload
				}
				names = append(names, id)
				if len(names) == maxCount {
					break
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrBuffer
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	sort.Strings(names)
	result := []Stored{}
	size := 0
	for _, id := range names {
		body, err := d.read(ctx, id)
		if err != nil {
			return nil, ErrBuffer
		}
		if len(body) > maxBytes-size {
			break
		}
		result = append(result, Stored{id, body})
		size += len(body)
		if len(result) == maxCount {
			break
		}
	}
	return result, nil
}
func (d *Directory) Remove(ctx context.Context, id string) error {
	if err := d.lock(ctx); err != nil {
		return err
	}
	defer d.unlock()
	if !digestID(id, false) {
		return ErrPayload
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := d.root.Remove(id + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrBuffer
	}
	return d.sync(ctx)
}
