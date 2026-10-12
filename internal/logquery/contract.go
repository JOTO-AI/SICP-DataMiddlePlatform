// Package logquery defines bounded management log reads independently of storage.
package logquery

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
)

type Source string
type Mode string
type IDFormat string

const (
	HistoryMode   Mode        = "history"
	LiveMode      Mode        = "live"
	RawID         IDFormat    = "raw"
	CorrelationID IDFormat    = "correlation"
	Audit         events.Kind = "audit"
)

const (
	Platform        Source = "platform"
	Upstream        Source = "upstream"
	DefaultPageSize        = 50
	MaxPageSize            = 200
	MaxRange               = 7 * 24 * time.Hour
	MaxCursorAge           = 24 * time.Hour
	MaxCursorBytes         = 4096
)

var (
	ErrArgument    = errors.New("invalid log query")
	ErrCursor      = errors.New("invalid log cursor")
	ErrExpired     = errors.New("log cursor expired")
	ErrUnavailable = errors.New("log store unavailable")
	ErrLimit       = errors.New("log query limit exceeded")
	ErrNotFound    = errors.New("log event not found")
	ErrForbidden   = errors.New("log scope forbidden")
	ErrTooLarge    = errors.New("management request too large")
	ErrTimeout     = errors.New("log query timed out")
	ErrProxy       = errors.New("management proxy unavailable")
)

// time.Parse accepts some non-RFC3339 offsets and single-digit hours. Keep the
// public input narrower and reject sub-microsecond precision even if all zero.
var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func (s Source) Valid() bool { return s == Platform || s == Upstream }

// Filter uses UTC microseconds, matching persisted PostgreSQL timestamps.
// Identifiers are canonical correlations, never raw IDs or personnel payloads.
type Filter struct {
	Mode      Mode               `json:"mode"`
	From      time.Time          `json:"from"`
	To        time.Time          `json:"to"`
	Kind      events.Kind        `json:"kind,omitempty"`
	Phase     events.Phase       `json:"phase,omitempty"`
	Outcome   events.Outcome     `json:"outcome,omitempty"`
	Operation contract.Operation `json:"operation,omitempty"`
	TaskID    string             `json:"task_id,omitempty"`
	RequestID string             `json:"request_id,omitempty"`
	TestRunID string             `json:"test_run_id,omitempty"`
	PageSize  int                `json:"page_size"`
}

func (f Filter) Validate() error {
	if !validTime(f.From) || f.PageSize < 1 || f.PageSize > MaxPageSize || !validCorrelation(f.TaskID) || !validCorrelation(f.RequestID) || !validCorrelation(f.TestRunID) {
		return ErrArgument
	}
	if f.Mode == HistoryMode {
		if !validTime(f.To) || !f.From.Before(f.To) || f.To.Sub(f.From) > MaxRange {
			return ErrArgument
		}
	} else if f.Mode != LiveMode || !f.To.IsZero() {
		return ErrArgument
	}
	switch f.Kind {
	case "", events.Request, events.Task, events.Step, events.Call, events.Attempt, Audit:
	default:
		return ErrArgument
	}
	switch f.Phase {
	case "", events.Started, events.Finished, events.Transport:
	default:
		return ErrArgument
	}
	switch f.Outcome {
	case "", events.Succeeded, events.Failed, events.Interrupted:
	default:
		return ErrArgument
	}
	if f.Operation != "" {
		found := false
		for _, d := range contract.Definitions() {
			found = found || d.Operation == f.Operation
		}
		if !found {
			return ErrArgument
		}
	}
	return nil
}

func validTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 2000 && t.UTC().Year() <= 9999 && t.Nanosecond()%1000 == 0
}

func validCorrelation(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// MarshalJSON exposes the absent live upper bound as null, not year 0001.
func (f Filter) MarshalJSON() ([]byte, error) {
	type plain Filter
	var to *time.Time
	if !f.To.IsZero() {
		value := f.To.UTC()
		to = &value
	}
	return json.Marshal(struct {
		plain
		To *time.Time `json:"to"`
	}{plain(canonical(f)), to})
}

type Correlator interface{ Correlation(string) string }

// NormalizeIdentity uses an explicit format; even a 64-character raw ID is
// hashed once. The injected correlator must be the configured log sanitizer.
func NormalizeIdentity(value string, format IDFormat, ids Correlator) (string, error) {
	if value == "" {
		return "", ErrArgument
	}
	switch format {
	case RawID:
		if !validIdentity(value) || ids == nil {
			return "", ErrArgument
		}
		value = ids.Correlation(value)
	case CorrelationID:
	default:
		return "", ErrArgument
	}
	if value == "" || !validCorrelation(value) {
		return "", ErrArgument
	}
	return value, nil
}

func validIdentity(s string) bool {
	if len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func canonical(f Filter) Filter {
	f.From, f.To = f.From.UTC(), f.To.UTC()
	return f
}

// ParseFilter rejects empty/duplicate/unknown parameters rather than allowing
// the HTTP layer and signed cursor to interpret the same request differently.
// On continuation, base must be the authenticated cursor's filter. Omitted
// values then retain the original interval, including server-generated dates.
func ParseFilter(values url.Values, now time.Time, base *Filter, ids Correlator) (Filter, string, error) {
	now = now.UTC().Truncate(time.Microsecond)
	f := Filter{Mode: HistoryMode, To: now, PageSize: DefaultPageSize}
	f.From = f.To.Add(-24 * time.Hour)
	if base != nil {
		if base.Validate() != nil {
			return Filter{}, "", ErrArgument
		}
		f = canonical(*base)
	}
	format := RawID
	if values.Has("id_format") {
		format = IDFormat(values.Get("id_format"))
		if format != RawID && format != CorrelationID {
			return Filter{}, "", ErrArgument
		}
	}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return Filter{}, "", ErrArgument
		}
		v := entries[0]
		switch key {
		case "mode":
			f.Mode = Mode(v)
		case "id_format": // Already checked; never persist input format in cursors.
		case "from", "to":
			if len(v) > 35 || !timestampPattern.MatchString(v) {
				return Filter{}, "", ErrArgument
			}
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil || t.Nanosecond()%1000 != 0 {
				return Filter{}, "", ErrArgument
			}
			if key == "from" {
				f.From = t.UTC()
			} else {
				f.To = t.UTC()
			}
		case "kind":
			f.Kind = events.Kind(v)
		case "phase":
			f.Phase = events.Phase(v)
		case "outcome":
			f.Outcome = events.Outcome(v)
		case "operation":
			f.Operation = contract.Operation(v)
		case "task_id", "request_id", "test_run_id":
			id, err := NormalizeIdentity(v, format, ids)
			if err != nil {
				return Filter{}, "", err
			}
			switch key {
			case "task_id":
				f.TaskID = id
			case "request_id":
				f.RequestID = id
			case "test_run_id":
				f.TestRunID = id
			}
		case "page_size":
			n, err := strconv.Atoi(v)
			if err != nil || strconv.Itoa(n) != v {
				return Filter{}, "", ErrArgument
			}
			f.PageSize = n
		case "cursor":
			if len(v) > MaxCursorBytes {
				return Filter{}, "", ErrCursor
			}
		default:
			return Filter{}, "", ErrArgument
		}
	}
	if base == nil {
		if !values.Has("from") {
			f.From = f.To.Add(-24 * time.Hour)
		}
		if f.Mode == LiveMode {
			f.To = time.Time{}
			if f.From.After(now) || now.Sub(f.From) > MaxRange {
				return Filter{}, "", ErrArgument
			}
		}
	}
	if f.Mode == LiveMode && values.Has("to") {
		return Filter{}, "", ErrArgument
	}
	if err := f.Validate(); err != nil {
		return Filter{}, "", err
	}
	if base != nil && canonical(f) != canonical(*base) {
		return Filter{}, "", ErrCursor
	}
	return canonical(f), values.Get("cursor"), nil
}

func HTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrArgument), errors.Is(err, ErrCursor):
		return 400, "invalid_argument"
	case errors.Is(err, ErrExpired):
		return 409, "cursor_expired"
	case errors.Is(err, ErrForbidden):
		return 403, "forbidden"
	case errors.Is(err, ErrTooLarge):
		return 413, "request_too_large"
	case errors.Is(err, ErrTimeout):
		return 504, "deadline_exceeded"
	case errors.Is(err, ErrProxy):
		return 502, "proxy_unavailable"
	case errors.Is(err, ErrLimit):
		return 429, "resource_exhausted"
	case errors.Is(err, ErrNotFound):
		return 404, "not_found"
	default:
		return 503, "unavailable"
	}
}
