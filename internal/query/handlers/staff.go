package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/staff"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

func Staff(key string, svc staff.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("staff dependencies required")
	}
	return transport.Business(contract.Staff, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.StaffRequest)
		if !ok {
			return nil, &transport.Failure{Category: contract.Internal}
		}
		result, err := read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.Staff)
			if err != nil {
				return nil, err
			}
			return svc.Query(ctx, h, req, synced)
		})
		return result, phase2Failure(err)
	})
}
