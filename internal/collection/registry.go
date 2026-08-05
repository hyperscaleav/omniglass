// Package collection holds the node-side collection engine and the pure helpers
// the ingest path shares with it. Registry answers reject-not-project: a
// measurement name lands only if it is a registered, observable signal.
package collection

import "github.com/hyperscaleav/omniglass/internal/storage"

// Registry is an immutable snapshot of the observable-signal vocabulary, built
// from the three catalog reads (metric_type, property_type, event_type). It is
// pure: no I/O, safe to hold and share.
type Registry struct {
	kinds map[string]string // name -> routing kind (metric/state/event)
	// collisions are names registered in more than one catalog. The catalogs are
	// separate tables with separate uniqueness, so nothing at the schema level
	// stops a name existing in two of them.
	collisions []string
}

// NewRegistry snapshots the collectable vocabulary: the metric lane (every
// metric_type name routes to the metric sink), the property lane (every
// property_type name routes to the state sink; the catalog split of #587 removed
// the kind discriminator, so lane membership IS the routing), and the event types
// (kind "event", the occurrence keyspace, ADR-0063).
//
// A name present in MORE THAN ONE catalog is a collision, and a collision
// resolves to NOTHING. The merge used to apply event types second, so an event
// silently won and a colliding metric name became unwritable: it reached the
// event arm, failed the numeric-value extraction, and vanished with no row, no
// error, and (once the push route existed) a 202 telling the caller it had
// landed. Picking a winner is the bug, so the snapshot refuses the name and
// reports it instead. The data fix belongs upstream, at the create routes; this
// makes the state visible rather than silent.
func NewRegistry(metricTypes []storage.MetricType, propertyTypes []storage.PropertyType, eventTypes []storage.EventType) Registry {
	kinds := make(map[string]string, len(metricTypes)+len(propertyTypes)+len(eventTypes))
	var collisions []string
	for _, mt := range metricTypes {
		kinds[mt.Name] = "metric"
	}
	for _, pt := range propertyTypes {
		if _, dup := kinds[pt.Name]; dup {
			collisions = append(collisions, pt.Name)
			continue
		}
		kinds[pt.Name] = "state"
	}
	for _, et := range eventTypes {
		if _, dup := kinds[et.Name]; dup {
			collisions = append(collisions, et.Name)
			continue
		}
		kinds[et.Name] = "event"
	}
	// Drop every colliding name so no catalog wins it.
	for _, name := range collisions {
		delete(kinds, name)
	}
	return Registry{kinds: kinds, collisions: collisions}
}

// Collisions returns the names registered in more than one catalog, which
// resolve to nothing. Empty in a healthy install; non-empty means an operator
// created a name that already existed in another catalog, and every sample
// carrying it is being refused until one of them is renamed.
func (r Registry) Collisions() []string { return r.collisions }

// Allows reports whether name is a registered measurement and, if so, its kind.
// An unregistered name is rejected (reject-not-project).
func (r Registry) Allows(name string) (kind string, ok bool) {
	kind, ok = r.kinds[name]
	return kind, ok
}
