package candidate

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

type QueryEvidence struct {
	Dependencies map[string]string
	Cases        int
	SHA256       string
}
type Evidence struct {
	Target      source.Target
	Combination string
	CheckedAt   time.Time
	Queries     map[contract.Operation]QueryEvidence
}

func (e Evidence) Validate(target source.Target, m Manifest) error {
	if m.Validate() != nil || e.Target != target || e.Combination != m.Combination || e.CheckedAt.IsZero() || len(e.Queries) != len(contract.Definitions()) {
		return ErrBuild
	}
	for _, definition := range contract.Definitions() {
		deps, err := dependencies(m, definition.Operation)
		if err != nil {
			return err
		}
		query, ok := e.Queries[definition.Operation]
		if !ok || query.Cases < 1 || !digest(query.SHA256) || !maps.Equal(deps, query.Dependencies) {
			return ErrBuild
		}
	}
	return nil
}

// BeginValidation atomically makes prior readiness non-authoritative and
// freezes candidate writes under the execution fence. Ready must verify the
// same candidate, generation, manifest and owner again before committing.
// Query validation is a worker operation on closed candidates; it never opens
// public read admission or uses the normal query Coordinator to bypass it.
type ValidationStore interface {
	BeginValidation(context.Context, source.Target, Manifest) (Evidence, error)
	Ready(context.Context, source.Target, Manifest, Evidence) error
	ValidationFailed(context.Context, source.Target) error
}
type Checks interface {
	SourceStorage(context.Context, read.Handle, Manifest) error
	SourceIndexes(context.Context, read.Handle, Manifest) error
}
type Queries interface {
	Check(context.Context, read.Handle, contract.Operation, time.Time) (int, string, error)
}
type Validator struct {
	Store   ValidationStore
	Checks  Checks
	Queries Queries
	Clock   ports.Clock
}

func dependencies(m Manifest, op contract.Operation) (map[string]string, error) {
	tables, err := metadata.Dependencies(op)
	if err != nil {
		return nil, err
	}
	all := map[string]string{}
	for _, table := range m.Tables {
		all[table.Table] = table.Scope + ":" + table.SHA256
	}
	out := map[string]string{}
	for _, table := range tables {
		v, ok := all[table]
		if !ok {
			return nil, ErrBuild
		}
		out[table] = v
	}
	return out, nil
}

func (v Validator) Validate(ctx context.Context, target source.Target, m Manifest) (result Evidence, err error) {
	end := events.BeginBuild(ctx, events.BuildScope{Stage: "validate_copy", Combination: m.Combination, Candidate: target.CandidateID, Slot: target.Position.Slot, Generation: target.Generation}, string(target.Position.Instance))
	defer func() { end(err) }()
	activity.Report(ctx, "validating_copy", "", string(target.Position.Instance))
	if v.Store == nil || v.Checks == nil || v.Queries == nil || v.Clock == nil || m.Validate() != nil {
		return result, ErrBuild
	}
	h := read.Handle{Position: target.Position, Generation: target.Generation, Combination: m.Combination}
	if h.Validate() != nil || target.CandidateID == "" {
		return result, ErrBuild
	}
	m.Tables = append([]TableVersion(nil), m.Tables...)
	previous, err := v.Store.BeginValidation(ctx, target, m)
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, v.Store.ValidationFailed(cleanup, target))
			result = Evidence{}
		}
	}()
	if err = v.Checks.SourceStorage(ctx, h, m); err != nil {
		return
	}
	if err = v.Checks.SourceIndexes(ctx, h, m); err != nil {
		return
	}
	at := v.Clock.Now()
	if at.IsZero() {
		err = ErrBuild
		return
	}
	result = Evidence{Target: target, Combination: m.Combination, CheckedAt: at, Queries: map[contract.Operation]QueryEvidence{}}
	for _, definition := range contract.Definitions() {
		if err = ctx.Err(); err != nil {
			return
		}
		op := definition.Operation
		var deps map[string]string
		deps, err = dependencies(m, op)
		if err != nil {
			return
		}
		old, ok := previous.Queries[op]
		if ok && previous.Target == target && previous.Combination == m.Combination && !previous.CheckedAt.IsZero() && old.Cases > 0 && digest(old.SHA256) && maps.Equal(old.Dependencies, deps) {
			old.Dependencies = maps.Clone(old.Dependencies)
			result.Queries[op] = old
			continue
		}
		var count int
		var summary string
		count, summary, err = v.Queries.Check(ctx, h, op, at)
		if err != nil {
			return
		}
		if count < 1 || !digest(summary) {
			err = ErrBuild
			return
		}
		result.Queries[op] = QueryEvidence{Dependencies: deps, Cases: count, SHA256: summary}
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = v.Store.Ready(ctx, target, m, result); err != nil {
		return
	}
	return
}
