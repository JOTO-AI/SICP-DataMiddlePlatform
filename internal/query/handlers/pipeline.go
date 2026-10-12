package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	querymeta "github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/metadata"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

type Pipeline struct {
	Grades    grades.Service
	Timetable timetable.Service
	Admission read.FixedAdmission
	Snapshots snapshot.Manager
	Cache     cache.Executor
	Metadata  Metadata
	Times     compute.Times
}

// Handlers registers all four real operations. Callers retain the coalescer and
// close it during shutdown before closing database pools.
func (p Pipeline) Handlers(key string, recorder *events.Recorder, observe transport.Observation) (map[contract.Operation]http.Handler, error) {
	if p.Grades.Source == nil || p.Grades.Clock == nil || p.Timetable.Source == nil || p.Timetable.Clock == nil || p.Admission == nil || p.Metadata == nil || p.Snapshots.Routes == nil || p.Snapshots.Store == nil || p.Snapshots.Clock == nil || p.Snapshots.Random == nil || p.Cache.Results.Store == nil || p.Cache.Results.Observe == nil || p.Cache.Flights == nil || p.Cache.Validate == nil {
		return nil, errors.New("complete query pipeline dependencies required")
	}
	out := map[contract.Operation]http.Handler{}
	for _, definition := range contract.Definitions() {
		op := definition.Operation
		h, err := transport.BusinessWithSnapshot(op, key, recorder, observe, func(ctx context.Context, input any) (any, error) { return p.run(ctx, op, input) })
		if err != nil {
			return nil, err
		}
		out[op] = h
	}
	return out, nil
}

func (p Pipeline) run(ctx context.Context, op contract.Operation, input any) (any, error) {
	var h read.Handle
	var token string
	var err error
	if contract.SupportsSnapshot(op) {
		selection, selectErr := p.Snapshots.Select(ctx, op, input, transport.SnapshotToken(ctx))
		if selectErr != nil {
			return nil, selectErr
		}
		h, token = selection.Handle, selection.Token
	} else {
		h, err = p.Snapshots.Routes.Active(ctx)
		if err != nil {
			return nil, err
		}
	}
	result, err := p.Cache.Query(ctx, h, op, input, func(workCtx context.Context, fixed read.Handle) ([]byte, error) {
		value, loadErr := read.ExecuteFixed(workCtx, p.Admission, fixed, func(sqlCtx context.Context, actual read.Handle) (any, error) {
			synced, metaErr := p.Metadata.SyncedAt(sqlCtx, actual, op)
			if metaErr != nil {
				return nil, metaErr
			}
			switch req := input.(type) {
			case contract.StudentGradesRequest:
				return p.Grades.Student(sqlCtx, actual, req, synced)
			case contract.BatchGradesRequest:
				return p.Grades.Batch(sqlCtx, actual, req, synced)
			case contract.StudentTimetableRequest:
				return p.Timetable.Student(sqlCtx, actual, req, synced)
			case contract.StaffTimetableRequest:
				return p.Timetable.Staff(sqlCtx, actual, req, synced)
			default:
				return nil, errors.New("invalid parsed query operation")
			}
		})
		if loadErr != nil {
			return nil, loadErr
		}
		return querymeta.Payload(value)
	})
	if err != nil {
		if errors.Is(err, timetable.ErrAmbiguous) {
			return nil, &transport.Failure{Category: contract.Conflict, Cause: err}
		}
		return nil, err
	}
	// Read this even on a hit: an unchanged-data round can advance checked_at.
	synced, err := p.Metadata.SyncedAt(ctx, h, op)
	if err != nil {
		return nil, err
	}
	response, err := querymeta.Compose(op, result.Data, synced, p.Times)
	if err != nil {
		return nil, err
	}
	return transport.Reply{Data: response, Snapshot: token, Combination: h.Combination}, nil
}
