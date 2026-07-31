// Package collection holds the node-side collection engine and the pure helpers
// the ingest path shares with it. Registry answers reject-not-project: a
// measurement name lands only if it is a registered, observable property.
package collection

import "github.com/hyperscaleav/omniglass/internal/storage"

// Registry is an immutable snapshot of the observable-property vocabulary, built
// from a ListPropertyTypes read. It is pure: no I/O, safe to hold and share.
type Registry struct {
	kinds map[string]string // name -> kind
	// collisions are names registered in BOTH property_type and event_type.
	// property_type and event_type are separate tables with separate uniqueness,
	// so nothing at the schema level stops a name existing in both.
	collisions []string
}

// NewRegistry snapshots the collectable vocabulary: the observable properties
// (those carrying a metric/state kind) plus the event types (kind "event", the
// occurrence keyspace, ADR-0063). A declared-only property (nil kind) is not
// collectable, so it is omitted.
//
// A name present in BOTH registries is a collision, and a collision resolves to
// NOTHING. The merge used to apply event types second, so an event silently won
// and a colliding metric name became unwritable: it reached the event arm, failed
// the numeric-value extraction, and vanished with no row, no error, and (once the
// push route existed) a 202 telling the caller it had landed. Picking a winner is
// the bug, so the snapshot refuses the name and reports it instead. The data fix
// belongs upstream, at the create routes; this makes the state visible rather than
// silent.
func NewRegistry(properties []storage.PropertyType, eventTypes []storage.EventType) Registry {
	kinds := make(map[string]string, len(properties)+len(eventTypes))
	for _, p := range properties {
		if p.Kind != nil {
			kinds[p.Name] = *p.Kind
		}
	}
	var collisions []string
	for _, et := range eventTypes {
		if _, dup := kinds[et.Name]; dup {
			collisions = append(collisions, et.Name)
			continue
		}
		kinds[et.Name] = "event"
	}
	// Drop every colliding name so neither registry wins it.
	for _, name := range collisions {
		delete(kinds, name)
	}
	return Registry{kinds: kinds, collisions: collisions}
}

// Collisions returns the names registered in both property_type and event_type,
// which resolve to nothing. Empty in a healthy install; non-empty means an
// operator created a name that already existed in the other registry, and every
// sample carrying it is being refused until one of them is renamed.
func (r Registry) Collisions() []string { return r.collisions }

// Allows reports whether name is a registered measurement and, if so, its kind.
// An unregistered name is rejected (reject-not-project).
func (r Registry) Allows(name string) (kind string, ok bool) {
	kind, ok = r.kinds[name]
	return kind, ok
}
