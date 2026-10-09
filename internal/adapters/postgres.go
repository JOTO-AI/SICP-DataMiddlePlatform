// Package adapters owns network transports. Errors deliberately omit addresses,
// SQL parameters, driver details and provider response bodies.
package adapters

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/buildmetric"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/query/coord"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPostgres = errors.New("PostgreSQL operation failed")

type Postgres struct {
	pools      map[storage.Connection]*pgxpool.Pool
	order      []*pgxpool.Pool
	mu         sync.Mutex
	executions map[string]execution
}
type execution struct {
	pool *pgxpool.Pool
	pid  uint32
	done bool
}

func (*Postgres) String() string     { return "[PostgreSQL adapter redacted]" }
func (p *Postgres) GoString() string { return p.String() }

// PostgresPoolLimits applies independently to each business-slot pool.
type PostgresPoolLimits struct{ Business, Control, Logs int }

func OpenPostgres(ctx context.Context, r storage.Registry, timeout time.Duration, process ...string) (*Postgres, error) {
	return OpenPostgresWithLimits(ctx, r, timeout, PostgresPoolLimits{16, 16, 16}, process...)
}

func OpenPostgresWithLimits(ctx context.Context, r storage.Registry, timeout time.Duration, limits PostgresPoolLimits, process ...string) (*Postgres, error) {
	for _, limit := range []int{limits.Business, limits.Control, limits.Logs} {
		if limit < 1 || limit > 1000 {
			return nil, ErrPostgres
		}
	}
	p := &Postgres{pools: map[storage.Connection]*pgxpool.Pool{}, executions: map[string]execution{}}
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
	for i, c := range connections {
		limit := limits.Business
		if i == 4 {
			limit = limits.Control
		}
		if i == 5 {
			limit = limits.Logs
		}
		name := ""
		if len(process) == 1 {
			name = process[0]
		}
		cfg, e := postgresPoolConfig(c.DSN(), timeout, limit, name)
		if e != nil {
			return p, e
		}
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

// Session timezone controls server rendering, not pgx's Go scan location.
// Persisted JSON checkpoints use UTC; keep driver values in the same location
// so immutable state comparisons survive SQL and JSON round trips.
func configureTimeCodecs(types *pgtype.Map) {
	types.RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})
	types.RegisterType(&pgtype.Type{Name: "timestamp", OID: pgtype.TimestampOID, Codec: &pgtype.TimestampCodec{ScanLocation: time.UTC}})
}
func (p *Postgres) Begin(ctx context.Context, c storage.Connection) (storage.Transaction, error) {
	pool := p.pools[c]
	if pool == nil {
		return nil, ErrPostgres
	}
	id := coord.ExecutionID(ctx)
	conn, e := pool.Acquire(ctx)
	if e != nil {
		if id != "" {
			p.mu.Lock()
			p.executions[id] = execution{done: true}
			p.mu.Unlock()
		}
		return nil, pgError(ctx, e)
	}
	if id != "" {
		p.mu.Lock()
		p.executions[id] = execution{pool: pool, pid: conn.Conn().PgConn().PID()}
		p.mu.Unlock()
	}
	tx, e := conn.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		conn.Release()
		return nil, pgError(ctx, e)
	}
	once := &sync.Once{}
	return pgTransaction{tx: tx, owner: p, id: id, release: func() { once.Do(conn.Release) }}, nil
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
	if errors.Is(e, pgx.ErrNoRows) {
		return sql.ErrNoRows
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrPostgres
}

type pgTransaction struct {
	tx      pgx.Tx
	owner   *Postgres
	id      string
	release func()
}

func (t pgTransaction) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	buildmetric.Add(ctx, buildmetric.SQL, 1)
	tag, e := t.tx.Exec(ctx, q, args...)
	return tag.RowsAffected(), pgError(ctx, e)
}

func (t pgTransaction) CopyFrom(ctx context.Context, table string, columns []string, rows [][]any) (int64, error) {
	buildmetric.Add(ctx, buildmetric.SQL, 1)
	n, err := t.tx.CopyFrom(ctx, pgx.Identifier{"source", table}, columns, pgx.CopyFromRows(rows))
	return n, pgError(ctx, err)
}
func (t pgTransaction) QueryRow(ctx context.Context, q string, args ...any) storage.Row {
	buildmetric.Add(ctx, buildmetric.SQL, 1)
	return pgRow{ctx, t.tx.QueryRow(ctx, q, args...)}
}
func (t pgTransaction) Query(ctx context.Context, q string, args ...any) (storage.Rows, error) {
	buildmetric.Add(ctx, buildmetric.SQL, 1)
	r, e := t.tx.Query(ctx, q, args...)
	if e != nil {
		return nil, pgError(ctx, e)
	}
	return pgRows{ctx, r}, nil
}
func (t pgTransaction) Commit(ctx context.Context) error {
	if t.release != nil {
		defer t.release()
	}
	e := t.tx.Commit(ctx)
	if e == nil {
		t.ended()
	}
	return pgError(ctx, e)
}
func (t pgTransaction) Rollback(ctx context.Context) error {
	if t.release != nil {
		defer t.release()
	}
	e := t.tx.Rollback(ctx)
	if e == nil {
		t.ended()
	}
	if errors.Is(e, pgx.ErrTxClosed) {
		return nil
	}
	return pgError(ctx, e)
}
func (t pgTransaction) ended() {
	if t.id != "" {
		t.owner.mu.Lock()
		v := t.owner.executions[t.id]
		v.done = true
		t.owner.executions[t.id] = v
		t.owner.mu.Unlock()
	}
}

// Stopped requires a positive transaction acknowledgement or server evidence.
// A canceled HTTP context or an asynchronous connection close is insufficient.
func (p *Postgres) Stopped(ctx context.Context, id string) (bool, error) {
	p.mu.Lock()
	v, ok := p.executions[id]
	p.mu.Unlock()
	if !ok {
		return false, nil
	}
	if v.done {
		return true, nil
	}
	var running bool
	if e := v.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND xact_start IS NOT NULL)", v.pid).Scan(&running); e != nil {
		return false, ErrPostgres
	}
	if !running {
		p.mu.Lock()
		v.done = true
		p.executions[id] = v
		p.mu.Unlock()
	}
	return !running, nil
}
func (p *Postgres) Forget(id string) { p.mu.Lock(); delete(p.executions, id); p.mu.Unlock() }

// ProcessSQLStopped checks every physical pool; absence on only the control
// database would not exclude an old business write still running on S or M.
func (p *Postgres) ProcessSQLStopped(ctx context.Context, process string) (bool, error) {
	for _, pool := range p.order {
		var present bool
		if e := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1)", "sicp-"+process).Scan(&present); e != nil {
			return false, ErrPostgres
		}
		if present {
			return false, nil
		}
	}
	return true, nil
}

func (p *Postgres) OwnSQLStopped(ctx context.Context, process string) (bool, error) {
	for _, pool := range p.order {
		var active bool
		if e := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND pid<>pg_backend_pid() AND xact_start IS NOT NULL)", "sicp-"+process).Scan(&active); e != nil {
			return false, ErrPostgres
		}
		if active {
			return false, nil
		}
	}
	return true, nil
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

// postgresPoolConfig is shared by all six pools; explicit deployment limits
// override pool settings embedded in DSNs.
func postgresPoolConfig(dsn string, timeout time.Duration, maxConns int, process string) (*pgxpool.Config, error) {
	if dsn == "" || maxConns < 1 || maxConns > 1000 {
		return nil, ErrPostgres
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, ErrPostgres
	}
	cfg.ConnConfig.ConnectTimeout = timeout
	cfg.MaxConns = int32(maxConns)
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		configureTimeCodecs(conn.TypeMap())
		return nil
	}
	if process != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = "sicp-" + process
	}
	return cfg, nil
}
