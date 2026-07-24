package settings

import "fmt"

// Level is one contribution to the cascade: a named layer, its document, and the
// key-paths it locks per namespace. Levels are passed to Resolve broad to specific
// (default, file, platform, then later group, user). "default" is the type's own
// declaration rather than a binding: it is what the setting is when nobody set it,
// and it never appears as a row.
type Level struct {
	Name  string
	Doc   Doc
	Locks map[string][]string
}

// Resolved is the effective settings document plus provenance. Sources maps
// "namespace.key" to the level whose value won; Locks maps "namespace.key" to the
// level that locked it (absent when unlocked).
type Resolved struct {
	Values  Doc
	Sources map[string]string
	Locks   map[string]string
}

// Resolve computes the effective document. Without locks, a more-specific level
// wins (later in the argument list). A lock at level L pins the value contributed
// at or below L and forbids any more-specific level from overriding it; when two
// levels lock the same key, the broader (earlier) lock wins, so a platform lock is
// absolute over a group or user lock. Provenance (Sources) and the winning lock
// level (Locks) are recorded per key.
func Resolve(levels ...Level) Resolved {
	// Lock identity keys on the level name, so names must be unique: a duplicate
	// would let a more-specific level bypass a broader level's lock. Level names are
	// engine constants (default, file, platform, group, user), so a collision is a
	// programming defect, not a runtime condition.
	seen := make(map[string]bool, len(levels))
	for _, lvl := range levels {
		if seen[lvl.Name] {
			panic(fmt.Sprintf("settings: duplicate level name %q in Resolve", lvl.Name))
		}
		seen[lvl.Name] = true
	}
	r := Resolved{
		Values:  Doc{},
		Sources: map[string]string{},
		Locks:   map[string]string{},
	}
	// First pass, broad to specific: record the broadest lock per key (it wins).
	for _, lvl := range levels {
		for ns, keys := range lvl.Locks {
			for _, key := range keys {
				path := fmt.Sprintf("%s.%s", ns, key)
				if _, already := r.Locks[path]; !already {
					r.Locks[path] = lvl.Name
				}
			}
		}
	}
	// Second pass, broad to specific: a level sets a key only if no broader level
	// locked it (a lock freezes the value at the locking level and blocks more-
	// specific writes).
	for _, lvl := range levels {
		for ns, keys := range lvl.Doc {
			if r.Values[ns] == nil {
				r.Values[ns] = map[string]any{}
			}
			for key, val := range keys {
				path := fmt.Sprintf("%s.%s", ns, key)
				if lockLevel, locked := r.Locks[path]; locked && lockLevel != lvl.Name && lockedBefore(levels, lockLevel, lvl.Name) {
					continue // a broader level locked this key; skip this write
				}
				r.Values[ns][key] = val
				r.Sources[path] = lvl.Name
			}
		}
	}
	return r
}

// lockedBefore reports whether lockLevel appears before writeLevel in the level
// order (so the lock is broader and blocks the write).
func lockedBefore(levels []Level, lockLevel, writeLevel string) bool {
	for _, lvl := range levels {
		switch lvl.Name {
		case lockLevel:
			return true
		case writeLevel:
			return false
		}
	}
	return false
}
