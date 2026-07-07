// Package collection holds the node-side collection engine and the pure helpers
// the ingest path shares with it. Registry answers reject-not-project: a
// measurement name lands only if it is a registered datapoint_type.
package collection

import "github.com/hyperscaleav/omniglass/internal/storage"

// Registry is an immutable snapshot of the datapoint_type vocabulary, built from
// a ListDatapointTypes read. It is pure: no I/O, safe to hold and share.
type Registry struct {
	kinds map[string]string // name -> kind
}

// NewRegistry snapshots the registered types. A later scope-precedence pass
// (private shadows official) refines this; slice 1 is official-only, so last
// write wins on name.
func NewRegistry(types []storage.DatapointType) Registry {
	kinds := make(map[string]string, len(types))
	for _, dt := range types {
		kinds[dt.Name] = dt.Kind
	}
	return Registry{kinds: kinds}
}

// Allows reports whether name is a registered measurement and, if so, its kind.
// An unregistered name is rejected (reject-not-project).
func (r Registry) Allows(name string) (kind string, ok bool) {
	kind, ok = r.kinds[name]
	return kind, ok
}
