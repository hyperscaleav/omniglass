package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// registryCache keeps the ingest sample registry between batches (#1279),
// and beside it the property type names some command type targets (#1299),
// which every batch hands LandBatch so the property lane need not read them.
// Building it reads all three catalogs; a batch instead reads the catalogs'
// fingerprint (storage.CatalogFingerprint, one short value) and reuses the
// registry while the fingerprint holds. The fingerprint is read at most once
// every fingerprintEvery (#1928), so an unchanged catalog costs one small read
// every few seconds, however fast batches land. A batch carrying a name the
// kept registry lacks reads it at once, so a type created moments before its
// first sample is honored by that sample; a name the read confirms no catalog
// holds is remembered until the interval's next read, so it forces one extra
// read an interval, not one a batch. Any other catalog change (a type retyped
// or deleted, a calc rule moving a key's writer class, a command type
// targeting a property) is honored within the interval. A failed read is not
// remembered: the next batch reads again.
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
	targeted    map[string]bool
	// warnedAt is when the registry's collisions were last logged; zero
	// until a registry with one is built.
	warnedAt time.Time
	// checkedAt is when the fingerprint was last read and matched or built
	// the registry; a batch inside fingerprintEvery of it reads nothing.
	checkedAt time.Time
	// unknown is each lane-qualified name a read inside the interval found no
	// catalog holds, so it forces no further read until the interval's own.
	unknown map[string]bool
	// now and log default to time.Now and slog.Default; a test sets them.
	now func() time.Time
	log *slog.Logger
}

// clock is c.now, or time.Now when the test set none.
func (c *registryCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// collisionReminder is how often a collision that persists is logged again.
const collisionReminder = time.Hour

// fingerprintEvery is how long a fingerprint read is trusted: a batch inside
// it reuses the kept registry without reading the fingerprint (#1928).
const fingerprintEvery = 5 * time.Second

// get is the registry and the command-targeted property names, as of the
// catalogs at most fingerprintEvery ago, or as of now when one of batches
// names a sample the kept registry lacks.
func (c *registryCache) get(ctx context.Context, store Store, batches ...*ogv1.TelemetryBatch) (collection.Registry, map[string]bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	at := c.clock()
	fresh := c.fingerprint != "" && at.Sub(c.checkedAt) < fingerprintEvery
	if fresh && !c.lacks(batches) {
		c.warnCollisions()
		return c.reg, c.targeted, nil
	}
	fp, err := store.CatalogFingerprint(ctx)
	if err != nil {
		return collection.Registry{}, nil, err
	}
	if fp != "" && fp == c.fingerprint {
		if !fresh {
			c.checkedAt, c.unknown = at, nil
		}
		c.remember(batches)
		c.warnCollisions()
		return c.reg, c.targeted, nil
	}
	metricTypes, err := store.ListMetricTypes(ctx)
	if err != nil {
		return collection.Registry{}, nil, err
	}
	properties, err := store.ListPropertyTypes(ctx)
	if err != nil {
		return collection.Registry{}, nil, err
	}
	eventTypes, err := store.ListEventTypes(ctx)
	if err != nil {
		return collection.Registry{}, nil, err
	}
	targeted, err := store.CommandTargetedProperties(ctx)
	if err != nil {
		return collection.Registry{}, nil, err
	}
	c.reg = collection.NewRegistry(metricTypes, properties, eventTypes)
	c.targeted = targeted
	c.fingerprint = fp
	c.checkedAt, c.unknown = at, nil
	c.remember(batches)
	c.warnedAt = time.Time{}
	c.warnCollisions()
	return c.reg, c.targeted, nil
}

// lacks reports whether batches name a sample the kept registry lacks that no
// read this interval already confirmed missing.
func (c *registryCache) lacks(batches []*ogv1.TelemetryBatch) bool {
	lacking := false
	c.eachMissing(batches, func(k string) bool {
		lacking = !c.unknown[k]
		return !lacking
	})
	return lacking
}

// remember records every name batches carry that the registry lacks, so it
// forces no further read until the interval's own.
func (c *registryCache) remember(batches []*ogv1.TelemetryBatch) {
	c.eachMissing(batches, func(k string) bool {
		if c.unknown == nil {
			c.unknown = map[string]bool{}
		}
		c.unknown[k] = true
		return true
	})
}

// eachMissing calls f with each name batches carry that the registry lacks,
// qualified by its lane, until f returns false.
func (c *registryCache) eachMissing(batches []*ogv1.TelemetryBatch, f func(string) bool) {
	for _, ev := range batches {
		for _, s := range ev.GetMetrics() {
			if _, ok := c.reg.Metric(s.GetName()); !ok && !f("metric\x00"+s.GetName()) {
				return
			}
		}
		for _, s := range ev.GetProperties() {
			if _, ok := c.reg.Property(s.GetName()); !ok && !f("property\x00"+s.GetName()) {
				return
			}
		}
		for _, s := range ev.GetEvents() {
			if _, ok := c.reg.Event(s.GetName()); !ok && !f("event\x00"+s.GetName()) {
				return
			}
		}
	}
}

// warnCollisions logs the registry's collisions and its shadowed names if it
// has any and they were not logged within collisionReminder. A rebuilt registry
// is logged at once. A collision is refused until one side is deleted; a
// shadowed name (#1307) is not refused, since the operator's row resolves, but
// it changes what the name means on this install, so it is said too.
func (c *registryCache) warnCollisions() {
	clashes, shadowed := c.reg.Collisions(), c.reg.Shadowed()
	if len(clashes) == 0 && len(shadowed) == 0 {
		return
	}
	at := c.clock()
	if !c.warnedAt.IsZero() && at.Sub(c.warnedAt) < collisionReminder {
		return
	}
	c.warnedAt = at
	log := slog.Default()
	if c.log != nil {
		log = c.log
	}
	if len(clashes) > 0 {
		log.Warn("registry name collision: samples using these names are refused until one side is deleted",
			"names", clashes)
	}
	if len(shadowed) > 0 {
		log.Warn("a ship-with registry name is shadowed by an operator's row in another catalog: samples using it land on the operator's, and the official row waits until the operator deletes theirs",
			"names", shadowed)
	}
}
