package wire

import (
	"fmt"
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
