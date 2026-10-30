// Package collection holds the node-side collection engine and the pure helpers
// the ingest path shares with it. Registry answers reject-not-project: a
// measurement name lands only if it is a registered, observable signal on the
// lane it arrived on.
package collection

import (
	"slices"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

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
	// collisions are names registered in more than one catalog that nothing
	// can resolve. The catalogs are separate tables with separate uniqueness,
	// so nothing at the schema level stops a name existing in two of them.
	collisions []string
	// shadowed are names an official row holds in one catalog and an
	// operator's row holds in another: the operator's resolves (#1307).
	shadowed []string
}

// NewRegistry snapshots the collectable vocabulary per lane: the metric lane
// (metric_type), the property lane (property_type), and the event lane
// (event_type, the occurrence keyspace, ADR-0063).
//
// A name present in MORE THAN ONE catalog resolves in one case and to NOTHING
// in every other. The one case: exactly one operator's row holds it and the
// others are official. The official row is the platform's and always stays (the
// boot seed upserts every one), but the operator's shadows it on this install,
// so the series the operator already had keep landing where they did, and the
// name is reported as shadowed. Every other case is a collision: two official
// rows is a release bug, two operator rows (or an operator row among several) is
// a create the fence refused, and picking a winner there would silently make the
// loser unwritable, so the snapshot refuses the name and reports it so the state
// is visible. The namespace tiering of #318 (private shadows official) is the
// general form of this rule.
func NewRegistry(metricTypes []storage.MetricType, propertyTypes []storage.PropertyType, eventTypes []storage.EventType) Registry {
	// Each row carries its writer class (ADR-0161). A catalog read resolves
	// it; a row built by hand is resolved here the same way, so the snapshot
	// never holds a classless key. The rows are copied first: the caller's
	// slices are its own.
	metricTypes, propertyTypes, eventTypes = slices.Clone(metricTypes), slices.Clone(propertyTypes), slices.Clone(eventTypes)
	for i := range metricTypes {
		if metricTypes[i].WriterClass == "" {
			metricTypes[i].WriterClass = writerclass.Of(metricTypes[i].Name, metricTypes[i].WrittenBy != nil)
		}
	}
	for i := range propertyTypes {
		if propertyTypes[i].WriterClass == "" {
			propertyTypes[i].WriterClass = writerclass.Of(propertyTypes[i].Name, propertyTypes[i].WrittenBy != nil)
		}
	}
	for i := range eventTypes {
		if eventTypes[i].WriterClass == "" {
			eventTypes[i].WriterClass = writerclass.Of(eventTypes[i].Name, false)
		}
	}
	// A row built by hand names no namespace: it is the platform's when
	// official and the operator's otherwise, as the backfill placed every row
	// (#1431).
	for i := range metricTypes {
		metricTypes[i].Namespace = namespaceOf(metricTypes[i].Namespace, metricTypes[i].Official)
	}
	for i := range propertyTypes {
		propertyTypes[i].Namespace = namespaceOf(propertyTypes[i].Namespace, propertyTypes[i].Official)
	}
	for i := range eventTypes {
		eventTypes[i].Namespace = namespaceOf(eventTypes[i].Namespace, eventTypes[i].Official)
	}

	// Who holds each name: the lane and the namespace.
	holders := map[string][]storage.NameHolder{}
	for _, mt := range metricTypes {
		holders[mt.Name] = append(holders[mt.Name], storage.NameHolder{Lane: "metric", Namespace: mt.Namespace})
	}
	for _, pt := range propertyTypes {
		holders[pt.Name] = append(holders[pt.Name], storage.NameHolder{Lane: "property", Namespace: pt.Namespace})
	}
	for _, et := range eventTypes {
		holders[et.Name] = append(holders[et.Name], storage.NameHolder{Lane: "event", Namespace: et.Namespace})
	}
	// The lane each name resolves on (storage.ResolveNameLane, the one rule every
	// reader of a name shares); none for a collision.
	winner := make(map[string]storage.NameHolder, len(holders))
	var collisions, shadowed []string
	for name, hs := range holders {
		h, isShadowed, ok := storage.ResolveNameLane(name, hs)
		switch {
		case !ok:
			// A name only another namespace holds is no collision: a bare
			// name simply does not reach it.
			if reachable(hs) {
				collisions = append(collisions, name)
			}
		case isShadowed:
			winner[name] = h
			shadowed = append(shadowed, name)
		default:
			winner[name] = h
		}
	}
	slices.Sort(collisions)
	slices.Sort(shadowed)

	metrics := make(map[string]storage.MetricType, len(metricTypes))
	properties := make(map[string]storage.PropertyType, len(propertyTypes))
	events := make(map[string]storage.EventType, len(eventTypes))
	for _, mt := range metricTypes {
		if winner[mt.Name] == (storage.NameHolder{Lane: "metric", Namespace: mt.Namespace}) {
			metrics[mt.Name] = mt
		}
	}
	for _, pt := range propertyTypes {
		if winner[pt.Name] == (storage.NameHolder{Lane: "property", Namespace: pt.Namespace}) {
			properties[pt.Name] = pt
		}
	}
	for _, et := range eventTypes {
		if winner[et.Name] == (storage.NameHolder{Lane: "event", Namespace: et.Namespace}) {
			events[et.Name] = et
		}
	}
	return Registry{metrics: metrics, properties: properties, events: events, collisions: collisions, shadowed: shadowed}
}

// Collisions returns the names registered in more than one catalog that resolve
// to nothing. Empty in a healthy install; non-empty means two operator rows (or
// two official ones) hold a name, and every sample carrying it is being refused
// until one of them is deleted.
func (r Registry) Collisions() []string { return r.collisions }

// Shadowed returns the names an official row holds in one catalog and an
// operator's row holds in another: each resolves to the operator's row, and the
// official one waits until the operator deletes theirs (#1307).
func (r Registry) Shadowed() []string { return r.shadowed }

// IsShadowed reports a name an operator's row shadows an official row of (#1307).
func (r Registry) IsShadowed(name string) bool { return slices.Contains(r.shadowed, name) }

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

// namespaceOf is a row's namespace, derived from official for a row built by
// hand that names none.
func namespaceOf(namespace string, official bool) string {
	switch {
	case namespace != "":
		return namespace
	case official:
		return storage.NamespaceOG
	}
	return storage.NamespaceLocal
}

// reachable reports whether a bare name could reach any of these holders: only
// local and og answer one.
func reachable(hs []storage.NameHolder) bool {
	for _, h := range hs {
		if h.Namespace == storage.NamespaceLocal || h.Namespace == storage.NamespaceOG {
			return true
		}
	}
	return false
}
