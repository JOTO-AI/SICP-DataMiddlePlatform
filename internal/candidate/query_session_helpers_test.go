package candidate

import (
	"context"
	"encoding/json"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/read"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
)

// QuerySession lives only inside the caller's fixed candidate/read admission.
// No cached row or empty answer survives closing the validation invocation.
func QuerySession(ctx context.Context, queries Queries, h read.Handle, m Manifest) (Queries, func(), error) {
	if factory, ok := queries.(interface {
		Session(context.Context, read.Handle, Manifest) (Queries, func(), error)
	}); ok {
		return factory.Session(ctx, h, m)
	}
	return queries, func() {}, nil
}

type caseMemo struct {
	gradeIDs     []string
	gradesLoaded bool
}

func (o Online) Session(ctx context.Context, h read.Handle, m Manifest) (Queries, func(), error) {
	if m.Validate() != nil || h.Validate() != nil || h.Combination != m.Combination {
		return nil, nil, ErrBuild
	}
	bound, cancel := context.WithCancel(ctx)
	versions := map[string]string{}
	for _, table := range m.Tables {
		versions[table.Table] = table.Scope + ":" + table.SHA256
	}
	memo := &queryMemo{ctx: bound, source: o.Source, handle: h, versions: versions, rows: map[string][]source.SourceRow{}, limit: 32 << 20}
	o.Source = memo
	o.Grades.Source = memo
	o.Timetable.Source = memo
	o.memo = &caseMemo{}
	o.GroupedStaff = o.Limits.BatchBytes > 0 && o.Timetable.Limits.BatchBytes > 0
	return o, func() {
		cancel()
		buildmetric.Memory(ctx, -memo.used)
		memo.rows = nil
		memo.used = 0
		o.memo.gradeIDs = nil
	}, nil
}

type queryMemo struct {
	ctx         context.Context
	source      read.Matcher
	handle      read.Handle
	versions    map[string]string
	rows        map[string][]source.SourceRow
	used, limit int64
}

func cloneRows(rows []source.SourceRow) []source.SourceRow {
	out := make([]source.SourceRow, len(rows))
	for i, row := range rows {
		out[i] = source.SourceRow{}
		for key, raw := range row {
			out[i][key] = append(json.RawMessage(nil), raw...)
		}
	}
	return out
}
func (m *queryMemo) ReadMatching(ctx context.Context, h read.Handle, table, after string, n int, filters []source.Filter) ([]source.SourceRow, error) {
	if err := m.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	version, ok := m.versions[table]
	if !ok || h != m.handle {
		return nil, read.ErrHandle
	}
	keyRaw, err := json.Marshal(struct {
		Table, Version, After string
		Limit                 int
		Filters               []source.Filter
	}{table, version, after, n, filters})
	if err != nil {
		return nil, err
	}
	key := string(keyRaw)
	if rows, ok := m.rows[key]; ok {
		return cloneRows(rows), nil
	}
	rows, err := m.source.ReadMatching(ctx, h, table, after, n, filters)
	if err != nil {
		return nil, err
	}
	if err = m.ctx.Err(); err != nil {
		return nil, err
	}
	// Small indexed associations only. Large scans never evict useful dimensions
	// or turn the entire database into a process-local cache.
	if len(filters) == 0 || len(rows) > 32 || len(m.rows) >= 100000 {
		return rows, nil
	}
	size := int64(len(key)) + 64
	for _, row := range rows {
		size += 64
		for col, raw := range row {
			size += int64(len(col)+len(raw)) + 32
		}
	}
	if size <= m.limit-m.used {
		m.rows[key] = cloneRows(rows)
		m.used += size
		buildmetric.Memory(m.ctx, size)
	}
	return rows, nil
}
