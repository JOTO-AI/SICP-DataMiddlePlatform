package control

import (
	"context"
	"math"
	"strings"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage"
)

type Resource struct {
	ID, Kind, BundleID string
	Revision           int64
	Retired            bool
}
type CollectionPlan struct{ Resource Resource }

func validResource(r Resource) bool {
	return strings.TrimSpace(r.ID) != "" && len(r.ID) <= 256 && (r.Kind == "seal" || r.Kind == "version") && r.BundleID != ""
}
func loadResource(ctx context.Context, tx storage.Transaction, id string) (Resource, error) {
	r := Resource{ID: id}
	err := tx.QueryRow(ctx, "SELECT kind,bundle_id,revision,retired FROM control.resources WHERE id=$1 FOR UPDATE", id).Scan(&r.Kind, &r.BundleID, &r.Revision, &r.Retired)
	return r, err
}

func (r Repository) RegisterResource(ctx context.Context, resource Resource) error {
	if r.owner == nil || !validResource(resource) || resource.Revision != 0 || resource.Retired {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := r.fenceWriter(ctx, tx); err != nil {
			return err
		}
		if err := lockSnapshots(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO control.resources (id,kind,bundle_id) VALUES ($1,$2,$3) ON CONFLICT (id) DO NOTHING", resource.ID, resource.Kind, resource.BundleID); err != nil {
			return err
		}
		current, err := loadResource(ctx, tx, resource.ID)
		if err != nil {
			return err
		}
		if current.Kind != resource.Kind || current.BundleID != resource.BundleID || current.Retired {
			return ErrIdempotency
		}
		return nil
	})
}

// ResourceReference uses terminal owner tombstones. Retain after release cannot
// revive a delayed backfill/recovery worker's old identity.
func (r Repository) ResourceReference(ctx context.Context, id, owner, kind string, release bool) error {
	if id == "" || strings.TrimSpace(owner) == "" || len(owner) > 256 || (kind != "reader" && kind != "pagination" && kind != "backfill" && kind != "recovery") {
		return ErrEntity
	}
	return r.transaction(ctx, func(tx storage.Transaction) error {
		if err := lockSnapshots(ctx, tx); err != nil {
			return err
		}
		resource, err := loadResource(ctx, tx, id)
		if err != nil {
			return err
		}
		if resource.Retired {
			if release {
				return nil
			}
			return ErrBusy
		}
		if resource.Revision == math.MaxInt64 {
			return ErrBusy
		}
		var changed int64
		if release {
			changed, err = tx.Exec(ctx, "INSERT INTO control.resource_refs (resource_id,owner_id,kind,released) VALUES ($1,$2,$3,TRUE) ON CONFLICT (resource_id,owner_id,kind) DO UPDATE SET released=TRUE WHERE control.resource_refs.released=FALSE", id, owner, kind)
		} else {
			changed, err = tx.Exec(ctx, "INSERT INTO control.resource_refs (resource_id,owner_id,kind) VALUES ($1,$2,$3) ON CONFLICT (resource_id,owner_id,kind) DO NOTHING", id, owner, kind)
			if err == nil {
				var released bool
				err = tx.QueryRow(ctx, "SELECT released FROM control.resource_refs WHERE resource_id=$1 AND owner_id=$2 AND kind=$3", id, owner, kind).Scan(&released)
				if err == nil && released {
					return ErrIdempotency
				}
			}
		}
		if err != nil {
			return err
		}
		if changed < 0 || changed > 1 {
			return ErrEntity
		}
		if changed == 1 {
			return exactlyOne(tx.Exec(ctx, "UPDATE control.resources SET revision=revision+1 WHERE id=$1 AND retired=FALSE", id))
		}
		return nil
	})
}

// Collection uses the snapshot lock and all four slot locks, shared with
// snapshots and reader admission, before inspecting references. Version
// retirement closes old slot admission atomically before physical deletion.
func (r Repository) collectionResource(ctx context.Context, tx storage.Transaction, id string) (Resource, error) {
	if r.owner == nil {
		return Resource{}, ErrOwnership
	}
	if err := r.fenceWriter(ctx, tx); err != nil {
		return Resource{}, err
	}
	if err := lockSnapshots(ctx, tx); err != nil {
		return Resource{}, err
	}
	for _, instance := range []storage.Instance{storage.Main, storage.Standby} {
		for slot := 0; slot < 2; slot++ {
			if err := lockSlot(ctx, tx, storage.Position{Instance: instance, Slot: slot}); err != nil {
				return Resource{}, err
			}
		}
	}
	return loadResource(ctx, tx, id)
}
func collectionEligible(ctx context.Context, tx storage.Transaction, r Resource) error {
	var busy bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM control.resource_refs WHERE resource_id=$1 AND released=FALSE) OR EXISTS (SELECT 1 FROM control.readers WHERE bundle_id=$2 AND (released_at IS NULL OR sql_finished_at IS NULL)) OR EXISTS (SELECT 1 FROM control.query_snapshots WHERE bundle_id=$2 AND expires_at>CURRENT_TIMESTAMP) OR EXISTS (SELECT 1 FROM control.tasks WHERE status NOT IN ('failed','success_published','success_unchanged')) OR EXISTS (SELECT 1 FROM control.publication_outbox WHERE status='pending')", r.ID, r.BundleID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	var raw *string
	if err := tx.QueryRow(ctx, "SELECT (SELECT target::text FROM control.publication_outbox WHERE status='confirmed' ORDER BY sequence DESC LIMIT 1)").Scan(&raw); err != nil {
		return err
	}
	if raw == nil {
		return ErrBusy
	}
	active, err := DecodeActive(Document(*raw))
	if err != nil {
		return err
	}
	if active.Position.BundleID == r.BundleID {
		return ErrBusy
	}
	return nil
}
func (r Repository) PlanCollection(ctx context.Context, id string) (CollectionPlan, error) {
	var out CollectionPlan
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		resource, err := r.collectionResource(ctx, tx, id)
		if err != nil {
			return err
		}
		if !validResource(resource) {
			return ErrEntity
		}
		if !resource.Retired {
			if err = collectionEligible(ctx, tx, resource); err != nil {
				return err
			}
		}
		out = CollectionPlan{Resource: resource}
		return nil
	})
	if err != nil {
		return CollectionPlan{}, err
	}
	return out, nil
}
func (r Repository) RetireCollection(ctx context.Context, plan CollectionPlan) (Resource, error) {
	if !validResource(plan.Resource) {
		return Resource{}, ErrEntity
	}
	var out Resource
	err := r.transaction(ctx, func(tx storage.Transaction) error {
		current, err := r.collectionResource(ctx, tx, plan.Resource.ID)
		if err != nil {
			return err
		}
		if current.Kind != plan.Resource.Kind || current.BundleID != plan.Resource.BundleID {
			return ErrIdempotency
		}
		if current.Retired {
			if current.Revision != plan.Resource.Revision && (plan.Resource.Revision == math.MaxInt64 || current.Revision != plan.Resource.Revision+1) {
				return ErrIdempotency
			}
			out = current
			return nil
		}
		if current.Revision != plan.Resource.Revision || current.Revision == math.MaxInt64 {
			return ErrIdempotency
		}
		if err = collectionEligible(ctx, tx, current); err != nil {
			return err
		}
		if current.Kind == "version" {
			if _, err = tx.Exec(ctx, "UPDATE control.slots SET admission_open=FALSE WHERE bundle_id=$1", current.BundleID); err != nil {
				return err
			}
		}
		if err = exactlyOne(tx.Exec(ctx, "UPDATE control.resources SET retired=TRUE,revision=revision+1 WHERE id=$1 AND revision=$2 AND retired=FALSE", current.ID, current.Revision)); err != nil {
			return err
		}
		current.Retired = true
		current.Revision++
		out = current
		return nil
	})
	if err != nil {
		return Resource{}, err
	}
	return out, nil
}
