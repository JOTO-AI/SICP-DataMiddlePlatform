package merge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/staging"
)

var (
	ErrBaseline = errors.New("source baseline evidence cannot be verified")
	ErrScope    = errors.New("full source acquisition scope does not match baseline")
	ErrQuality  = errors.New("full source replacement failed quality gate")
)

// Baseline binds original source objects to the digest retained with the
// trusted previous build evidence. Never recalculate SHA256 from SQL-restored
// rows and call that a verified baseline: SQL NULL has already lost information.
type Baseline struct {
	Table, Scope, SHA256 string
	Rows                 []ingest.Record
}

// FullInput's scope comes from the trusted task/source configuration, not a
// request parameter. It identifies the same school/source range across runs;
// a time window is not a source range. Existing acquisition files bind table,
// mode and pagination, but do not independently attest the upstream identity.
type FullInput struct {
	Scope  string
	Sealed *staging.Sealed
}

// Quality is an explicit per-table policy. Zero MaximumDeletedRows forbids
// deletion. Emptying a previously nonempty source additionally requires an
// explicit AllowEmpty decision; an initially empty source needs no exception.
type Quality struct {
	MinimumRows, MaximumDeletedRows int
	AllowEmpty                      bool
}

// SourceDigest is retained when a trusted source-semantic target is produced.
// It binds table, source scope and all rows, independently of row/key order.
func SourceDigest(ctx context.Context, table, scope string, rows []ingest.Record) (string, error) {
	if _, err := definition(table); err != nil || scope == "" || len(scope) > 256 {
		return "", ErrInvalid
	}
	indexed, err := unique(ctx, table, rows)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	header, _ := json.Marshal(struct{ Table, Scope string }{table, scope})
	hash.Write(header)
	hash.Write([]byte{'\n'})
	for _, row := range ordered(indexed) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		raw, err := row.Canonical()
		if err != nil {
			return "", ErrInvalid
		}
		hash.Write(raw)
		hash.Write([]byte{'\n'})
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Full replaces a bounded table only after verifying actual sealed acquisition
// pages, the previous source digest, the range and the deletion policy. It does
// not publish the candidate. Large tables use the streaming merge entry point.
func Full(ctx context.Context, base Baseline, input FullInput, policy Quality) (Result, error) {
	d, err := definition(base.Table)
	if err != nil || policy.MinimumRows < 0 || policy.MaximumDeletedRows < 0 {
		return Result{}, ErrInvalid
	}
	if input.Scope != base.Scope || input.Scope == "" {
		return Result{}, ErrScope
	}
	digest, err := SourceDigest(ctx, base.Table, base.Scope, base.Rows)
	if err != nil {
		return Result{}, err
	}
	if digest != base.SHA256 {
		return Result{}, ErrBaseline
	}
	if input.Sealed == nil || input.Sealed.Reference().RunID == "" || input.Sealed.Reference().SHA256 == "" {
		return Result{}, ErrInvalid
	}
	entries := input.Sealed.Pages()
	var incoming []ingest.Record
	found := false
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if entry.Request.Mode != ingest.Full && entry.Request.Mode != ingest.ForceFull {
			return Result{}, ErrScope
		}
		if entry.Request.Table != base.Table {
			continue
		}
		found = true
		page, err := input.Sealed.ReadPage(ctx, i)
		if err != nil {
			return Result{}, err
		}
		incoming = append(incoming, page.Records...)
	}
	if !found {
		return Result{}, ErrInvalid
	}
	var rows []ingest.Record
	if d.History {
		// Missing keys are removed by full replacement; keys present in the
		// acquisition still obey the same cross-round latest/conflict rule.
		keys := make(map[string]bool)
		for _, r := range incoming {
			keys[r.Key] = true
		}
		for _, r := range base.Rows {
			if keys[r.Key] {
				incoming = append(incoming, r)
			}
		}
		rows, err = Latest(ctx, base.Table, incoming)
	} else {
		var indexed map[string]ingest.Record
		indexed, err = unique(ctx, base.Table, incoming)
		if err == nil {
			rows = ordered(indexed)
		}
	}
	if err != nil {
		return Result{}, err
	}
	report, err := Compare(ctx, base.Table, base.Rows, rows)
	if err != nil {
		return Result{}, err
	}
	deleted := 0
	for _, change := range report.Changes {
		if change.Kind == Removed {
			deleted++
		}
	}
	if len(rows) < policy.MinimumRows || deleted > policy.MaximumDeletedRows || (len(base.Rows) > 0 && len(rows) == 0 && !policy.AllowEmpty) {
		return Result{}, ErrQuality
	}
	return Result{Rows: rows, Changes: report, DeletionsKnown: true}, nil
}
