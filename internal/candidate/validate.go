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

const StorageCheckPolicy = "source-storage-v1"

// Legacy evidence remains readable after an upgrade.
const BusinessCheckPolicy = "bounded-smoke-v2"
const MaxBackgroundCases = 9

type QueryEvidence struct {
	Policy       string `json:",omitempty"`
	Suite        string `json:",omitempty"`
	Dependencies map[string]string
	Cases        int
	SHA256       string
}
type Evidence struct {
	Policy      string                  `json:",omitempty"`
	Tables      map[string]TableVersion `json:",omitempty"`
	Target      source.Target
	Combination string
	CheckedAt   time.Time
	Queries     map[contract.Operation]QueryEvidence
}

func (e Evidence) Validate(target source.Target, m Manifest) error {
	if m.Validate() != nil || e.Target != target || e.Combination != m.Combination || e.CheckedAt.IsZero() {
		return ErrBuild
	}
	if e.Policy == StorageCheckPolicy {
		if len(e.Queries) != 0 || len(e.Tables) != len(m.Tables) {
			return ErrBuild
		}
		for _, table := range m.Tables {
			if got, ok := e.Tables[table.Table]; !ok || got != table {
				return ErrBuild
			}
		}
		return nil
	}
	if e.Policy != "" || len(e.Tables) != 0 || len(e.Queries) != len(contract.Definitions()) {
		return ErrBuild
	}
	for _, definition := range contract.Definitions() {
		deps, err := dependencies(m, definition.Operation)
		if err != nil {
			return err
		}
		query, ok := e.Queries[definition.Operation]
		// Legacy published copies remain readable during upgrades. New checks
		// must not reuse their exhaustive evidence as a smoke-suite result.
		if query.Policy != "" && ((query.Policy != BusinessCheckPolicy && query.Policy != "bounded-smoke-v1") || query.Cases > MaxBackgroundCases) {
			return ErrBuild
		}
		if query.Suite != "" && !digest(query.Suite) {
			return ErrBuild
		}
		if !ok || query.Cases < 1 || !digest(query.SHA256) || !maps.Equal(deps, query.Dependencies) {
			return ErrBuild
		}
	}
	return nil
}

// BeginValidation atomically makes prior readiness non-authoritative and
// freezes candidate writes under the execution fence. Ready must verify the
// same candidate, generation, manifest and owner again before committing.
// Storage validation is a worker operation on closed candidates; it never opens
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
type Validator struct {
	Store  ValidationStore
	Checks Checks
	Clock  ports.Clock
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
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "validate_copy", Combination: m.Combination, Candidate: target.CandidateID, Slot: target.Position.Slot, Generation: target.Generation}, string(target.Position.Instance))
	defer func() { end(err) }()
	activity.Report(ctx, "validating_copy", "", string(target.Position.Instance))
	if v.Store == nil || v.Checks == nil || v.Clock == nil || m.Validate() != nil {
		return result, ErrBuild
	}
	h := read.Handle{Position: target.Position, Generation: target.Generation, Combination: m.Combination}
	if h.Validate() != nil || target.CandidateID == "" {
		return result, ErrBuild
	}
	m.Tables = append([]TableVersion(nil), m.Tables...)
	_, err = v.Store.BeginValidation(ctx, target, m)
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
	result = Evidence{Policy: StorageCheckPolicy, Target: target, Combination: m.Combination, CheckedAt: at, Tables: map[string]TableVersion{}}
	for _, table := range m.Tables {
		result.Tables[table.Table] = table
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = v.Store.Ready(ctx, target, m, result); err != nil {
		return
	}
	return
}
