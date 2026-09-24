// Package runtimecontrol connects update/candidate interfaces to durable control
// facts. Every mutation uses the same execution and slot locks as publication.
package runtimecontrol

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/candidate"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/ports"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/source"
	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/update"
)

type Store struct {
	Repository  control.Repository
	Owner       control.Ownership
	Clock       ports.Clock
	CodeVersion string
}

func (s Store) repo() control.Repository { return s.Repository.WithOwnership(s.Owner) }
func one(n int64, e error) error {
	if e != nil {
		return e
	}
	if n != 1 {
		return control.ErrEntity
	}
	return nil
}
func document(v any) string { b, _ := json.Marshal(v); return string(b) }
func current(ctx context.Context, tx storage.Transaction) (*control.ActiveMetadata, error) {
	var raw *string
	if e := tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&raw); e != nil {
		return nil, e
	}
	if raw == nil {
		return nil, nil
	}
	a, e := control.DecodeActive(control.Document(*raw))
	return &a, e
}
func noPending(ctx context.Context, tx storage.Transaction) error {
	var pending bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')").Scan(&pending); e != nil {
		return e
	}
	if pending {
		return control.ErrBusy
	}
	return nil
}
func slot(ctx context.Context, tx storage.Transaction, p storage.Position) (control.Slot, error) {
	v := control.Slot{Instance: string(p.Instance), Slot: int16(p.Slot)}
	e := control.ReadFact(ctx, tx, &v)
	return v, e
}
func bundle(ctx context.Context, tx storage.Transaction, id string) (candidate.Manifest, error) {
	v := control.Bundle{ID: id}
	if e := control.ReadFact(ctx, tx, &v); e != nil {
		return candidate.Manifest{}, e
	}
	var m candidate.Manifest
	if json.Unmarshal([]byte(v.Tables), &m) != nil || m.Validate() != nil || m.Combination != id {
		return m, control.ErrEntity
	}
	return m, nil
}
func (s Store) checkpoint(ctx context.Context, tx storage.Transaction, name string, dst any) (bool, error) {
	var raw *string
	if e := tx.QueryRow(ctx, "SELECT (SELECT payload::text FROM control.checkpoints WHERE task_id=$1 AND name=$2)", s.Owner.TaskID, name).Scan(&raw); e != nil {
		return false, e
	}
	if raw == nil {
		return false, nil
	}
	if json.Unmarshal([]byte(*raw), dst) != nil {
		return false, control.ErrEntity
	}
	return true, nil
}
func (s Store) save(ctx context.Context, tx storage.Transaction, name string, v any) error {
	return one(tx.Exec(ctx, "INSERT INTO control.checkpoints (task_id,name,payload,recorded_at) VALUES ($1,$2,$3::jsonb,$4) ON CONFLICT (task_id,name) DO UPDATE SET payload=EXCLUDED.payload,recorded_at=EXCLUDED.recorded_at", s.Owner.TaskID, name, document(v), s.Clock.Now()))
}
func (s Store) saveBundle(ctx context.Context, tx storage.Transaction, m candidate.Manifest) error {
	if m.Validate() != nil {
		return control.ErrEntity
	}
	if _, e := tx.Exec(ctx, "INSERT INTO control.bundles (id,tables,created_at) VALUES ($1,$2::jsonb,$3) ON CONFLICT (id) DO NOTHING", m.Combination, document(m), s.Clock.Now()); e != nil {
		return e
	}
	v, e := bundle(ctx, tx, m.Combination)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(v, m) {
		return control.ErrIdempotency
	}
	return nil
}
func (s Store) task(ctx context.Context, tx storage.Transaction) (control.Task, error) {
	t := control.Task{ID: s.Owner.TaskID}
	e := control.ReadFact(ctx, tx, &t)
	if e != nil {
		return t, e
	}
	if t.Status != "running" || t.OwnerID == nil || *t.OwnerID != s.Owner.OwnerID || t.OwnerGeneration != s.Owner.Generation || t.StartedAt == nil {
		return t, control.ErrOwnership
	}
	return t, nil
}
func planned(ctx context.Context, tx storage.Transaction, t source.Target) (control.Candidate, error) {
	v := control.Candidate{ID: t.CandidateID}
	e := control.ReadFact(ctx, tx, &v)
	if e != nil {
		return v, e
	}
	if v.Instance != string(t.Position.Instance) || int(v.Slot) != t.Position.Slot || v.Generation != t.Generation {
		return v, control.ErrEntity
	}
	return v, nil
}
func (s Store) candidate(ctx context.Context, tx storage.Transaction, t source.Target) (control.Candidate, error) {
	v, e := planned(ctx, tx, t)
	if e != nil {
		return v, e
	}
	if v.TaskID != s.Owner.TaskID || v.OwnerGeneration != s.Owner.Generation {
		return v, control.ErrOwnership
	}
	return v, nil
}
func (s Store) register(ctx context.Context, tx storage.Transaction, t source.Target, m candidate.Manifest) error {
	if _, e := tx.Exec(ctx, "INSERT INTO control.candidates (id,task_id,instance,slot,generation,bundle_id,owner_generation,state) VALUES ($1,$2,$3,$4,$5,$6,$7,'planned') ON CONFLICT (id) DO NOTHING", t.CandidateID, s.Owner.TaskID, string(t.Position.Instance), int16(t.Position.Slot), t.Generation, m.Combination, s.Owner.Generation); e != nil {
		return e
	}
	c, e := s.candidate(ctx, tx, t)
	if e != nil {
		return e
	}
	if c.BundleID != m.Combination {
		return control.ErrIdempotency
	}
	return nil
}
func rollback(ctx context.Context, tx storage.Transaction) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(c)
}

var _ update.InitialStore = Store{}

func (s Store) ConfirmPublication(ctx context.Context, e control.Publication, at time.Time) (control.Publication, error) {
	return s.repo().ConfirmPublication(ctx, e, at)
}
func (s Store) DrainShift(ctx context.Context, e control.Publication) (bool, error) {
	return s.repo().DrainShift(ctx, e)
}
func (s Store) Manifest(ctx context.Context, id string) (out candidate.Manifest, err error) {
	err = s.repo().Fenced(ctx, func(tx storage.Transaction) error { var e error; out, e = bundle(ctx, tx, id); return e })
	if err != nil {
		return candidate.Manifest{}, err
	}
	return
}
