package storage

import "context"

// Row.Scan preserves SQL NULL through nullable destinations supplied by callers.
type Row interface{ Scan(...any) error }

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
