package bus

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// catalogStore is a store whose catalogs the test sets: a fingerprint and a
// metric and property catalog, so a name can sit in both (a collision).
type catalogStore struct {
	fakeStore
	fp         string
	metrics    []storage.MetricType
	properties []storage.PropertyType
}

func (c *catalogStore) CatalogFingerprint(context.Context) (string, error) { return c.fp, nil }
func (c *catalogStore) ListMetricTypes(context.Context) ([]storage.MetricType, error) {
	return c.metrics, nil
}
func (c *catalogStore) ListPropertyTypes(context.Context) ([]storage.PropertyType, error) {
	return c.properties, nil
}
func (c *catalogStore) ListEventTypes(context.Context) ([]storage.EventType, error) {
	return nil, nil
}

// TestACollisionIsLoggedOnRebuildAndHourlyNotPerBatch is #1298: a registry
// name collision is logged when the registry is built with it, and again once
// an hour while it persists, never on every batch; once it is cleared, nothing
// more is logged.
func TestACollisionIsLoggedOnRebuildAndHourlyNotPerBatch(t *testing.T) {
	var logged bytes.Buffer
	now := time.Date(2026, 10, 28, 4, 0, 0, 0, time.UTC)
	cache := &registryCache{
		now: func() time.Time { return now },
		log: slog.New(slog.NewTextHandler(&logged, nil)),
	}
	store := &catalogStore{fp: "a",
		metrics:    []storage.MetricType{{Name: "input"}},
		properties: []storage.PropertyType{{Name: "input"}, {Name: "power"}},
	}
	warnings := func() int { return strings.Count(logged.String(), "registry name collision") }
	batch := func(after time.Duration) {
		t.Helper()
		now = now.Add(after)
		if _, err := cache.get(context.Background(), store); err != nil {
			t.Fatal(err)
		}
	}

	batch(0)
	if warnings() != 1 || !strings.Contains(logged.String(), "input") {
		t.Fatalf("building a registry with a collision: %d warnings, want 1 naming input:\n%s", warnings(), logged.String())
	}
	for i := 0; i < 100; i++ {
		batch(2 * time.Second)
	}
	if warnings() != 1 {
		t.Fatalf("100 batches while the collision held: %d warnings, want still 1", warnings())
	}
	batch(time.Hour)
	if warnings() != 2 {
		t.Fatalf("an hour on: %d warnings, want the reminder (2)", warnings())
	}
	batch(2 * time.Second)
	if warnings() != 2 {
		t.Fatalf("the batch after the reminder: %d warnings, want still 2", warnings())
	}

	// Any rebuild with the collision standing logs it at once, inside the
	// reminder's hour: a catalog change elsewhere (a new metric type) is when
	// an operator is editing the catalogs, so they hear it is still there.
	store.fp, store.metrics = "a2", append(store.metrics, storage.MetricType{Name: "volume"})
	batch(2 * time.Second)
	if warnings() != 3 {
		t.Fatalf("a rebuild with the collision standing: %d warnings, want it logged at once (3)", warnings())
	}

	// Deleting one side clears it: the registry rebuilds without a collision
	// and nothing more is logged, however long it runs.
	store.fp, store.properties = "b", []storage.PropertyType{{Name: "power"}}
	batch(2 * time.Second)
	batch(3 * time.Hour)
	if warnings() != 3 {
		t.Fatalf("after the collision was cleared: %d warnings, want still 3", warnings())
	}
}

// TestTheConsumerLogsACollisionOnceAcrossBatches holds the warning out of the
// per-batch path: the ingest consumer lands 100 batches carrying samples while
// a collision stands and logs it once, when its registry is built.
func TestTheConsumerLogsACollisionOnceAcrossBatches(t *testing.T) {
	var logged bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(prior)

	s := &Server{store: &catalogStore{fp: "a",
		metrics:    []storage.MetricType{{Name: "input"}, {Name: "volume"}},
		properties: []storage.PropertyType{{Name: "input"}},
	}}
	owner := &storage.TaskOwner{ComponentID: "c-1"}
	for i := 0; i < 100; i++ {
		ev := &ogv1.TelemetryBatch{TaskId: "t-1", NodeId: "node-a",
			Metrics: []*ogv1.MetricSample{{Name: "volume", Value: float64(i)}}}
		if err := s.land(context.Background(), "", ev, ingestBinding{SampleOwner: owner, Arc: componentArc(*owner),
			Writer: storage.NodeWriter(owner.ComponentID, "")}); err != nil {
			t.Fatalf("batch %d: %v", i, err)
		}
	}
	if n := strings.Count(logged.String(), "registry name collision"); n != 1 {
		t.Fatalf("100 batches while a collision stood logged it %d times, want once", n)
	}
}
