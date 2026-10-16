package logquery

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/observability"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

// Repository reads each batch and its epoch/floor from one short snapshot.
// Database must be the identity-verifying adapter in dual mode.
type Repository struct {
	Registry  storage.Registry
	Database  storage.Database
	Sanitizer *observability.Sanitizer
}
type Snapshot struct {
	Epoch            string
	HighWater, Floor int64
}
type Record struct {
	Sequence   int64
	ReceivedAt time.Time
	Event      observability.SafeEvent
}
type ReadRequest struct {
	Filter  Filter
	Cursor  *Cursor
	EventID string
	Stream  bool
}
type Batch struct {
	Snapshot Snapshot
	Records  []Record
}
type Reader interface {
	Read(context.Context, Source, ReadRequest) (Batch, error)
}

func (r Repository) Read(ctx context.Context, source Source, request ReadRequest) (out Batch, err error) {
	if !source.Valid() || r.Database == nil || r.Sanitizer == nil {
		return out, ErrUnavailable
	}
	if request.EventID != "" && !validCorrelation(request.EventID) {
		return out, ErrArgument
	}
	if request.EventID == "" && request.Filter.Validate() != nil {
		return out, ErrArgument
	}
	if request.Stream && (request.Cursor == nil || request.Cursor.Direction != Stream) {
		return out, ErrArgument
	}
	connection, e := r.Registry.LogReader(storage.LogSource(source))
	if e != nil {
		return out, ErrUnavailable
	}
	var tx storage.Transaction
	if db, ok := r.Database.(interface {
		BeginRead(context.Context, storage.Connection) (storage.Transaction, error)
	}); ok {
		tx, e = db.BeginRead(ctx, connection)
	} else {
		if r.Registry.LogMode() == storage.DualLogs {
			return out, ErrUnavailable
		}
		tx, e = r.Database.Begin(ctx, connection)
		if e == nil {
			_, e = tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY")
		}
	}
	if tx != nil {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			_ = tx.Rollback(cleanup)
		}()
	}
	if e != nil {
		return out, readError(ctx)
	}
	if tx.QueryRow(ctx, "SELECT epoch,high_water,floor FROM logs.commit_state WHERE singleton AND NOT has_table_privilege(current_user,'logs.events','INSERT,UPDATE,DELETE,TRUNCATE') AND NOT has_function_privilege(current_user,'logs.append_event(text,text,text,text,timestamptz,jsonb)','EXECUTE')").Scan(&out.Snapshot.Epoch, &out.Snapshot.HighWater, &out.Snapshot.Floor) != nil {
		return Batch{}, readError(ctx)
	}
	s := out.Snapshot
	if s.Epoch == "" || !validIdentity(s.Epoch) || s.Floor < 0 || s.HighWater < s.Floor {
		return Batch{}, ErrUnavailable
	}
	if c := request.Cursor; c != nil {
		if err := c.ValidateContinuation(request.Filter, s.Epoch, s.Floor); err != nil {
			return Batch{}, err
		}
		if c.HighWater > s.HighWater || c.Position > s.HighWater {
			return Batch{}, ErrExpired
		}
	}
	query, args := readSQL(request, s)
	q, ok := tx.(storage.QueryExecutor)
	if !ok {
		return Batch{}, ErrUnavailable
	}
	rows, e := q.Query(ctx, query, args...)
	if e != nil {
		return Batch{}, readError(ctx)
	}
	defer rows.Close()
	bytes := 0
	for rows.Next() {
		var rec Record
		var id, body string
		var size int
		if rows.Scan(&id, &rec.Sequence, &rec.ReceivedAt, &size, &body) != nil {
			return Batch{}, readError(ctx)
		}
		bytes += size
		if size <= 0 || size > MaxEventBytes || bytes > MaxResponseBytes || len(body) == 0 {
			return Batch{}, ErrLimit
		}
		safe, e := r.Sanitizer.Restore([]byte(body))
		if e != nil || safe.ID() != id || rec.Sequence <= s.Floor || rec.Sequence > s.HighWater || rec.ReceivedAt.IsZero() {
			return Batch{}, ErrUnavailable
		}
		if source == Upstream && safe.Event().Kind != "call" && safe.Event().Kind != "attempt" {
			return Batch{}, ErrUnavailable
		}
		rec.Event = safe
		out.Records = append(out.Records, rec)
		if len(out.Records) > MaxPageSize+1 {
			return Batch{}, ErrLimit
		}
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return Batch{}, readError(ctx)
	}
	rows.Close()
	if tx.Commit(ctx) != nil {
		return Batch{}, readError(ctx)
	}
	return out, nil
}

// All fragments are server-owned; every supplied value is a bound parameter.
func readSQL(r ReadRequest, s Snapshot) (string, []any) {
	args := []any{s.HighWater}
	where := "sequence <= $1"
	order := "DESC"
	limit := r.Filter.PageSize + 1
	if r.EventID != "" {
		where += " AND event_id=$2"
		args = append(args, r.EventID)
		limit = 1
	} else if r.Stream {
		order = "ASC"
		limit = MaxPageSize
		where += " AND sequence > $2"
		args = append(args, r.Cursor.Position)
	} else {
		high, position := s.HighWater, s.HighWater
		if r.Cursor != nil {
			high = r.Cursor.HighWater
			position = r.Cursor.Position
		}
		args[0] = high
		where += " AND sequence <= $2"
		if r.Cursor != nil {
			where = "sequence <= $1 AND sequence < $2"
		}
		args = append(args, position)
		where += " AND occurred_at >= $3 AND ($4::timestamptz IS NULL OR occurred_at < $4)"
		var to any
		if !r.Filter.To.IsZero() {
			to = r.Filter.To
		}
		args = append(args, r.Filter.From, to)
		where += " AND ($5='' OR kind=$5) AND ($6='' OR phase=$6) AND ($7='' OR payload #>> '{event,outcome}'=$7) AND ($8='' OR payload #>> '{event,metadata,operation}'=$8) AND ($9='' OR payload #>> '{event,identity,task_id}'=$9) AND ($10='' OR payload #>> '{event,identity,request_id}'=$10) AND ($11='' OR payload #>> '{event,identity,test_run_id}'=$11)"
		f := r.Filter
		args = append(args, string(f.Kind), string(f.Phase), string(f.Outcome), string(f.Operation), f.TaskID, f.RequestID, f.TestRunID)
	}
	// Compute sizes in SQL; oversized/cumulative payloads never cross the wire.
	query := "WITH page AS (SELECT event_id,sequence,received_at,payload,octet_length(payload::text) AS size FROM logs.events WHERE " + where + " ORDER BY sequence " + order + " LIMIT " + strconv.Itoa(limit) + ") SELECT event_id,sequence,received_at,size,CASE WHEN size<=262144 AND sum(size) OVER(ORDER BY sequence " + order + ")<=2097152 THEN payload::text ELSE '' END FROM page ORDER BY sequence " + order
	if r.Stream {
		query = "WITH page AS (SELECT event_id,sequence,received_at,payload,octet_length(payload::text) AS size FROM logs.events WHERE " + where + " ORDER BY sequence ASC LIMIT 200), sized AS (SELECT *,sum(size) OVER(ORDER BY sequence) AS total,row_number() OVER(ORDER BY sequence) AS ordinal FROM page) SELECT event_id,sequence,received_at,size,CASE WHEN size<=262144 AND total<=2097152 THEN payload::text ELSE '' END FROM sized WHERE total<=2097152 OR ordinal=1 ORDER BY sequence"
	}
	return query, args
}

func readError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	return ErrUnavailable
}

type Service struct {
	Reader     Reader
	Signer     *Signer
	Correlator Correlator
	Now        func() time.Time
	reads      chan struct{}
}

func NewService(reader Reader, signer *Signer, correlator Correlator) (*Service, error) {
	if reader == nil || signer == nil || correlator == nil {
		return nil, ErrArgument
	}
	return &Service{Reader: reader, Signer: signer, Correlator: correlator, Now: time.Now, reads: make(chan struct{}, 8)}, nil
}
func (s *Service) read(ctx context.Context, source Source, req ReadRequest) (Batch, error) {
	select {
	case s.reads <- struct{}{}:
		defer func() { <-s.reads }()
	default:
		return Batch{}, ErrLimit
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.Reader.Read(ctx, source, req)
}
func (s *Service) List(ctx context.Context, source Source, scope string, values url.Values) (Page, error) {
	var out Page
	now := s.Now().UTC().Truncate(time.Microsecond)
	var c *Cursor
	var base *Filter
	if token := values.Get("cursor"); token != "" {
		v, err := s.Signer.Decode(token, source, History, scope, now)
		if err != nil {
			return out, err
		}
		c = &v
		base = &v.Filter
	}
	f, _, err := ParseFilter(values, now, base, s.Correlator)
	if err != nil {
		return out, err
	}
	b, err := s.read(ctx, source, ReadRequest{Filter: f, Cursor: c})
	if err != nil {
		return out, err
	}
	if c == nil {
		token, err := s.Signer.Mint(source, b.Snapshot.Epoch, History, f, scope, b.Snapshot.HighWater, b.Snapshot.HighWater, b.Snapshot.Floor, now)
		if err != nil {
			return out, err
		}
		v, err := s.Signer.Decode(token, source, History, scope, now)
		if err != nil {
			return out, err
		}
		c = &v
	}
	out = Page{Items: []EventItem{}, Filter: f, Source: source, Epoch: c.Epoch, HighWater: c.HighWater, RetainedThrough: b.Snapshot.Floor, ExpiresAt: &c.ExpiresAt}
	for i, rec := range b.Records {
		if i == f.PageSize {
			v := *c
			v.Position = out.Items[len(out.Items)-1].Sequence
			token, e := s.Signer.Encode(v)
			if e != nil {
				return Page{}, e
			}
			out.NextCursor = &token
			break
		}
		out.Items = append(out.Items, project(source, c.Epoch, rec).EventItem)
	}
	if f.Mode == LiveMode {
		v, e := c.StreamStart()
		if e != nil {
			return Page{}, e
		}
		token, e := s.Signer.Encode(v)
		if e != nil {
			return Page{}, e
		}
		out.StreamCursor = &token
	}
	if !bounded(out) {
		return Page{}, ErrLimit
	}
	return out, nil
}
func (s *Service) Detail(ctx context.Context, source Source, id string) (Detail, error) {
	if id == "" || !validCorrelation(id) {
		return Detail{}, ErrArgument
	}
	b, err := s.read(ctx, source, ReadRequest{EventID: id})
	if err != nil {
		return Detail{}, err
	}
	if len(b.Records) == 0 {
		return Detail{}, ErrNotFound
	}
	if len(b.Records) != 1 {
		return Detail{}, ErrUnavailable
	}
	out := project(source, b.Snapshot.Epoch, b.Records[0])
	raw, e := json.Marshal(out)
	if e != nil || len(raw) > MaxEventBytes {
		return Detail{}, ErrLimit
	}
	return out, nil
}
func bounded(v any) bool {
	raw, e := json.Marshal(v)
	return e == nil && len(raw) <= MaxResponseBytes-512
}
func ptr[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}
func project(source Source, epoch string, r Record) Detail {
	e := r.Event.Event()
	e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Microsecond)
	r.ReceivedAt = r.ReceivedAt.UTC().Truncate(time.Microsecond)
	i := e.Identity
	out := Detail{EventItem: EventItem{Source: source, Epoch: epoch, EventID: e.ID, Sequence: r.Sequence, Kind: e.Kind, Phase: e.Phase, OccurredAt: &e.OccurredAt, ReceivedAt: &r.ReceivedAt, Outcome: ptr(e.Outcome), Operation: ptr(e.Metadata.Operation), RequestID: ptr(i.RequestID), TaskID: ptr(i.TaskID), StepID: ptr(i.StepID), CallID: ptr(i.CallID), AttemptID: ptr(i.AttemptID), AttemptNo: ptr(i.AttemptNo), HTTPStatus: e.HTTPStatus, BusinessCode: e.BusinessCode, DurationNS: e.DurationNS}, EntityID: e.EntityID, Metadata: e.Metadata, Response: e.Response, Diagnostics: e.Diagnostics}
	if i.Purpose != "" {
		out.Test = &TestIdentity{Purpose: Purpose(i.Purpose), TestRunID: i.TestRunID}
	}
	if a := e.Audit; a != nil {
		out.Audit = &AuditIdentity{ActorID: a.ActorID, Action: a.Action, ObjectID: a.ObjectID}
	}
	d := r.Event.Redaction()
	if b := e.Metadata.Build; b != nil {
		out.Context = &EventContext{Stage: b.Stage, Table: b.Table, Instance: i.Instance, Attempt: b.Attempt, Operation: b.Operation}
		// Zero is a real slot, but only a recorded generation identifies a slot.
		if b.Generation > 0 {
			out.Context.Slot = &b.Slot
		}
	} else if m := e.Metadata; m.Source != "" {
		out.Context = &EventContext{Table: m.Source, Page: m.Page, Mode: m.Mode}
	}
	out.Redaction = Redaction{Redacted: d.Redacted, Truncated: d.Truncated, OriginalBytes: d.OriginalBytes, FieldCount: d.FieldCount}
	return out
}
