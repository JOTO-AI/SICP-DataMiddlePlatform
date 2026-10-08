// Package candidate builds complete unpublished copies from trusted targets.
package candidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

var ErrBuild = errors.New("candidate build or complete target evidence invalid")

type TableVersion struct {
	Table, Scope, SHA256 string
	Rows                 int64
}
type Manifest struct {
	Combination string
	Input       staging.SealRef
	Tables      []TableVersion
}

func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Combination) == "" || len(m.Combination) > 256 || m.Input.RunID == "" || !digest(m.Input.SHA256) || len(m.Tables) != len(source.Catalog()) {
		return ErrBuild
	}
	known := map[string]bool{}
	for _, table := range source.Catalog() {
		known[table.Name] = true
	}
	for _, table := range m.Tables {
		if !known[table.Table] || strings.TrimSpace(table.Scope) == "" || len(table.Scope) > 256 || table.Rows < 0 || !digest(table.SHA256) {
			return ErrBuild
		}
		delete(known, table.Table)
	}
	return nil
}
func digest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && strings.ToLower(s) == s
}

// CompleteSource pins a persisted complete target, produced by merge against a
// verified full baseline (or initial full acquisition). A window/delta alone
// cannot implement this contract. Validate rechecks its immutable provenance;
// Open returns bounded, strictly key-sorted ORIGINAL source objects, not SQL
// restored rows, whose missing/null distinctions have already been lost.
type CompleteSource interface {
	Validate(context.Context, Manifest) error
	Open(context.Context, Manifest, TableVersion) (Rows, error)
}
type Rows interface {
	Next(context.Context) (ingest.Record, error)
	Close(context.Context) error
}

type TableEvidence struct {
	Version  TableVersion
	Complete bool
}
type Baseline struct {
	State  source.BuildState
	Tables map[string]TableEvidence
}

// Ledger methods are fenced persistent transitions. Inspect binds evidence to
// this exact slot/generation/candidate. Invalidate MUST commit before Rebuild;
// any partial write can then never be mistaken for the previous full baseline.
// Record/Complete must recheck ownership and slot generation, and Complete only
// means source storage complete. P12.02 separately establishes query readiness.
type Ledger interface {
	Inspect(context.Context, source.Target) (Baseline, error)
	Invalidate(context.Context, source.Target, string) error
	Record(context.Context, source.Target, TableEvidence) error
	Complete(context.Context, source.Target, Manifest) error
	Fail(context.Context, source.Target) error
}
type Writer interface {
	Rebuild(context.Context, source.Target, string, source.Iterator, int) (source.Report, error)
}

var _ Writer = source.Repository{}

type Builder struct {
	Ledger    Ledger
	Source    CompleteSource
	Writer    Writer
	BatchSize int
}
type Report struct {
	Reused, Rebuilt []string
	StorageComplete bool
	WrittenRows     int64
}

func validTarget(target source.Target, s source.BuildState) bool {
	return target.Position.Validate() == nil && target.Generation > 0 && target.CandidateID != "" && s.Generation == target.Generation && s.CandidateID == target.CandidateID && s.OwnerValid && !s.AdmissionOpen && s.Readers == 0 && ((s.Initializing && s.ActiveInstance == "") || (!s.Initializing && (s.ActiveInstance == "M" || s.ActiveInstance == "S") && s.ActiveInstance != target.Position.Instance))
}

func (b Builder) Build(ctx context.Context, target source.Target, m Manifest, force bool) (out Report, err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "write_copy", Combination: m.Combination, Candidate: target.CandidateID, Slot: target.Position.Slot, Generation: target.Generation}, string(target.Position.Instance))
	defer func() { end(err) }()
	if b.Ledger == nil || b.Source == nil || b.Writer == nil || b.BatchSize < 1 || b.BatchSize > source.MaxBatchRows || m.Validate() != nil {
		return out, ErrBuild
	}
	// Detach the caller's manifest; each copy/phase sees identical table versions.
	m.Tables = append([]TableVersion(nil), m.Tables...)
	activity.Report(ctx, "validating_target", "", string(target.Position.Instance))
	if err = b.Source.Validate(ctx, m); err != nil {
		return out, err
	}
	baseline, err := b.Ledger.Inspect(ctx, target)
	if err != nil {
		return out, err
	}
	if !validTarget(target, baseline.State) {
		return out, source.ErrTarget
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, b.Ledger.Fail(cleanup, target))
			out.StorageComplete = false
		}
	}()
	for _, table := range m.Tables {
		activity.Report(ctx, "writing_table", table.Table, string(target.Position.Instance))
		if err = ctx.Err(); err != nil {
			return
		}
		previous, ok := baseline.Tables[table.Table]
		if !force && ok && previous.Complete && previous.Version == table {
			// Persist adoption under the candidate identity; this is not a ready mark.
			if err = b.Ledger.Record(ctx, target, previous); err != nil {
				return
			}
			out.Reused = append(out.Reused, table.Table)
			continue
		}
		if err = b.Ledger.Invalidate(ctx, target, table.Table); err != nil {
			return
		}
		var rows Rows
		rows, err = b.Source.Open(ctx, m, table)
		if err != nil {
			return
		}
		if rows == nil {
			err = ErrBuild
			return
		}
		iterator := newChecked(table, rows)
		var report source.Report
		writeCtx, writeEnd := events.TraceBuild(ctx, events.BuildScope{Stage: "write_table", Table: table.Table, Combination: m.Combination, Candidate: target.CandidateID, Slot: target.Position.Slot, Generation: target.Generation}, string(target.Position.Instance))
		report, err = b.Writer.Rebuild(writeCtx, target, table.Table, iterator, b.BatchSize)
		out.WrittenRows += int64(report.WrittenRows)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = errors.Join(err, rows.Close(cleanup))
		cancel()
		if err == nil && (!report.Complete || !iterator.verified || int64(report.WrittenRows) != table.Rows) {
			err = ErrBuild
		}
		if err == nil {
			err = b.Ledger.Record(writeCtx, target, TableEvidence{Version: table, Complete: true})
		}
		writeEnd(err)
		if err != nil {
			return
		}
		out.Rebuilt = append(out.Rebuilt, table.Table)
	}
	activity.Report(ctx, "validating_target", "", string(target.Position.Instance))
	if err = b.Source.Validate(ctx, m); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = b.Ledger.Complete(ctx, target, m); err != nil {
		return
	}
	out.StorageComplete = true
	return
}

type checked struct {
	version  TableVersion
	rows     Rows
	hash     hash.Hash
	count    int64
	previous string
	verified bool
}

func newChecked(v TableVersion, rows Rows) *checked {
	h := sha256.New()
	header, _ := json.Marshal(struct{ Table, Scope string }{v.Table, v.Scope})
	h.Write(header)
	h.Write([]byte{'\n'})
	return &checked{version: v, rows: rows, hash: h}
}
func (c *checked) Next(ctx context.Context) (source.SourceRow, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if c.verified {
		return nil, false, nil
	}
	record, err := c.rows.Next(ctx)
	if err == io.EOF {
		if c.count != c.version.Rows || hex.EncodeToString(c.hash.Sum(nil)) != c.version.SHA256 {
			return nil, false, ErrBuild
		}
		c.verified = true
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := record.Canonical()
	if err != nil {
		return nil, false, ErrBuild
	}
	validated, err := ingest.DecodeRecord(c.version.Table, raw)
	buildmetric.Add(ctx, buildmetric.Decodes, 1)
	if err != nil || validated.Key != record.Key || record.Key <= c.previous || c.count >= c.version.Rows {
		return nil, false, ErrBuild
	}
	c.previous = record.Key
	c.count++
	c.hash.Write(raw)
	c.hash.Write([]byte{'\n'})
	row := source.SourceRow{}
	for field, token := range validated.Fields {
		row[field] = append(json.RawMessage(nil), token.Raw...)
	}
	return row, true, nil
}
