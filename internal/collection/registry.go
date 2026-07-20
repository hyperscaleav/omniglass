// Package collection holds the node-side collection engine and the pure helpers
// the ingest path shares with it. Registry answers reject-not-project: a
// measurement name lands only if it is a registered, observable property.
package collection

import "github.com/hyperscaleav/omniglass/internal/storage"

// Registry is an immutable snapshot of the observable-property vocabulary, built
// from a ListProperties read. It is pure: no I/O, safe to hold and share.
type Registry struct {
	kinds map[string]string // name -> kind
}

// NewRegistry snapshots the observable properties: those carrying a kind
// (metric/state/log). A declared-only property (nil kind) is not collectable, so
// it is omitted. A later scope-precedence pass refines this; today last write wins
// on name.
func NewRegistry(properties []storage.Property) Registry {
	kinds := make(map[string]string, len(properties))
	for _, p := range properties {
		if p.Kind != nil {
			kinds[p.Name] = *p.Kind
		}
	}
	return Registry{kinds: kinds}
}

// Allows reports whether name is a registered measurement and, if so, its kind.
// An unregistered name is rejected (reject-not-project).
func (r Registry) Allows(name string) (kind string, ok bool) {
	kind, ok = r.kinds[name]
	return kind, ok
}
