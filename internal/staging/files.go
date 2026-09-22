// Package staging retains immutable source pages and confirmed acquisition evidence.
package staging

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"strings"
)

// Files separates temporary writes, durable immutable publication and reads.
// Publish must never overwrite an existing destination. Sync confirms directory
// durability when recovering an earlier publication whose acknowledgement failed.
type Files interface {
	Read(context.Context, string, int64) ([]byte, error)
	WriteTemporary(context.Context, []byte) (string, error)
	Publish(context.Context, string, string) error
	Sync(context.Context) error
	List(context.Context, string) ([]string, error)
	Usage(context.Context) (int64, error)
}

func (d *Directory) Usage(ctx context.Context) (int64, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	dir, err := d.root.Open(".")
	if err != nil {
		return 0, ErrStorage
	}
	defer dir.Close()
	var size int64
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			info, statErr := entry.Info()
			if statErr != nil || !info.Mode().IsRegular() {
				return 0, ErrStorage
			}
			if info.Size() < 0 || info.Size() > math.MaxInt64-size {
				return 0, ErrCapacity
			}
			size += info.Size()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, ErrStorage
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
	}
	return size, ctx.Err()
}

// Directory uses a pre-provisioned, private persistent directory. os.Root
// confines all accesses; only flat generated filenames are accepted here.
// Production requires a local filesystem with hard links and directory fsync.
type Directory struct{ root *os.Root }

func OpenDirectory(path string) (*Directory, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrStorage
	}
	return &Directory{root: root}, nil
}
func (d *Directory) Close() error { return d.root.Close() }

// Remove is the physical primitive used only after Store.Cleanup has persisted
// retirement under the shared lifecycle lock. Missing files are retry-safe.
func (d *Directory) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !flatName(name) {
		return ErrInvalid
	}
	if err := d.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrStorage
	}
	return d.Sync(ctx)
}
func flatName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\:")
}
func (d *Directory) Read(ctx context.Context, name string, limit int64) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !flatName(name) || limit < 1 || limit > 128<<20 {
		return nil, ErrInvalid
	}
	f, err := d.root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fs.ErrNotExist
		}
		return nil, ErrStorage
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrStorage
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, ErrStorage
	}
	if int64(len(data)) > limit {
		return nil, ErrCapacity
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return data, nil
}
func (d *Directory) WriteTemporary(ctx context.Context, data []byte) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", ErrStorage
	}
	name := "pending-" + hex.EncodeToString(random[:]) + ".tmp"
	f, err := d.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", ErrStorage
	}
	_, writeErr := io.Copy(f, &contextReader{ctx: ctx, reader: bytes.NewReader(data)})
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return name, ErrStorage
	}
	if ctx.Err() != nil {
		return name, ctx.Err()
	}
	return name, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	if len(p) > 64<<10 {
		p = p[:64<<10]
	}
	return r.reader.Read(p)
}
func (d *Directory) Publish(ctx context.Context, temp, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !flatName(temp) || !flatName(name) || !strings.HasPrefix(temp, "pending-") || !strings.HasSuffix(temp, ".tmp") {
		return ErrInvalid
	}
	if err := d.root.Link(temp, name); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fs.ErrExist
		}
		return ErrStorage
	}
	if err := d.Sync(ctx); err != nil {
		return err
	}
	// Failure to remove an orphan is not allowed to invalidate the durable link.
	_ = d.root.Remove(temp)
	return nil
}
func (d *Directory) Sync(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	dir, err := d.root.Open(".")
	if err != nil {
		return ErrStorage
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return ErrStorage
	}
	return ctx.Err()
}
func (d *Directory) List(ctx context.Context, prefix string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !flatName(prefix) {
		return nil, ErrInvalid
	}
	dir, err := d.root.Open(".")
	if err != nil {
		return nil, ErrStorage
	}
	defer dir.Close()
	var result []string
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), prefix) {
				result = append(result, entry.Name())
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrStorage
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return result, nil
}
