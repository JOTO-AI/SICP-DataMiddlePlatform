package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/timetable"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func StudentTimetable(key string, svc timetable.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("student timetable dependencies required")
	}
	return transport.Business(contract.StudentTimetable, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.StudentTimetableRequest)
		if !ok {
			return nil, errors.New("invalid student timetable request")
		}
		result, err := read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.StudentTimetable)
			if err != nil {
				return nil, err
			}
			return svc.Student(ctx, h, req, synced)
		})
		if errors.Is(err, timetable.ErrAmbiguous) {
			return nil, &transport.Failure{Category: contract.Conflict, Cause: err}
		}
		return result, err
	})
}

func StaffTimetable(key string, svc timetable.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("staff timetable dependencies required")
	}
	return transport.Business(contract.StaffTimetable, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.StaffTimetableRequest)
		if !ok {
			return nil, errors.New("invalid staff timetable request")
		}
		return read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.StaffTimetable)
			if err != nil {
				return nil, err
			}
			return svc.Staff(ctx, h, req, synced)
		})
	})
}
