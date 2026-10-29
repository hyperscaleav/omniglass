// Package simfleet decides where a simulated device lives and how the
// collector reaches it: a loopback address from the simulated block, and an
// endpoint (a driver attach where the seed knows the driver's inputs, else a
// reachability probe on a port the device serves). The dev seed plans the
// seeded fleet with it, and the console's add-a-device route plans one more.
// Pure: the catalog and the addresses in use come in, the plan goes out.
package simfleet

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// CommunitySecret is the platform secret an SNMP attach references for its
// community string.
const CommunitySecret = "sim-community"

// The platform secrets the http attaches reference (#902): the simulated
// Kestrel bar's basic-auth login (admin / kestrel) and the simulated
// Tessellate sensor's api key, each what the simulator's face demands.
const (
	KestrelLoginSecret = "sim-kestrel-login"
	CensusKeySecret    = "sim-census-key"
	// UCCTokenSecret is the simulated UCC platform's api token (#1282), the
	// one its http api's X-UCC-Token header demands.
	UCCTokenSecret = "sim-ucc-token"
)

// AttachSecret is the platform secret each driver's simulated attach
// references, keyed by driver: an attach is offered only to a caller who can
// read it.
var AttachSecret = map[string]string{
	"snmp-generic":     CommunitySecret,
	"polaris-amp-snmp": CommunitySecret,
	"kestrel-api":      KestrelLoginSecret,
	"http-json":        CensusKeySecret,
	"ucc-rooms":        UCCTokenSecret,
}

// AddressCap is how many addresses the simulated block holds: 256 third
// octets of 250 hosts each.
const AddressCap = 256 * 250

// Address is the i-th address of the simulated block, 127.60.0.1 onward.
func Address(i int) string {
	return fmt.Sprintf("127.60.%d.%d", i/250, i%250+1)
}

// FreeAddress is the lowest address of the block not in used.
func FreeAddress(used map[string]bool) (string, bool) {
	for i := 0; i < AddressCap; i++ {
		if a := Address(i); !used[a] {
			return a, true
		}
	}
	return "", false
}

// Component is a component that may become a simulated device.
type Component struct {
	ID      string
	Product string
}

// Endpoint is one endpoint to create: a driver attach (Driver and Inputs) or
// a bare reachability probe (Transport and Target).
type Endpoint struct {
	Driver    string
	Inputs    map[string]string
	Transport string
	Target    string
}

// Device is a planned simulated device.
type Device struct {
	ComponentID string
	Address     string
	Endpoints   []Endpoint
}

// Catalog is what the plan consults: the profile that simulates a product,
// the driver a product is on, which drivers may be attached here (an SNMP
// attach needs its community secret to exist), and the lift the simulator
// applies to ports below 1024.
type Catalog struct {
	ProfileFor func(product string) (*profile.DeviceProfile, bool)
	DriverOf   func(product string) string
	Attachable func(driver string) bool
	PortOffset int
}

// Plan decides the address and endpoints of each component, taken in id
// order; a component whose product no profile simulates is skipped. An
// address in used (one an existing endpoint already targets) is never handed
// out: each component takes the lowest free one, so a deletion or a
// component gaining a product later cannot put two devices on one address.
func Plan(comps []Component, used map[string]bool, cat Catalog) ([]Device, error) {
	sorted := append([]Component(nil), comps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	shift := sim.PrivilegedPortShift(cat.PortOffset)
	if shift == nil {
		shift = func(p int) int { return p } // the simulator binds the profiles' own ports
	}
	taken := make(map[string]bool, len(used))
	for a := range used {
		taken[a] = true
	}
	next := 0
	var out []Device
	for _, c := range sorted {
		prof, ok := cat.ProfileFor(c.Product)
		if !ok {
			continue
		}
		for next < AddressCap && taken[Address(next)] {
			next++
		}
		if next == AddressCap {
			return nil, fmt.Errorf("simfleet: the simulated block 127.60.0.0/16 is full (%d addresses)", AddressCap)
		}
		addr := Address(next)
		taken[addr] = true
		dev := Device{ComponentID: c.ID, Address: addr}
		driver := cat.DriverOf(c.Product)
		inputs, known := Attach[driver]
		if known && (cat.Attachable == nil || cat.Attachable(driver)) {
			dev.Endpoints = append(dev.Endpoints, Endpoint{Driver: driver, Inputs: inputs(addr, shift)})
		} else if e, ok := probeFor(prof, addr, shift); ok {
			dev.Endpoints = append(dev.Endpoints, e)
		}
		out = append(out, dev)
	}
	return out, nil
}

// Attach is the inputs supplied for each driver attached to a simulated
// device, keyed by driver name. A driver that gains a spec joins here; until
// then its products get a probe (the dev seed's test fails for a spec'd
// driver with simulated products that is missing).
var Attach = map[string]func(addr string, shift func(int) int) map[string]string{
	"snmp-generic":     snmpAttach,
	"polaris-amp-snmp": snmpAttach,
	"newtron-nvp": func(addr string, _ func(int) int) map[string]string {
		return map[string]string{"host": addr} // 51325, the spec's default, needs no privilege
	},
	// The Kestrel bar's https API on 443, its key pinned on first use (the
	// spec's default mode), and the Tessellate sensor's http API on 80.
	"kestrel-api": func(addr string, shift func(int) int) map[string]string {
		return map[string]string{"host": addr, "port": strconv.Itoa(shift(443)), "login": KestrelLoginSecret}
	},
	"http-json": func(addr string, shift func(int) int) map[string]string {
		return map[string]string{"host": addr, "port": strconv.Itoa(shift(80)), "key": CensusKeySecret}
	},
	// The UCC platform's https API on 443 (#1282), its key pinned on first
	// use, its token in the header the platform demands.
	"ucc-rooms": func(addr string, shift func(int) int) map[string]string {
		return map[string]string{"host": addr, "port": strconv.Itoa(shift(443)), "token": UCCTokenSecret}
	},
}

// snmpAttach is an snmp driver's inputs: the device's agent on 161 (shifted
// where the simulator lifts it) and the community secret.
func snmpAttach(addr string, shift func(int) int) map[string]string {
	return map[string]string{"host": addr, "port": strconv.Itoa(shift(161)), "community": CommunitySecret}
}

// probeFor picks a reachability probe from what the profile serves: HTTP
// where it serves plain HTTP, else a TCP port check (a TLS-only API, a line
// protocol).
func probeFor(prof *profile.DeviceProfile, addr string, shift func(int) int) (Endpoint, bool) {
	target := func(port int) string { return fmt.Sprintf("%s:%d", addr, shift(port)) }
	for _, iface := range prof.Interfaces {
		if iface.Protocol == "http" && !iface.TLS {
			return Endpoint{Transport: "http", Target: target(iface.Port)}, true
		}
	}
	for _, iface := range prof.Interfaces {
		if iface.Protocol == "http" || iface.Protocol == "tcp" {
			return Endpoint{Transport: "tcp", Target: target(iface.Port)}, true
		}
	}
	return Endpoint{}, false
}
