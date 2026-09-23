package compute

import (
	"errors"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
)

var ErrTime = errors.New("invalid source time or explicit timezone")
var naiveTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d{1,6})?$`)

type Times struct{ location *time.Location }

func NewTimes(zone string) (Times, error) {
	if zone == "" || zone == "Local" {
		return Times{}, ErrTime
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return Times{}, ErrTime
	}
	return Times{location: location}, nil
}

// Source interprets naive calendar fields in the explicitly configured zone.
// Already zoned RFC3339 input represents an instant and is converted, not
// reinterpreted. Absence stays nil; malformed present input is an error.
func (t Times) Source(raw *string) (*string, error) {
	if t.location == nil {
		return nil, ErrTime
	}
	if raw == nil {
		return nil, nil
	}
	var parsed time.Time
	var err error
	if naiveTime.MatchString(*raw) {
		canonical := strings.Replace(*raw, " ", "T", 1)
		parsed, err = time.ParseInLocation("2006-01-02T15:04:05.999999", canonical, t.location)
		// Reject nonexistent local times silently shifted across a DST gap.
		if err == nil && parsed.Format("2006-01-02T15:04:05") != canonical[:19] {
			return nil, ErrTime
		}
	} else {
		parsed, err = time.Parse(time.RFC3339Nano, *raw)
	}
	if err != nil || parsed.Year() < 1 || parsed.Nanosecond()%1000 != 0 {
		return nil, ErrTime
	}
	return t.Instant(parsed)
}

// Instant formats a known persisted instant, e.g. a confirmed synced_at. It
// never falls back to Now and does not accept a missing/zero confirmation.
func (t Times) Instant(value time.Time) (*string, error) {
	if t.location == nil || value.IsZero() {
		return nil, ErrTime
	}
	local := value.In(t.location)
	if local.Year() < 1 || local.Year() > 9999 {
		return nil, ErrTime
	}
	out := local.Format(time.RFC3339Nano)
	return &out, nil
}

func (t Times) Latest(values ...*string) (*string, error) {
	var latest *string
	var at time.Time
	for _, raw := range values {
		formatted, err := t.Source(raw)
		if err != nil {
			return nil, err
		}
		if formatted == nil {
			continue
		}
		v, err := time.Parse(time.RFC3339Nano, *formatted)
		if err != nil {
			return nil, ErrTime
		}
		if latest == nil || v.After(at) {
			at = v
			latest = formatted
		}
	}
	return latest, nil
}
