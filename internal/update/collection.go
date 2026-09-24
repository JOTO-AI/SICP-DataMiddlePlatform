package update

import (
	"context"

	"github.com/JOTO-AI/SICP-DataMiddlePlatform/internal/storage/control"
)

type CollectionStore interface {
	RetireCollection(context.Context, control.CollectionPlan) (control.Resource, error)
}
type RetiredDeleter interface {
	DeleteRetired(context.Context, control.Resource) error
}
type Collector struct {
	Store CollectionStore
	Files RetiredDeleter
}

func (c Collector) Collect(ctx context.Context, plan control.CollectionPlan) error {
	if c.Store == nil || c.Files == nil {
		return ErrInitialization
	}
	retired, err := c.Store.RetireCollection(ctx, plan)
	if err != nil {
		return err
	}
	if !retired.Retired || retired.ID != plan.Resource.ID || retired.Kind != plan.Resource.Kind || retired.BundleID != plan.Resource.BundleID {
		return control.ErrEntity
	}
	return c.Files.DeleteRetired(ctx, retired)
}
