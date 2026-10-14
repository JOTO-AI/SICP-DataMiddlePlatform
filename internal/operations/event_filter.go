package operations

import (
	"net/url"
	"regexp"
	"time"
)

// EventFilter applies to accepted_at, not mutable stage timestamps.
// The signed cursor retains this range and every supplied predicate.
type EventFilter struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Mode   string    `json:"mode,omitempty"`
	Status string    `json:"status,omitempty"`
	TaskID string    `json:"task_id,omitempty"`
}

var eventStatusPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var eventTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func (f EventFilter) Validate() error {
	if f.From.IsZero() || f.To.IsZero() || !f.From.Before(f.To) || f.To.Sub(f.From) > 7*24*time.Hour || f.From.Nanosecond()%1000 != 0 || f.To.Nanosecond()%1000 != 0 {
		return ErrArgument
	}
	if f.Mode != "" && f.Mode != "initialize" && f.Mode != "incremental" && f.Mode != "force_full" {
		return ErrArgument
	}
	if f.Status != "" && !eventStatusPattern.MatchString(f.Status) || f.TaskID != "" && !ValidID(f.TaskID) {
		return ErrArgument
	}
	return nil
}
func BindEventFilter(p *Token, values url.Values) (EventFilter, error) {
	f := EventFilter{From: p.At.Add(-24 * time.Hour), To: p.At}
	if values.Get("cursor") != "" && p.Events == nil {
		return f, ErrConflict
	}
	if p.Events != nil {
		f = *p.Events
	}
	for key, list := range values {
		if len(list) != 1 || list[0] == "" {
			return f, ErrArgument
		}
		value := list[0]
		switch key {
		case "cursor", "page_size":
		case "from", "to":
			if len(value) > 35 || !eventTimePattern.MatchString(value) {
				return f, ErrArgument
			}
			at, e := time.Parse(time.RFC3339Nano, value)
			if e != nil {
				return f, ErrArgument
			}
			at = at.UTC()
			if key == "from" {
				f.From = at
			} else {
				f.To = at
			}
		case "mode":
			f.Mode = value
		case "status":
			f.Status = value
		case "task_id":
			f.TaskID = value
		default:
			return f, ErrArgument
		}
	}
	if p.Events == nil && !values.Has("from") {
		f.From = f.To.Add(-24 * time.Hour)
	}
	if err := f.Validate(); err != nil {
		return f, err
	}
	if p.Events != nil && (!f.From.Equal(p.Events.From) || !f.To.Equal(p.Events.To) || f.Mode != p.Events.Mode || f.Status != p.Events.Status || f.TaskID != p.Events.TaskID) {
		return f, ErrArgument
	}
	p.Events = &f
	return f, nil
}
