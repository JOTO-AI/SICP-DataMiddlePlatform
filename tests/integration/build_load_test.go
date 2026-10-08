//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/transport"
)

// A versioned closed-loop workload: one sequential client per operation, each
// paced at one request/second, at most four in flight. Slow requests reduce the
// offered rate rather than creating an unbounded queue. Both phases use it.
const onlineLoadVersion = "bp07-four-operations-v1"

type loadRequest struct{ Operation, Path, Body string }
type loadResult struct {
	Count, Errors int
	P95NS         int64
	Failure       string
	// Only hashes of synthetic business data, never response bodies or tokens.
	Responses map[string]map[string]int
}
type loadPhase struct {
	StartedAt, FinishedAt time.Time
	Operations            map[string]loadResult
}
type onlineEvidence struct {
	Version                                 string
	Concurrency, IntervalMS, MinimumSamples int
	Baseline, Updating                      loadPhase
	References                              map[string]map[string]string
	LatencyGate                             bool
	Verified                                bool
	Failure                                 string
}

func onlineRequests() []loadRequest {
	bodies := map[contract.Operation]string{
		contract.StudentGrades:    `{"student_number":"000000000","academic_year":"all","page":1,"page_size":10}`,
		contract.BatchGrades:      `{"student_numbers":["000000000","000000001"],"academic_year":"all","page":1,"page_size":10}`,
		contract.StudentTimetable: `{"student_number":"000000000","academic_year":"2022-2023","semester":"1"}`,
		contract.StaffTimetable:   `{"staff_number":"000000000","academic_year":"2022-2023","semester":"1"}`,
	}
	var requests []loadRequest
	for _, def := range contract.Definitions() {
		requests = append(requests, loadRequest{string(def.Operation), def.Path, bodies[def.Operation]})
	}
	return requests
}

func loadResponse(ctx context.Context, client *http.Client, origin, key string, request loadRequest) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+request.Path, strings.NewReader(request.Body))
	if err != nil {
		return "", "", errors.New("request construction failed")
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", errors.New("request transport failed")
	}
	// Match the production response bound; retain only one response per client.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	closeErr := resp.Body.Close()
	var envelope struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	bundle := resp.Header.Get(transport.CombinationHeader)
	if readErr != nil || closeErr != nil || len(raw) > 16<<20 || resp.StatusCode != http.StatusOK || bundle == "" || len(bundle) > 256 || json.Unmarshal(raw, &envelope) != nil || envelope.Code != "ok" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return "", "", errors.New("business response failed")
	}
	h := sha256.Sum256(envelope.Data)
	return bundle, hex.EncodeToString(h[:]), nil
}

// Stopping prevents new requests and joins all clients without cancelling an
// in-flight successful response. The global context and HTTP timeout bound join.
func startOnlineLoad(ctx context.Context, origin, key string) func() loadPhase {
	stop := make(chan struct{})
	done := make(chan loadPhase, 1)
	phase := loadPhase{StartedAt: time.Now().UTC(), Operations: map[string]loadResult{}}
	requests := onlineRequests()
	results := make([]loadResult, len(requests))
	var clients sync.WaitGroup
	for i, request := range requests {
		clients.Add(1)
		go func() {
			defer clients.Done()
			pool := &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: 25 * time.Second}
			defer pool.CloseIdleConnections()
			client := &http.Client{Transport: pool, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			result := loadResult{Responses: map[string]map[string]int{}}
			// 65536 exact samples/client exceeds the 100 minute runner budget at
			// 1 Hz. Overflow fails explicitly; it never truncates a passing p95.
			latencies := make([]int64, 0, 128)
			defer func() {
				if len(latencies) > 0 {
					slices.Sort(latencies)
					result.P95NS = latencies[(95*len(latencies)+99)/100-1]
				}
				results[i] = result
			}()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					result.Failure = "load context expired"
					return
				default:
				}
				if len(latencies) == 65536 {
					result.Failure = "load sample capacity exceeded"
					return
				}
				started := time.Now()
				bundle, digest, err := loadResponse(ctx, client, origin, key, request)
				latencies = append(latencies, time.Since(started).Nanoseconds())
				result.Count++
				if err != nil {
					result.Errors++
					result.Failure = err.Error()
				} else {
					if result.Responses[bundle] == nil {
						if len(result.Responses) == 2 {
							result.Failure = "unexpected third combination"
							return
						}
						result.Responses[bundle] = map[string]int{}
					}
					digests := result.Responses[bundle]
					if digests[digest] == 0 && len(digests) == 2 {
						result.Failure = "unstable response digest"
						return
					}
					digests[digest]++
				}
				select {
				case <-stop:
					return
				case <-ctx.Done():
					result.Failure = "load context expired"
					return
				case <-ticker.C:
				}
			}
		}()
	}
	go func() {
		clients.Wait()
		for i, request := range requests {
			phase.Operations[request.Operation] = results[i]
		}
		phase.FinishedAt = time.Now().UTC()
		done <- phase
	}()
	var once sync.Once
	var final loadPhase
	return func() loadPhase { once.Do(func() { close(stop); final = <-done }); return final }
}

func loadReference(ctx context.Context, origin, key, bundle string) (map[string]string, error) {
	pool := &http.Transport{MaxConnsPerHost: 1}
	defer pool.CloseIdleConnections()
	client := &http.Client{Transport: pool, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	out := map[string]string{}
	for _, request := range onlineRequests() {
		got, digest, err := loadResponse(ctx, client, origin, key, request)
		if err != nil || got != bundle {
			return nil, errors.New("fixed-version query reference failed: " + request.Operation)
		}
		out[request.Operation] = digest
	}
	return out, nil
}

func verifyLoadPhase(phase loadPhase, references map[string]map[string]string, minimum int) error {
	if len(phase.Operations) != 4 || minimum < 1 {
		return errors.New("incomplete load operations")
	}
	for _, request := range onlineRequests() {
		result, ok := phase.Operations[request.Operation]
		if !ok || result.Count < minimum || result.Errors != 0 || result.Failure != "" || result.P95NS <= 0 {
			return errors.New("failed or insufficient load samples")
		}
		observed := 0
		for bundle, digests := range result.Responses {
			want := references[bundle][request.Operation]
			if want == "" || len(digests) != 1 {
				return errors.New("unknown combination or mixed response")
			}
			for digest, n := range digests {
				if digest != want || n < 1 {
					return errors.New("response differs from fixed-version reference")
				}
				observed += n
			}
		}
		if observed != result.Count {
			return errors.New("incomplete response evidence")
		}
	}
	return nil
}

func verifyOnlineLoad(e *onlineEvidence, references map[string]map[string]string, strictLatency bool) error {
	if err := verifyLoadPhase(e.Updating, references, e.MinimumSamples); err != nil {
		return err
	}
	for op, result := range e.Updating.Operations {
		base := e.Baseline.Operations[op]
		if strictLatency && result.P95NS > base.P95NS*6/5 {
			return errors.New("query p95 degraded by more than twenty percent")
		}
	}
	return nil
}

func TestI05_B10OnlineLoadRejectsMixedOrMissingEvidence(t *testing.T) {
	references := map[string]map[string]string{"old": {}, "new": {}}
	phase := loadPhase{Operations: map[string]loadResult{}}
	for _, request := range onlineRequests() {
		references["old"][request.Operation] = "old-digest"
		references["new"][request.Operation] = "new-digest"
		phase.Operations[request.Operation] = loadResult{Count: 2, P95NS: 100, Responses: map[string]map[string]int{"old": {"old-digest": 1}, "new": {"new-digest": 1}}}
	}
	if err := verifyLoadPhase(phase, references, 2); err != nil {
		t.Fatal(err)
	}
	op := onlineRequests()[0].Operation
	result := phase.Operations[op]
	result.Responses["new"] = map[string]int{"old-digest": 1}
	phase.Operations[op] = result
	if verifyLoadPhase(phase, references, 2) == nil {
		t.Fatal("mixed version accepted")
	}
	result.Responses["new"] = map[string]int{"new-digest": 1}
	result.Count = 3
	phase.Operations[op] = result
	if verifyLoadPhase(phase, references, 2) == nil {
		t.Fatal("missing sample accepted")
	}
	result.Count, result.Errors = 2, 1
	phase.Operations[op] = result
	if verifyLoadPhase(phase, references, 2) == nil {
		t.Fatal("query failure accepted")
	}
	result.Errors = 0
	phase.Operations[op] = result
	e := &onlineEvidence{MinimumSamples: 2, Baseline: loadPhase{Operations: map[string]loadResult{}}, Updating: phase}
	for name, sample := range phase.Operations {
		sample.P95NS = 100
		e.Baseline.Operations[name] = sample
	}
	result.P95NS = 120
	phase.Operations[op] = result
	if err := verifyOnlineLoad(e, references, true); err != nil {
		t.Fatal("twenty percent boundary rejected", err)
	}
	result.P95NS = 121
	phase.Operations[op] = result
	if verifyOnlineLoad(e, references, true) == nil {
		t.Fatal("latency regression accepted")
	}
}
