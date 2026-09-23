package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/grades"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

// Metadata must return the confirmed sync fact for this pinned combination and
// operation, never the latest live route or request time. Implemented in P11.
type Metadata interface {
	SyncedAt(context.Context, read.Handle, contract.Operation) (time.Time, error)
}

func StudentGrades(key string, svc grades.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("student grades dependencies required")
	}
	return transport.Business(contract.StudentGrades, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.StudentGradesRequest)
		if !ok {
			return nil, errors.New("invalid student grade request")
		}
		return read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.StudentGrades)
			if err != nil {
				return nil, err
			}
			return svc.Student(ctx, h, req, synced)
		})
	})
}

func BatchGrades(key string, svc grades.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("batch grades dependencies required")
	}
	return transport.Business(contract.BatchGrades, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.BatchGradesRequest)
		if !ok {
			return nil, errors.New("invalid batch grade request")
		}
		return read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.BatchGrades)
			if err != nil {
				return nil, err
			}
			return svc.Batch(ctx, h, req, synced)
		})
	})
}
