package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

const MaxSmokeRequests = 8

// SmokeCases contains private deployment query conditions, never source records.
type SmokeCases map[contract.Operation][]json.RawMessage

func (SmokeCases) String() string   { return "[private smoke cases]" }
func (SmokeCases) GoString() string { return "[private smoke cases]" }

func ParseSmokeCases(raw string) (SmokeCases, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	bad := errors.New("invalid smoke cases configuration")
	if len(raw) > 64<<10 {
		return nil, bad
	}
	var cases SmokeCases
	dec := json.NewDecoder(strings.NewReader(raw))
	if dec.Decode(&cases) != nil || dec.Decode(new(any)) != io.EOF || len(cases) != len(contract.Definitions()) {
		return nil, bad
	}
	for _, def := range contract.Definitions() {
		requests := cases[def.Operation]
		if len(requests) < 1 || len(requests) > MaxSmokeRequests {
			return nil, bad
		}
		seen := map[string]bool{}
		for i, raw := range requests {
			req, err := Parse(def.Operation, bytes.NewReader(raw))
			if err != nil {
				return nil, bad
			}
			// Fixed batch probes must name students, otherwise one request could
			// silently expand into a whole-school regression.
			if batch, ok := req.(contract.BatchGradesRequest); ok && (len(batch.StudentNumbers) == 0 || len(batch.StudentNumbers) > MaxSmokeRequests) {
				return nil, bad
			}
			normalized, err := json.Marshal(req)
			if err != nil || seen[string(normalized)] {
				return nil, bad
			}
			seen[string(normalized)] = true
			requests[i] = normalized
		}
	}
	return cases, nil
}
