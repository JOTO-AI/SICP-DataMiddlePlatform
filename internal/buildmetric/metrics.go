// Package buildmetric holds bounded, process-local build counters. It never
// writes storage or creates goroutines and contains no source values or SQL.
package buildmetric

import (
	"context"
	"math"
	"sync"
	"time"
)

type Counter uint8

const (
	RowsRead Counter = iota
	RowsWritten
	BytesRead
	BytesWritten
	Batches
	FileValidations
	Decodes
	SQL
	Cases
	LockWaitNS
	SpoolWritten
	counterCount
)

type Summary struct {
	RowsRead        int64 `json:"rows_read"`
	RowsWritten     int64 `json:"rows_written"`
	BytesRead       int64 `json:"bytes_read"`
	BytesWritten    int64 `json:"bytes_written"`
	Batches         int64 `json:"batches"`
	FileValidations int64 `json:"file_validations"`
	Decodes         int64 `json:"decodes"`
	SQL             int64 `json:"sql"`
	Cases           int64 `json:"cases"`
	LockWaitNS      int64 `json:"lock_wait_ns"`
	SpoolWritten    int64 `json:"spool_bytes_written"`
	BufferPeak      int64 `json:"buffer_peak_bytes"`
	DiskPeak        int64 `json:"temporary_disk_peak_bytes"`
	Invalid         bool  `json:"invalid,omitempty"`
}

type key struct{}

func Enabled(ctx context.Context) bool { _, ok := ctx.Value(key{}).(*Scope); return ok }

// LockAcquisition measures lock acquisition RPC wall time (including transport),
// never lock hold time. It is a no-op outside an instrumented build scope.
func LockAcquisition(ctx context.Context) func() {
	if _, ok := ctx.Value(key{}).(*Scope); !ok {
		return func() {}
	}
	started := time.Now()
	return func() { Add(ctx, LockWaitNS, time.Since(started).Nanoseconds()) }
}

type Scope struct {
	mu                                 sync.Mutex
	parent                             *Scope
	values                             [counterCount]int64
	buffer, disk, bufferPeak, diskPeak int64
	invalid                            bool
}

func Begin(ctx context.Context) (context.Context, *Scope) {
	parent, _ := ctx.Value(key{}).(*Scope)
	s := &Scope{parent: parent}
	return context.WithValue(ctx, key{}, s), s
}
func Add(ctx context.Context, counter Counter, value int64) {
	s, _ := ctx.Value(key{}).(*Scope)
	for ; s != nil; s = s.parent {
		s.mu.Lock()
		if counter >= counterCount || value < 0 || value > math.MaxInt64-s.values[counter] {
			s.invalid = true
		} else {
			s.values[counter] += value
		}
		s.mu.Unlock()
	}
}

// Memory and Disk track live canonical/bound payload bytes, not allocator RSS.
// A parent accounts for simultaneous children, so peaks are never summed.
func Memory(ctx context.Context, delta int64) { gauge(ctx, delta, false) }
func Disk(ctx context.Context, delta int64)   { gauge(ctx, delta, true) }
func gauge(ctx context.Context, delta int64, disk bool) {
	s, _ := ctx.Value(key{}).(*Scope)
	for ; s != nil; s = s.parent {
		s.mu.Lock()
		current, peak := &s.buffer, &s.bufferPeak
		if disk {
			current, peak = &s.disk, &s.diskPeak
		}
		if delta == math.MinInt64 || delta < 0 && *current < -delta || delta > 0 && *current > math.MaxInt64-delta {
			s.invalid = true
		} else {
			*current += delta
			if *current > *peak {
				*peak = *current
			}
		}
		s.mu.Unlock()
	}
}
func (s *Scope) Snapshot() Summary {
	if s == nil {
		return Summary{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.values
	return Summary{RowsRead: v[RowsRead], RowsWritten: v[RowsWritten], BytesRead: v[BytesRead], BytesWritten: v[BytesWritten], Batches: v[Batches], FileValidations: v[FileValidations], Decodes: v[Decodes], SQL: v[SQL], Cases: v[Cases], LockWaitNS: v[LockWaitNS], SpoolWritten: v[SpoolWritten], BufferPeak: s.bufferPeak, DiskPeak: s.diskPeak, Invalid: s.invalid}
}
func (s Summary) Valid() bool {
	return s.RowsRead >= 0 && s.RowsWritten >= 0 && s.BytesRead >= 0 && s.BytesWritten >= 0 && s.Batches >= 0 && s.FileValidations >= 0 && s.Decodes >= 0 && s.SQL >= 0 && s.Cases >= 0 && s.LockWaitNS >= 0 && s.SpoolWritten >= 0 && s.BufferPeak >= 0 && s.DiskPeak >= 0
}
