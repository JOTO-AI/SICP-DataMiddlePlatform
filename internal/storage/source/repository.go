// Package source stores only the allowlisted source columns in business slots.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/activity"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/value"
)

const MaxBatchRows = 1000
const MaxBatchBytes = 8 << 20

type SourceRow map[string]json.RawMessage
type Iterator interface {
	Next(context.Context) (SourceRow, bool, error)
}

type Target struct {
	Position    storage.Position
	Generation  int64
	CandidateID string
}

// BuildState is obtained from locked coordinator state, never from HTTP input.
// A lease excludes route changes, read admission and ownership changes until
// Release. A production gate must not use a timeout alone as proof of draining.
type BuildState struct {
	ActiveInstance storage.Instance
	Initializing   bool
	Generation     int64
	CandidateID    string
	Readers        int64
	AdmissionOpen  bool
	OwnerValid     bool
}
type Lease interface {
	State() BuildState
	Release(context.Context) error
}
type Gate interface {
	LockBuild(context.Context, Target) (Lease, error)
}

type Report struct {
	CompletedBatches, WrittenRows int
	Complete                      bool
}
type Repository struct {
	Registry storage.Registry
	Database storage.Database
	Gate     Gate
	UseCopy  bool
}

var ErrTarget = errors.New("target is not an exclusively held inactive candidate")
var ErrInput = errors.New("invalid source table or batch input")

func (r Repository) Rebuild(ctx context.Context, target Target, tableName string, input Iterator, batchSize int) (report Report, err error) {
	table, ok := tables[tableName]
	if !ok || input == nil || batchSize < 1 || batchSize > MaxBatchRows {
		return report, ErrInput
	}
	if target.Position.Validate() != nil || target.Generation < 1 || target.CandidateID == "" || r.Gate == nil || r.Database == nil {
		return report, ErrTarget
	}
	connection, err := r.Registry.Business(target.Position)
	if err != nil {
		return report, err
	}
	lease, err := r.Gate.LockBuild(ctx, target)
	if err != nil {
		return report, err
	}
	if lease == nil {
		return report, ErrTarget
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if releaseErr := lease.Release(cleanup); releaseErr != nil {
			err = errors.Join(err, releaseErr)
			report.Complete = false
		}
	}()
	state := lease.State()
	if (state.ActiveInstance == "" && !state.Initializing) || (state.Initializing && state.ActiveInstance != "") || (state.ActiveInstance != "" && state.ActiveInstance != storage.Main && state.ActiveInstance != storage.Standby) || state.ActiveInstance == target.Position.Instance || state.Generation != target.Generation || state.CandidateID != target.CandidateID || state.Readers != 0 || state.AdmissionOpen || !state.OwnerValid {
		return report, ErrTarget
	}
	defer func() {
		if err == nil && report.Complete {
			err = analyze(ctx, r.Database, connection, table)
			if err != nil {
				report.Complete = false
			}
		}
	}()
	first := true
	var buffered int64
	defer func() { buildmetric.Memory(ctx, -buffered) }()
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		batch := make([][]any, 0, batchSize)
		bytesRead := 0
		done := false
		for len(batch) < batchSize {
			row, more, err := input.Next(ctx)
			if err != nil {
				return report, err
			}
			if !more {
				done = true
				break
			}
			for _, raw := range row {
				if len(raw) > MaxBatchBytes-bytesRead {
					return report, ErrInput
				}
				bytesRead += len(raw)
			}
			buildmetric.Memory(ctx, int64(bytesRead)-buffered)
			buffered = int64(bytesRead)
			if bytesRead > MaxBatchBytes {
				return report, ErrInput
			}
			parameters, err := bindRow(table, row)
			if err != nil {
				return report, err
			}
			batch = append(batch, parameters)
		}
		if len(batch) == 0 && !first {
			report.Complete = true
			return report, nil
		}
		if err := writeBatch(ctx, r.Database, connection, table, batch, first, r.UseCopy); err != nil {
			return report, err
		}
		report.CompletedBatches++
		report.WrittenRows += len(batch)
		activity.Advance(ctx, int64(len(batch)), 1)
		buildmetric.Add(ctx, buildmetric.Batches, 1)
		buildmetric.Add(ctx, buildmetric.RowsWritten, int64(len(batch)))
		buildmetric.Add(ctx, buildmetric.BytesWritten, int64(bytesRead))
		buildmetric.Memory(ctx, -buffered)
		buffered = 0
		first = false
		if done {
			report.Complete = true
			return report, nil
		}
	}
}

func analyze(ctx context.Context, db storage.Database, connection storage.Connection, table Table) error {
	tx, err := db.Begin(ctx, connection)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var allowed bool
	if err = tx.QueryRow(ctx, "SELECT has_table_privilege($1,'MAINTAIN')", "source."+table.Name).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrTarget
	} // ANALYZE alone would silently skip it.
	if _, err = tx.Exec(ctx, "ANALYZE source."+table.Name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func bindRow(table Table, row SourceRow) ([]any, error) {
	allowed := make(map[string]bool, len(table.Columns))
	parameters := make([]any, len(table.Columns))
	for i, column := range table.Columns {
		allowed[column.Name] = true
		raw, present := row[column.Name]
		bound, err := value.Bind(column.Kind, value.Field{Present: present, Raw: raw})
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", column.Name, err)
		}
		if column.Name == table.PrimaryKey {
			key, ok := bound.(string)
			if !ok || strings.TrimSpace(key) == "" {
				return nil, ErrInput
			}
		}
		parameters[i] = bound
	}
	for key := range row {
		if !allowed[key] {
			return nil, ErrInput
		}
	}
	return parameters, nil
}

func writeBatch(ctx context.Context, db storage.Database, connection storage.Connection, table Table, batch [][]any, truncate, useCopy bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx, connection)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	if truncate {
		if _, err := tx.Exec(ctx, "TRUNCATE TABLE source."+table.Name); err != nil {
			return err
		}
	}
	if len(batch) > 0 {
		if useCopy {
			copier, ok := tx.(storage.CopyExecutor)
			if !ok {
				return errors.New("source COPY adapter unavailable")
			}
			columns := make([]string, len(table.Columns))
			for i, column := range table.Columns {
				columns[i] = column.Name
			}
			count, err := copier.CopyFrom(ctx, table.Name, columns, batch)
			if err != nil {
				return err
			}
			if count != int64(len(batch)) {
				return errors.New("source COPY row count mismatch")
			}
		} else {
			columns := make([]string, len(table.Columns))
			for i, column := range table.Columns {
				columns[i] = `"` + column.Name + `"`
			}
			groups := make([]string, len(batch))
			parameters := make([]any, 0, len(batch)*len(columns))
			for i, row := range batch {
				placeholders := make([]string, len(row))
				for j, v := range row {
					parameters = append(parameters, v)
					placeholders[j] = fmt.Sprintf("$%d", len(parameters))
				}
				groups[i] = "(" + strings.Join(placeholders, ",") + ")"
			}
			// Rebuilds reject duplicate keys. Incremental merging happens in P04,
			// before this complete-candidate writer, so no implicit upsert is allowed.
			sql := "INSERT INTO source." + table.Name + " (" + strings.Join(columns, ",") + ") VALUES " + strings.Join(groups, ",")
			count, err := tx.Exec(ctx, sql, parameters...)
			if err != nil {
				return err
			}
			if count != int64(len(batch)) {
				return errors.New("source batch row count mismatch")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rollback(ctx context.Context, tx storage.Transaction) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

// ReadBatch is for pinned slot storage access. Runtime query admission and the
// generation/bundle handle are supplied by P06/P11 before calling this method.
// SQL NULL maps to nil RawMessage, not a fabricated source key-presence claim.
func (r Repository) ReadBatch(ctx context.Context, position storage.Position, tableName, after string, limit int) (result []SourceRow, err error) {
	return r.ReadMatching(ctx, position, tableName, after, limit, nil)
}

// Filter selects an allowlisted TEXT column; values are always bound. Contains
// means literal, case-sensitive substring, so %, _ and backslash are ordinary.
type Filter struct {
	Column   string
	Value    string
	Contains bool
	// Values selects exact membership when non-nil. It cannot be combined with
	// Value or Contains; an empty set is rejected instead of removing a filter.
	Values []string
}

func (r Repository) ReadMatching(ctx context.Context, position storage.Position, tableName, after string, limit int, filters []Filter) (result []SourceRow, err error) {
	table, ok := tables[tableName]
	if !ok || limit < 1 || limit > MaxBatchRows || len(filters) > 32 {
		return nil, ErrInput
	}
	// Validate structure before acquiring any database resource.
	conditions := []string{`"` + table.PrimaryKey + `" COLLATE "C" > $1`}
	args := []any{after, limit}
	for _, filter := range filters {
		allowed := false
		for _, column := range table.Columns {
			if column.Name == filter.Column && column.Kind == value.Text {
				allowed = true
				break
			}
		}
		if !allowed || len(filter.Value) > 4096 || strings.ContainsRune(filter.Value, 0) {
			return nil, ErrInput
		}
		if filter.Values != nil {
			if len(filter.Values) == 0 || len(filter.Values) > MaxBatchRows || filter.Value != "" || filter.Contains {
				return nil, ErrInput
			}
			for _, item := range filter.Values {
				if len(item) > 4096 || strings.ContainsRune(item, 0) {
					return nil, ErrInput
				}
			}
			args = append(args, append([]string(nil), filter.Values...))
			conditions = append(conditions, fmt.Sprintf(`"%s" COLLATE "C" = ANY($%d::text[])`, filter.Column, len(args)))
			continue
		}
		args = append(args, filter.Value)
		column := `"` + filter.Column + `"`
		if filter.Contains {
			conditions = append(conditions, fmt.Sprintf("strpos(%s, $%d) > 0", column, len(args)))
		} else {
			conditions = append(conditions, fmt.Sprintf(`%s COLLATE "C" = $%d`, column, len(args)))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	connection, err := r.Registry.Business(position)
	if err != nil {
		return nil, err
	}
	if r.Database == nil {
		return nil, ErrInput
	}
	tx, err := r.Database.Begin(ctx, connection)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	reader, ok := tx.(storage.QueryExecutor)
	if !ok {
		return nil, errors.New("database adapter does not support source reads")
	}
	columns := make([]string, len(table.Columns))
	for i, column := range table.Columns {
		columns[i] = `"` + column.Name + `"`
		if column.Kind == value.JSON {
			columns[i] += "::text"
		}
	}
	sql := "SELECT " + strings.Join(columns, ",") + " FROM source." + table.Name + " WHERE " + strings.Join(conditions, " AND ") + ` ORDER BY "` + table.PrimaryKey + `" COLLATE "C" LIMIT $2`
	rows, err := reader.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result = make([]SourceRow, 0, limit)
	bytesRead := 0
	values := make([]any, len(table.Columns))
	destinations := make([]any, len(values))
	for i := range values {
		destinations[i] = &values[i]
	}
	for rows.Next() {
		if len(result) >= limit {
			return nil, errors.New("source read limit exceeded")
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		row := SourceRow{}
		for i, column := range table.Columns {
			raw, err := value.Restore(column.Kind, values[i])
			if err != nil {
				return nil, err
			}
			bytesRead += len(raw)
			if bytesRead > MaxBatchBytes {
				return nil, errors.New("source read byte budget exceeded")
			}
			row[column.Name] = raw
		}
		result = append(result, row)
		buildmetric.Add(ctx, buildmetric.RowsRead, 1)
	}
	buildmetric.Add(ctx, buildmetric.BytesRead, int64(bytesRead))
	buildmetric.Add(ctx, buildmetric.Batches, 1)
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
