// Package adapters owns network transports. Errors deliberately omit addresses,
// SQL parameters, driver details and provider response bodies.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPostgres = errors.New("PostgreSQL operation failed")

type Postgres struct {
	pools map[storage.Connection]*pgxpool.Pool
	order []*pgxpool.Pool
}

func (*Postgres) String() string     { return "[PostgreSQL adapter redacted]" }
func (p *Postgres) GoString() string { return p.String() }

func OpenPostgres(ctx context.Context, r storage.Registry, timeout time.Duration) (*Postgres, error) {
	p := &Postgres{pools: map[storage.Connection]*pgxpool.Pool{}}
	connections := []storage.Connection{}
	for _, i := range []storage.Instance{storage.Main, storage.Standby} {
		for s := 0; s < 2; s++ {
			c, e := r.Business(storage.Position{Instance: i, Slot: s})
			if e != nil {
				return p, ErrPostgres
			}
			connections = append(connections, c)
		}
	}
	connections = append(connections, r.Control(), r.Logs())
	configs := make([]*pgxpool.Config, 0, len(connections))
	for _, c := range connections {
		cfg, e := pgxpool.ParseConfig(c.DSN())
		if e != nil || c.DSN() == "" {
			return p, ErrPostgres
		}
		cfg.ConnConfig.ConnectTimeout = timeout
		cfg.MaxConns = 16
		cfg.MinConns = 0
		cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
		configs = append(configs, cfg)
	}
	for i, c := range connections {
		cfg := configs[i]
		pool, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			return p, ErrPostgres
		}
		p.pools[c] = pool
		p.order = append(p.order, pool)
		if e = pool.Ping(ctx); e != nil {
			return p, ErrPostgres
		}
	}
	return p, nil
}
func (p *Postgres) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	pool := p.pools[c]
	if pool == nil {
		return nil, ErrPostgres
	}
	tx, e := pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return nil, pgError(ctx, e)
	}
	return pgTransaction{tx}, nil
}
func (p *Postgres) Close(ctx context.Context) error {
	// Pool.Close waits for checked-out SQL. Lifecycle bounds that wait and marks
	// it incomplete; it must never report SQL stopped merely because time elapsed.
	for i := len(p.order) - 1; i >= 0; i-- {
		p.order[i].Close()
	}
	return ctx.Err()
}
func pgError(ctx context.Context, e error) error {
	if e == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrPostgres
}

type pgTransaction struct{ tx pgx.Tx }

func (t pgTransaction) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	tag, e := t.tx.Exec(ctx, q, args...)
	return tag.RowsAffected(), pgError(ctx, e)
}
func (t pgTransaction) QueryRow(ctx context.Context, q string, args ...any) storage.Row {
	return pgRow{ctx, t.tx.QueryRow(ctx, q, args...)}
}
func (t pgTransaction) Query(ctx context.Context, q string, args ...any) (storage.Rows, error) {
	r, e := t.tx.Query(ctx, q, args...)
	if e != nil {
		return nil, pgError(ctx, e)
	}
	return pgRows{ctx, r}, nil
}
func (t pgTransaction) Commit(ctx context.Context) error { return pgError(ctx, t.tx.Commit(ctx)) }
func (t pgTransaction) Rollback(ctx context.Context) error {
	e := t.tx.Rollback(ctx)
	if errors.Is(e, pgx.ErrTxClosed) {
		return nil
	}
	return pgError(ctx, e)
}

type pgRow struct {
	ctx context.Context
	row pgx.Row
}

func (r pgRow) Scan(dst ...any) error { return pgError(r.ctx, r.row.Scan(dst...)) }

type pgRows struct {
	ctx  context.Context
	rows pgx.Rows
}

func (r pgRows) Next() bool            { return r.rows.Next() }
func (r pgRows) Scan(dst ...any) error { return pgError(r.ctx, r.rows.Scan(dst...)) }
func (r pgRows) Err() error            { return pgError(r.ctx, r.rows.Err()) }
func (r pgRows) Close()                { r.rows.Close() }
