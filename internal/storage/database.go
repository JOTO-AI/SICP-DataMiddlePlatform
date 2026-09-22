package storage

import "context"

// Row.Scan preserves SQL NULL through nullable destinations supplied by callers.
type Row interface{ Scan(...any) error }

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

// QueryExecutor adds streaming reads to an executor without requiring migration
// or control-only substitutes to implement unused row iteration methods.
type QueryExecutor interface {
	Query(context.Context, string, ...any) (Rows, error)
}

type Executor interface {
	Exec(context.Context, string, ...any) (int64, error)
	QueryRow(context.Context, string, ...any) Row
}

type Transaction interface {
	Executor
	Commit(context.Context) error
	Rollback(context.Context) error
}

// Database always receives an explicit, validated connection descriptor.
type Database interface {
	Begin(context.Context, Connection) (Transaction, error)
}
