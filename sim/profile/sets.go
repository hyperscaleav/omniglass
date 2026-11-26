package profile

import (
	"fmt"
	"regexp"
	"strings"
)

// Set is a state path an HTTP route sets from one field of its request body,
// to one of a fixed list of values: the profile's own statement that a state
// path can be driven from outside (a display's power, its input, a mic's
// mute). The console offers each value as a quick set (#2462), and the
// simulator reads the same declaration to tell a path driven away from its
// default.
type Set struct {
	// Path is the state path the route writes.
	Path string
	// Field is the request body field its value comes from.
	Field string
	// Values are the values the route accepts, in the profile's order.
	Values []string
}

var bodyFieldRE = regexp.MustCompile(`^\{\{-?\s*\.Request\.Body\.([A-Za-z_][A-Za-z0-9_]*)\s*-?\}\}$`)

// Sets are the quick sets a route declares: each mutation that writes a path
// straight from a body field and lists the values it accepts. A mutation that
// computes its value, takes any value, or sets a constant is not a set.
func (r *Route) Sets() []Set {
	var out []Set
	for _, m := range r.Mutations {
		tmpl, ok := m.Value.(string)
		if !ok || len(m.Enum) == 0 {
			continue
		}
		g := bodyFieldRE.FindStringSubmatch(strings.TrimSpace(tmpl))
		if g == nil {
			continue
		}
		out = append(out, Set{Path: m.Path, Field: g[1], Values: append([]string(nil), m.Enum...)})
	}
	return out
}

// DefaultAt reads the value a dotted state path holds in a defaults tree, as
// text ("on", "true", "38"). It reports false when the path is not there.
func DefaultAt(defaults map[string]any, path string) (string, bool) {
	var cur any = defaults
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[seg]; !ok {
			return "", false
		}
	}
	if cur == nil {
		return "", false
	}
	return fmt.Sprint(cur), true
}
