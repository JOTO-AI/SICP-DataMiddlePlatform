package storage

import (
	"context"
	"time"
)

// VerifiedLogs checks only the selected backend, independently of the health of
// any other target. Missing schema, absent attestation and changed physical
// identity all fail closed. Every transaction (including after reconnect) checks
// identity before returning an executor to a reader/writer.
type VerifiedLogs struct {
	Registry Registry
	Database Database
}

func (v VerifiedLogs) Begin(ctx context.Context, c Connection) (Transaction, error) {
	if v.Database == nil || v.Registry.LogMode() != DualLogs {
		return nil, ErrLogIdentity
	}
	var source LogSource
	for _, s := range []LogSource{PlatformLog, UpstreamLog} {
		target, e := v.Registry.LogConnection(s)
		if e == nil && target == c {
			source = s
		}
	}
	if source == "" {
		return nil, ErrLogIdentity
	}
	tx, err := v.Database.Begin(ctx, c)
	if err != nil {
		return nil, ErrLogIdentity
	}
	var valid bool
	if err = tx.QueryRow(ctx, "SELECT logs.verify_deployment($1,$2)", string(source), v.Registry.LogTopology()).Scan(&valid); err != nil || !valid || ctx.Err() != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
		return nil, ErrLogIdentity
	}
	return tx, nil
}

func (v VerifiedLogs) Ready(ctx context.Context, source LogSource) bool {
	c, err := v.Registry.LogConnection(source)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, err := v.Begin(ctx, c)
	if err != nil {
		return false
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer stop()
	return tx.Rollback(cleanup) == nil && ctx.Err() == nil
}
