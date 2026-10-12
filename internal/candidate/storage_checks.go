package candidate

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/events"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ingest"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

type IndexReader interface {
	CheckPrimaryIndex(context.Context, storage.Position, string) error
}
type StorageChecks struct {
	Target            CompleteSource
	Source            read.Matcher
	Indexes           IndexReader
	Clock             ports.Clock
	Limits            read.Limits
	BackgroundTimeout time.Duration
}

func (s StorageChecks) SourceStorage(ctx context.Context, h read.Handle, m Manifest) (err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "content_check", Combination: h.Combination, Slot: h.Position.Slot, Generation: h.Generation}, string(h.Position.Instance))
	defer func() { end(err) }()
	if s.Target == nil || s.Source == nil || s.Clock == nil || m.Validate() != nil || h.Combination != m.Combination {
		return ErrBuild
	}
	if s.BackgroundTimeout != 0 {
		if s.BackgroundTimeout <= 0 || s.BackgroundTimeout > time.Hour {
			return read.ErrBudget
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.BackgroundTimeout)
		defer cancel()
	}
	if files, native := s.Target.(TargetFiles); native {
		// Initial full-file verification is performed by the mandatory database
		// comparison below. The closing full scan remains a mutation guard.
		session, closeSession, sessionErr := files.session(ctx, m)
		if sessionErr != nil {
			return sessionErr
		}
		defer closeSession()
		s.Target = session
	} else if err := s.Target.Validate(ctx, m); err != nil {
		return err
	}
	b, err := read.NewBudget(s.Limits, s.Clock)
	if err != nil {
		return err
	}
	for _, version := range m.Tables {
		if s.BackgroundTimeout != 0 {
			limits, limitErr := read.TableLimits(version.Rows, s.BackgroundTimeout)
			if limitErr != nil {
				return limitErr
			}
			b, err = read.NewBudget(limits, s.Clock)
			if err != nil {
				return err
			}
		}
		var table source.Table
		for _, t := range source.Catalog() {
			if t.Name == version.Table {
				table = t
			}
		}
		if err = s.table(ctx, h, m, version, table, b); err != nil {
			return err
		}
	}
	activity.Report(ctx, "validating_target", "", string(h.Position.Instance))
	return s.Target.Validate(ctx, m)
}
func (s StorageChecks) table(ctx context.Context, h read.Handle, m Manifest, v TableVersion, table source.Table, b *read.Budget) (err error) {
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "content_table", Table: v.Table, Combination: h.Combination, Slot: h.Position.Slot, Generation: h.Generation}, string(h.Position.Instance))
	defer func() { end(err) }()
	rows, err := s.Target.Open(ctx, m, v)
	if err != nil {
		return err
	}
	if rows == nil {
		return ErrBuild
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, rows.Close(cleanup))
	}()
	original := newChecked(v, rows)
	advance := activity.Begin(ctx, "content_check", v.Table, string(h.Position.Instance), "rows", "", v.Rows)
	err = read.Scan(ctx, s.Source, h, table.Name, nil, b, func(actual source.SourceRow) error {
		expected, more, err := original.Next(ctx)
		if err != nil {
			return err
		}
		if !more || len(actual) != len(table.Columns) {
			return ErrBuild
		}
		for _, column := range table.Columns {
			raw, ok := actual[column.Name]
			if !ok {
				return ErrBuild
			}
			want, present := expected[column.Name]
			a, err := value.Bind(column.Kind, value.Field{Present: len(raw) > 0, Raw: raw})
			if err != nil {
				return err
			}
			b, err := value.Bind(column.Kind, value.Field{Present: present, Raw: want})
			if err != nil {
				return err
			}
			if column.Kind == value.JSON && a != nil && b != nil {
				// Preserve number lexemes and JSON null; object key order may differ.
				x, err := (ingest.Record{Fields: map[string]ingest.Token{"v": {Raw: raw}}}).Canonical()
				if err != nil {
					return err
				}
				y, err := (ingest.Record{Fields: map[string]ingest.Token{"v": {Raw: want}}}).Canonical()
				if err != nil {
					return err
				}
				if !bytes.Equal(x, y) {
					return ErrBuild
				}
			} else if a != b {
				return ErrBuild
			}
		}
		advance(1, 0)
		return nil
	})
	if err != nil {
		return err
	}
	_, more, err := original.Next(ctx)
	if err != nil {
		return err
	}
	if more || !original.verified {
		return ErrBuild
	}
	return nil
}
func (s StorageChecks) SourceIndexes(ctx context.Context, h read.Handle, m Manifest) (err error) {
	activity.Report(ctx, "index_check", "", string(h.Position.Instance))
	ctx, end := events.TraceBuild(ctx, events.BuildScope{Stage: "index_check", Combination: h.Combination, Slot: h.Position.Slot, Generation: h.Generation}, string(h.Position.Instance))
	defer func() { end(err) }()
	if s.Indexes == nil || m.Validate() != nil || h.Validate() != nil || h.Combination != m.Combination {
		return ErrBuild
	}
	for _, table := range m.Tables {
		if err := s.Indexes.CheckPrimaryIndex(ctx, h.Position, table.Table); err != nil {
			return err
		}
	}
	return nil
}
