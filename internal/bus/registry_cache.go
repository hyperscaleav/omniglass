package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"

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
//
// A name in two catalogs (a collision) resolves to nothing, so every sample
// carrying it is refused. The fence at the create routes stops new ones, but
// an install from before it may hold one, and a release can seed one (#1307);
// either would otherwise lose data quietly, so the
// cache says so: when it builds a registry with a collision, and again every
// collisionReminder while it persists, never on every batch (#1298).
type registryCache struct {
	mu          sync.Mutex
	fingerprint string
	reg         collection.Registry
	// warnedAt is when the registry's collisions were last logged; zero
	// until a registry with one is built.
	warnedAt time.Time
	// now and log default to time.Now and slog.Default; a test sets them.
	now func() time.Time
	log *slog.Logger
}

// collisionReminder is how often a collision that persists is logged again.
const collisionReminder = time.Hour

func (c *registryCache) get(ctx context.Context, store Store) (collection.Registry, error) {
	fp, err := store.CatalogFingerprint(ctx)
	if err != nil {
		return collection.Registry{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if fp != "" && fp == c.fingerprint {
		c.warnCollisions()
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
	c.warnedAt = time.Time{}
	c.warnCollisions()
	return c.reg, nil
}

// warnCollisions logs the registry's collisions if it has any and they were
// not logged within collisionReminder. A rebuilt registry is logged at once.
func (c *registryCache) warnCollisions() {
	clashes := c.reg.Collisions()
	if len(clashes) == 0 {
		return
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	at := now()
	if !c.warnedAt.IsZero() && at.Sub(c.warnedAt) < collisionReminder {
		return
	}
	c.warnedAt = at
	log := slog.Default()
	if c.log != nil {
		log = c.log
	}
	log.Warn("registry name collision: samples using these names are refused until one side is deleted",
		"names", clashes)
}
