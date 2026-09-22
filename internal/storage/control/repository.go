package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Repository struct {
	Registry storage.Registry
	Database storage.Database
}

var ErrEntity = errors.New("invalid control entity")
var ErrMissing = errors.New("control entity not found")

type definition struct {
	table string
	value reflect.Value
}

// Only these concrete types may contribute identifiers. Tags are compiled
// constants; arbitrary structs, SQL names and embedded lookalikes are rejected.
func describe(entity any) (definition, error) {
	table := ""
	switch entity.(type) {
	case *Task:
		table = "tasks"
	case *Step:
		table = "steps"
	case *Checkpoint:
		table = "checkpoints"
	case *Schedule:
		table = "schedule"
	case *Execution:
		table = "execution"
	case *Bundle:
		table = "bundles"
	case *Slot:
		table = "slots"
	case *Candidate:
		table = "candidates"
	case *Progress:
		table = "source_progress"
	case *Publication:
		table = "publication_outbox"
	case *Notification:
		table = "notification_outbox"
	case *Reader:
		table = "readers"
	default:
		return definition{}, ErrEntity
	}
	v := reflect.ValueOf(entity)
	if v.IsNil() {
		return definition{}, ErrEntity
	}
	return definition{table: table, value: v.Elem()}, nil
}

func tag(d definition, i int) (string, string) {
	p := strings.Split(d.value.Type().Field(i).Tag.Get("db"), ",")
	if len(p) == 1 {
		return p[0], ""
	}
	return p[0], p[1]
}

func parameter(v reflect.Value) (any, error) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	if document, ok := v.Interface().(Document); ok {
		if !json.Valid([]byte(document)) {
			return nil, ErrEntity
		}
		return string(document), nil
	}
	return v.Interface(), nil
}

func keys(d definition, offset int) ([]string, []any, error) {
	var predicates []string
	var values []any
	for i := 0; i < d.value.NumField(); i++ {
		name, role := tag(d, i)
		if role != "key" {
			continue
		}
		v := d.value.Field(i)
		if (v.Kind() == reflect.String && v.String() == "") || (v.Kind() == reflect.Bool && !v.Bool()) {
			return nil, nil, ErrEntity
		}
		value, err := parameter(v)
		if err != nil {
			return nil, nil, err
		}
		values = append(values, value)
		predicates = append(predicates, fmt.Sprintf(`"%s"=$%d`, name, offset+len(values)))
	}
	return predicates, values, nil
}

// transaction confines every entity operation to the fixed control connection.
// Lifecycle transitions/idempotency/fencing are added in P02.06/P02.07 rather
// than inferred by this mapping layer from arbitrary entity status strings.
func (r Repository) transaction(ctx context.Context, fn func(storage.Transaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := r.Registry.Control()
	if c.DSN() == "" || r.Database == nil {
		return ErrEntity
	}
	tx, err := r.Database.Begin(ctx, c)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Insert creates a new fact; it never silently replaces an existing row.
func (r Repository) Insert(ctx context.Context, entity any) error {
	d, err := describe(entity)
	if err != nil {
		return err
	}
	if _, _, err := keys(d, 0); err != nil {
		return err
	}
	var columns, placeholders []string
	var values []any
	generatedIndex := -1
	for i := 0; i < d.value.NumField(); i++ {
		name, role := tag(d, i)
		if role == "generated" {
			if d.value.Field(i).Int() != 0 {
				return ErrEntity
			}
			generatedIndex = i
			continue
		}
		v, err := parameter(d.value.Field(i))
		if err != nil {
			return err
		}
		columns = append(columns, `"`+name+`"`)
		values = append(values, v)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(values)))
	}
	sql := "INSERT INTO control." + d.table + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	var generated int64
	err = r.transaction(ctx, func(tx storage.Transaction) error {
		if generatedIndex >= 0 {
			name, _ := tag(d, generatedIndex)
			return tx.QueryRow(ctx, sql+` RETURNING "`+name+`"`, values...).Scan(&generated)
		}
		count, err := tx.Exec(ctx, sql, values...)
		if err != nil {
			return err
		}
		if count != 1 {
			return errors.New("control insert count mismatch")
		}
		return nil
	})
	if err == nil && generatedIndex >= 0 {
		d.value.Field(generatedIndex).SetInt(generated)
	}
	return err
}

// Update changes one existing identity. It does not create absent records.
func (r Repository) Update(ctx context.Context, entity any) error {
	d, err := describe(entity)
	if err != nil {
		return err
	}
	var sets []string
	var values []any
	for i := 0; i < d.value.NumField(); i++ {
		name, role := tag(d, i)
		if role == "key" || role == "generated" {
			continue
		}
		v, err := parameter(d.value.Field(i))
		if err != nil {
			return err
		}
		values = append(values, v)
		sets = append(sets, fmt.Sprintf(`"%s"=$%d`, name, len(values)))
	}
	where, identity, err := keys(d, len(values))
	if err != nil {
		return err
	}
	values = append(values, identity...)
	return r.transaction(ctx, func(tx storage.Transaction) error {
		count, err := tx.Exec(ctx, "UPDATE control."+d.table+" SET "+strings.Join(sets, ",")+" WHERE "+strings.Join(where, " AND "), values...)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrMissing
		}
		return nil
	})
}

// Load uses the destination's identity and replaces it only after a successful
// read transaction. A scan/commit failure cannot leave a partial entity behind.
func (r Repository) Load(ctx context.Context, destination any) error {
	d, err := describe(destination)
	if err != nil {
		return err
	}
	where, identity, err := keys(d, 0)
	if err != nil {
		return err
	}
	loaded := reflect.New(d.value.Type()).Elem()
	var columns []string
	var destinations []any
	for i := 0; i < d.value.NumField(); i++ {
		name, _ := tag(d, i)
		column := `"` + name + `"`
		t := d.value.Field(i).Type()
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == reflect.TypeOf(Document("")) {
			column += "::text"
		}
		columns = append(columns, column)
		destinations = append(destinations, loaded.Field(i).Addr().Interface())
	}
	err = r.transaction(ctx, func(tx storage.Transaction) error {
		return tx.QueryRow(ctx, "SELECT "+strings.Join(columns, ",")+" FROM control."+d.table+" WHERE "+strings.Join(where, " AND "), identity...).Scan(destinations...)
	})
	if err == nil {
		d.value.Set(loaded)
	}
	return err
}
