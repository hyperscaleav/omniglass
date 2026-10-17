package sim

import (
	"fmt"
	"strconv"
	"strings"
)

// SetPath writes value into the JSON tree rooted at root at a dotted
// path. Numeric segments index into arrays; non-numeric segments index
// into maps. Intermediate maps are created as needed so callers don't
// have to pre-declare every container. Array segments error if out of
// range so a bad index fails loudly instead of silently growing the
// slice with zero values.
//
// This is the single path-write primitive shared by local mutations
// (applying a route's mutate block to local state) and externally
// applied diffs (applyExternalDiffs). Keeping one implementation keeps
// the two paths in sync: a write that succeeds locally also succeeds
// when applied externally, and vice versa.
func SetPath(root map[string]any, path string, value any) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	segs := strings.Split(path, ".")
	if len(segs) == 1 {
		root[segs[0]] = value
		return nil
	}

	var cursor any = root
	for i := 0; i < len(segs)-1; i++ {
		next, err := stepOrCreate(cursor, segs[i])
		if err != nil {
			return fmt.Errorf("segment %d (%q): %w", i, segs[i], err)
		}
		cursor = next
	}
	return assign(cursor, segs[len(segs)-1], value)
}

// stepOrCreate returns (creating if necessary) the container at seg
// inside parent. Missing map keys are filled with empty maps; missing
// array indices are an error because silently growing a slice with zero
// values masks indexing bugs.
func stepOrCreate(parent any, seg string) (any, error) {
	switch p := parent.(type) {
	case map[string]any:
		v, ok := p[seg]
		if !ok {
			fresh := map[string]any{}
			p[seg] = fresh
			return fresh, nil
		}
		return v, nil
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer index", seg)
		}
		if idx < 0 || idx >= len(p) {
			return nil, fmt.Errorf("index %d out of range [0,%d)", idx, len(p))
		}
		return p[idx], nil
	default:
		return nil, fmt.Errorf("cannot descend through %T", parent)
	}
}

func assign(container any, seg string, value any) error {
	switch c := container.(type) {
	case map[string]any:
		c[seg] = value
		return nil
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return fmt.Errorf("%q is not an integer index", seg)
		}
		if idx < 0 || idx >= len(c) {
			return fmt.Errorf("index %d out of range [0,%d)", idx, len(c))
		}
		c[idx] = value
		return nil
	default:
		return fmt.Errorf("cannot assign into %T", container)
	}
}
