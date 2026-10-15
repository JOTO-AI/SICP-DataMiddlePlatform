package events

import (
	"context"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/diagnostic"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/contract"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// Metadata is a typed allowlist, never a map of caller-controlled attributes.
type Metadata struct {
	Authentication *diagnostic.Authentication `json:"authentication,omitempty"`
	Build          *BuildStage                `json:"build,omitempty"`
	Source         string                     `json:"source,omitempty"`
	Path           string                     `json:"path,omitempty"`
	Mode           string                     `json:"mode,omitempty"`
	Page           int                        `json:"page,omitempty"`
	PageSize       int                        `json:"page_size,omitempty"`
	StartTime      string                     `json:"start_time,omitempty"`
	EndTime        string                     `json:"end_time,omitempty"`
	Operation      contract.Operation         `json:"operation,omitempty"`
	Failure        string                     `json:"failure,omitempty"`
}

func (Metadata) String() string     { return "[event metadata redacted]" }
func (m Metadata) GoString() string { return m.String() }
func (m Metadata) valid() bool {
	if m.Authentication != nil && (!m.Authentication.Valid() || (m.Failure != "authentication" && m.Failure != "cancelled" && m.Failure != "request_timeout" && m.Failure != "budget")) {
		return false
	}
	if m.Build != nil && (!m.Build.valid() || m.Source != "" || m.Operation != "") {
		return false
	}
	if m.Source != "" {
		found := false
		for _, table := range source.Catalog() {
			if table.Name == m.Source {
				found = true
				break
			}
		}
		if !found || m.Path != "/apizt/openapi/invoke/sjzc/release/"+m.Source || m.Page < 1 || m.PageSize < 1 {
			return false
		}
		switch m.Mode {
		case "full", "force_full":
			if m.StartTime != "" || m.EndTime != "" {
				return false
			}
		case "incremental":
			if !windowTime(m.StartTime) || !windowTime(m.EndTime) {
				return false
			}
		default:
			return false
		}
	} else if m.Path != "" || m.Mode != "" || m.Page != 0 || m.PageSize != 0 || m.StartTime != "" || m.EndTime != "" {
		return false
	}
	if m.Operation != "" {
		found := false
		for _, d := range contract.Definitions() {
			if m.Operation == d.Operation {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	switch m.Failure {
	case "", "unknown", "authentication", "configuration", "transport", "http", "business", "structure", "response_limit", "cancelled", "record", "retry_wait", "observation", "budget", "request_timeout", "cooldown", "cooldown_storage":
	default:
		return false
	}
	return true
}
func windowTime(s string) bool {
	if len(s) < 19 || len(s) > 35 || strings.Contains(s, ",") {
		return false
	}
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		digits := 0
		for i := dot + 1; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
			digits++
		}
		if digits < 1 || digits > 9 {
			return false
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

type metadataKey struct{}

func WithMetadata(ctx context.Context, m Metadata) context.Context {
	return context.WithValue(ctx, metadataKey{}, m)
}

// Unknown classifications cannot discard the failure record. Retain the
// detailed reason separately in the bounded request diagnostic.
func NormalizeFailure(failure string) string {
	m := Metadata{Failure: failure}
	if !m.valid() {
		return "unknown"
	}
	return failure
}
