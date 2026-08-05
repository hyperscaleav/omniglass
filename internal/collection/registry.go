// Package collection holds the node-side collection engine and the pure helpers
// the ingest path shares with it. Registry answers reject-not-project: a
// measurement name lands only if it is a registered, observable signal on the
// lane it arrived on.
package collection

import "github.com/hyperscaleav/omniglass/internal/storage"

// Registry is an immutable snapshot of the observable-signal vocabulary, built
// from the three catalog reads (metric_type, property_type, event_type). It is
// pure: no I/O, safe to hold and share.
//
// Since the per-lane wire (#594) the lane IS the routing, so the registry keeps
// no kind strings: it answers membership per catalog, returning the catalog row
// so ingest can validate the sample against it (data_type, validation,
// payload_schema).
type Registry struct {
	metrics    map[string]storage.MetricType
	properties map[string]storage.PropertyType
	events     map[string]storage.EventType
	// collisions are names registered in more than one catalog. The catalogs are
	// separate tables with separate uniqueness, so nothing at the schema level
	// stops a name existing in two of them.
	collisions []string
}

// NewRegistry snapshots the collectable vocabulary per lane: the metric lane
// (metric_type), the property lane (property_type), and the event lane
// (event_type, the occurrence keyspace, ADR-0063).
//
// A name present in MORE THAN ONE catalog is a collision, and a collision
// resolves to NOTHING on any lane. Under the old kind-routed wire, picking a
// winner silently made the loser unwritable; under the per-lane wire it would
// tell two callers with different intents that both names are fine while only
// one table ever sees rows. The data fix belongs upstream, at the create routes;
// the snapshot refuses the name and reports it so the state is visible.
func NewRegistry(metricTypes []storage.MetricType, propertyTypes []storage.PropertyType, eventTypes []storage.EventType) Registry {
	metrics := make(map[string]storage.MetricType, len(metricTypes))
	properties := make(map[string]storage.PropertyType, len(propertyTypes))
	events := make(map[string]storage.EventType, len(eventTypes))
	var collisions []string
	for _, mt := range metricTypes {
		metrics[mt.Name] = mt
	}
	for _, pt := range propertyTypes {
		if _, dup := metrics[pt.Name]; dup {
			collisions = append(collisions, pt.Name)
			continue
		}
		properties[pt.Name] = pt
	}
	for _, et := range eventTypes {
		_, inMetrics := metrics[et.Name]
		_, inProperties := properties[et.Name]
		if inMetrics || inProperties {
			collisions = append(collisions, et.Name)
			continue
		}
		events[et.Name] = et
	}
	// Drop every colliding name so no catalog wins it.
	for _, name := range collisions {
		delete(metrics, name)
		delete(properties, name)
		delete(events, name)
	}
	return Registry{metrics: metrics, properties: properties, events: events, collisions: collisions}
}

// Collisions returns the names registered in more than one catalog, which
// resolve to nothing. Empty in a healthy install; non-empty means an operator
// created a name that already existed in another catalog, and every sample
// carrying it is being refused until one of them is renamed.
func (r Registry) Collisions() []string { return r.collisions }

// Metric resolves name on the metric lane (reject-not-project: false means the
// sample carrying it is refused by name).
func (r Registry) Metric(name string) (storage.MetricType, bool) {
	mt, ok := r.metrics[name]
	return mt, ok
}

// Property resolves name on the property lane, returning the catalog row so the
// caller can validate the value against data_type and the validation schema.
func (r Registry) Property(name string) (storage.PropertyType, bool) {
	pt, ok := r.properties[name]
	return pt, ok
}

// Event resolves name on the event lane, returning the catalog row so the
// caller can validate the payload against payload_schema.
func (r Registry) Event(name string) (storage.EventType, bool) {
	et, ok := r.events[name]
	return et, ok
}
