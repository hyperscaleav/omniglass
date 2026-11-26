package sim

import (
	"fmt"
	"slices"

	"github.com/hyperscaleav/omniglass/sim/profile"
)

// Fault is a state path the device holds off its default: a display powered
// off, an input switched away. Values are text, as the profile's enums are.
type Fault struct {
	Path    string
	Value   string
	Default string
}

// registerSets remembers the state paths a route sets from its body, once each.
func (d *Device) registerSets(sets []profile.Set) {
	for _, s := range sets {
		if !slices.Contains(d.setPaths, s.Path) {
			d.setPaths = append(d.setPaths, s.Path)
		}
	}
}

// Faults is every settable state path the device now holds off its default,
// in the order the profile declares them. A path the profile gives no default
// is never a fault, and a healthy device returns none. This is what the
// console's fault chip reads (#2462): the simulator's own account, so a
// reload shows it.
func (d *Device) Faults() []Fault {
	var out []Fault
	for _, path := range d.setPaths {
		def, ok := profile.DefaultAt(d.defaults, path)
		if !ok {
			continue
		}
		cur := d.ReadPath(path)
		if cur == nil {
			continue
		}
		if v := fmt.Sprint(cur); v != def {
			out = append(out, Fault{Path: path, Value: v, Default: def})
		}
	}
	return out
}
