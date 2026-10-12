package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/compute"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/organizations"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

// phase2Failure keeps source consistency failures distinct from unavailable SQL.
func phase2Failure(err error) error {
	if errors.Is(err, compute.ErrValue) || errors.Is(err, compute.ErrRecords) || errors.Is(err, compute.ErrTime) || errors.Is(err, read.ErrOrder) || errors.Is(err, read.ErrAmbiguous) {
		return &transport.Failure{Category: contract.Internal, Cause: err}
	}
	return err
}

func Organizations(key string, svc organizations.Service, admission read.Admission, metadata Metadata, recorder *events.Recorder, observe transport.Observation) (http.Handler, error) {
	if svc.Source == nil || svc.Clock == nil || admission == nil || metadata == nil {
		return nil, errors.New("organization dependencies required")
	}
	return transport.Business(contract.Organizations, key, recorder, observe, func(ctx context.Context, input any) (any, error) {
		req, ok := input.(contract.OrganizationsRequest)
		if !ok {
			return nil, &transport.Failure{Category: contract.Internal}
		}
		result, err := read.Execute(ctx, admission, "", func(ctx context.Context, h read.Handle) (any, error) {
			synced, err := metadata.SyncedAt(ctx, h, contract.Organizations)
			if err != nil {
				return nil, err
			}
			return svc.Query(ctx, h, req, synced)
		})
		return result, phase2Failure(err)
	})
}
