// Package source stores only the allowlisted source columns in business slots.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	first := true
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
				bytesRead += len(raw)
			}
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
		if err := writeBatch(ctx, r.Database, connection, table, batch, first); err != nil {
			return report, err
		}
		report.CompletedBatches++
		report.WrittenRows += len(batch)
		first = false
		if done {
			report.Complete = true
			return report, nil
		}
	}
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

func writeBatch(ctx context.Context, db storage.Database, connection storage.Connection, table Table, batch [][]any, truncate bool) error {
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
	table, ok := tables[tableName]
	if !ok || limit < 1 || limit > MaxBatchRows {
		return nil, ErrInput
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
	sql := "SELECT " + strings.Join(columns, ",") + " FROM source." + table.Name + ` WHERE "` + table.PrimaryKey + `" COLLATE "C" > $1 ORDER BY "` + table.PrimaryKey + `" COLLATE "C" LIMIT $2`
	rows, err := reader.Query(ctx, sql, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result = make([]SourceRow, 0, limit)
	bytesRead := 0
	for rows.Next() {
		if len(result) >= limit {
			return nil, errors.New("source read limit exceeded")
		}
		values := make([]any, len(table.Columns))
		destinations := make([]any, len(values))
		for i := range values {
			destinations[i] = &values[i]
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
	}
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
