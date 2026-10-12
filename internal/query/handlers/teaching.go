package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/teaching"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func StaffTeachingClasses(key string, svc teaching.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("teaching class dependencies required")
	}
	return transport.Business(contract.StaffTeachingClasses, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.StaffTeachingClassesRequest)
		if !ok {
			return nil, &transport.Failure{Category: contract.Internal}
		}
		result, err := read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.StaffTeachingClasses)
			if err != nil {
				return nil, err
			}
			return svc.Staff(ctx, h, req, synced)
		})
		return result, phase2Failure(err)
	})
}

func TeachingClassStudents(key string, svc teaching.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("class roster dependencies required")
	}
	return transport.Business(contract.TeachingClassStudents, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.TeachingClassStudentsRequest)
		if !ok {
			return nil, &transport.Failure{Category: contract.Internal}
		}
		result, err := read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.TeachingClassStudents)
			if err != nil {
				return nil, err
			}
			return svc.Students(ctx, h, req, synced)
		})
		return result, phase2Failure(err)
	})
}
