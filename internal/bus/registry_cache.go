package bus

import (
	"context"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// registryCache keeps the ingest sample registry between batches (#1279).
// Building it reads all three catalogs; a batch instead reads the catalogs'
// fingerprint (storage.CatalogFingerprint, one short value) and reuses the
// registry while the fingerprint holds, so a catalog change is honored by the
// next batch and an unchanged catalog costs one small read.
//
// The fingerprint is read before the catalogs, so the registry stored under a
// fingerprint is never older than it: a catalog change landing between the
// two reads only makes the next batch see a different fingerprint and rebuild.
// An empty fingerprint (a stub store) is never cached.
type registryCache struct {
	mu          sync.Mutex
	fingerprint string
	reg         collection.Registry
}

func (c *registryCache) get(ctx context.Context, store Store) (collection.Registry, error) {
	fp, err := store.CatalogFingerprint(ctx)
	if err != nil {
		return collection.Registry{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if fp != "" && fp == c.fingerprint {
		return c.reg, nil
	}
	metricTypes, err := store.ListMetricTypes(ctx)
	if err != nil {
		return collection.Registry{}, err
	}
	properties, err := store.ListPropertyTypes(ctx)
	if err != nil {
		return collection.Registry{}, err
	}
	eventTypes, err := store.ListEventTypes(ctx)
	if err != nil {
		return collection.Registry{}, err
	}
	c.reg = collection.NewRegistry(metricTypes, properties, eventTypes)
	c.fingerprint = fp
	return c.reg, nil
}
