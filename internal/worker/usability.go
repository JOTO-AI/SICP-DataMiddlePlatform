package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/cache"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/snapshot"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

var ErrUsability = errors.New("authenticated new-combination probe incomplete")

// Probe uses ordinary authentication and the fixed four phase-one HTTP probes.
// Synthetic absent identities exercise empty business answers without logging
// personnel data. This is not evidence that every public business rule passed;
// candidate validation checks source storage, and new APIs have separate acceptance.
func Probe(ctx context.Context, origin, key, bundle string) error {
	return probe(ctx, origin, key, bundle, nil)
}

func probe(ctx context.Context, origin, key, bundle string, release func(context.Context, string, string, string, string) error) error {
	if key == "" || bundle == "" {
		return ErrUsability
	}
	transportPool := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	defer transportPool.CloseIdleConnections()
	client := &http.Client{Transport: transportPool, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, def := range contract.LegacyBusinessDefinitions() {
		body := `{"student_number":"__build_probe__","academic_year":"all","page":1,"page_size":1}`
		switch def.Operation {
		case contract.BatchGrades:
			body = `{"student_numbers":["__build_probe__"],"academic_year":"all","page":1,"page_size":1}`
		case contract.StudentTimetable:
			body = `{"student_number":"__build_probe__","academic_year":"2000-2001","semester":"1"}`
		case contract.StaffTimetable:
			body = `{"staff_number":"__build_probe__","academic_year":"2000-2001","semester":"1"}`
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(origin, "/")+def.Path, strings.NewReader(body))
		if err != nil {
			return ErrUsability
		}
		req.Header.Set("X-API-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return ErrUsability
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
		closeErr := resp.Body.Close()
		var envelope struct {
			Code string          `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if readErr != nil || closeErr != nil || len(raw) > 1<<20 || resp.StatusCode != 200 || resp.Header.Get(transport.CombinationHeader) != bundle || json.Unmarshal(raw, &envelope) != nil || envelope.Code != "ok" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return ErrUsability
		}
		buildmetric.Add(ctx, buildmetric.Cases, 1)
		if release != nil && (def.Operation == contract.StudentGrades || def.Operation == contract.BatchGrades) {
			request, err := transport.Parse(def.Operation, strings.NewReader(body))
			if err != nil {
				return ErrUsability
			}
			conditions, err := cache.Key(bundle, def.Operation, request)
			if err != nil {
				return ErrUsability
			}
			if err = release(ctx, resp.Header.Get(snapshot.Header), bundle, string(def.Operation), conditions); err != nil {
				return ErrUsability
			}
		}
	}
	return ctx.Err()
}

func (w Executor) recordUsability(ctx context.Context, task control.Task, owner control.Ownership) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	repo := w.Runtime.Control.WithOwnership(owner)
	timing, err := repo.BuildTiming(ctx, task.ID)
	if err != nil || timing == nil || timing.UsableAt != nil {
		return err
	}
	active, exists, err := w.Runtime.Active.Read(ctx)
	if err != nil || !exists || active.TaskID != task.ID {
		return ErrUsability
	}
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "usable_probe", Combination: active.Position.BundleID}, "")
	defer func() { end(err) }()
	if err = probe(ctx, w.Config.ConsoleQueryURL, w.Config.Service.APIKey, active.Position.BundleID, repo.ReleaseBuildProbeSnapshot); err != nil {
		return err
	}
	after, exists, err := w.Runtime.Active.Read(ctx)
	if err != nil || !exists || after != active {
		return ErrUsability
	}
	return repo.RecordBuildUsable(ctx, active.Position.BundleID, w.Clock.Now())
}
