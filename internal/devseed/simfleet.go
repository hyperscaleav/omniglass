package devseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The simulated fleet: every dev-seed component whose product the simulator
// serves gets a loopback address and endpoints on the dev node, so `make dev`
// beside `make dev-sim` (the simulator and the node) collects for real.
const (
	// SimPortOffset lifts the simulated devices' privileged ports (SNMP 161,
	// HTTP 80, SSH 22) above 1024 so the dev simulator runs unprivileged; the
	// endpoints target the lifted ports. `make dev-sim` passes the same value
	// (a test holds the Makefile to it).
	SimPortOffset = 10000
	// simCommunitySecret is the platform secret the SNMP attaches reference.
	simCommunitySecret = "sim-community"
)

type simComponent struct {
	ID      string
	Product string
}

// simEndpoint is one endpoint to create: a driver attach (Driver and
// Inputs) or a bare reachability probe (Transport and Target).
type simEndpoint struct {
	Driver    string
	Inputs    map[string]string
	Transport string
	Target    string
}

type simDevice struct {
	ComponentID string
	Address     string
	Endpoints   []simEndpoint
}

// simAddressCap is how many addresses the block holds: 256 third octets of
// 250 hosts each.
const simAddressCap = 256 * 250

// simAddress is the i-th address of the simulated block, 127.60.0.1 onward.
func simAddress(i int) string {
	return fmt.Sprintf("127.60.%d.%d", i/250, i%250+1)
}

// planSimFleet decides the address and endpoints of each component not yet
// wired, taken in id order. An address in used (one an existing endpoint
// already targets) is never handed out: each component takes the lowest
// free one, so a deletion or a component gaining a product later cannot put
// two devices on one address. A product on a driver whose inputs the seed
// knows (simAttach) gets that driver attached; any other gets a
// reachability probe on the port its profile serves.
func planSimFleet(comps []simComponent, used map[string]bool, profileFor func(string) (*profile.DeviceProfile, bool), driverOf func(string) string) ([]simDevice, error) {
	sorted := append([]simComponent(nil), comps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	shift := sim.PrivilegedPortShift(SimPortOffset)
	taken := make(map[string]bool, len(used))
	for a := range used {
		taken[a] = true
	}
	next := 0
	var out []simDevice
	for _, c := range sorted {
		prof, ok := profileFor(c.Product)
		if !ok {
			continue
		}
		for next < simAddressCap && taken[simAddress(next)] {
			next++
		}
		if next == simAddressCap {
			return nil, fmt.Errorf("devseed: the simulated block 127.60.0.0/16 is full (%d addresses)", simAddressCap)
		}
		addr := simAddress(next)
		taken[addr] = true
		dev := simDevice{ComponentID: c.ID, Address: addr}
		driver := driverOf(c.Product)
		if inputs, ok := simAttach[driver]; ok {
			dev.Endpoints = append(dev.Endpoints, simEndpoint{Driver: driver, Inputs: inputs(addr, shift)})
		} else if e, ok := probeFor(prof, addr, shift); ok {
			dev.Endpoints = append(dev.Endpoints, e)
		}
		out = append(out, dev)
	}
	return out, nil
}

// simAttach is the inputs the seed supplies for each driver it attaches,
// keyed by driver name. A driver that gains a spec joins here; until then
// its products get a probe, and the dev seed's test fails for a spec'd
// driver with simulated products that is missing.
var simAttach = map[string]func(addr string, shift func(int) int) map[string]string{
	"snmp-generic": func(addr string, shift func(int) int) map[string]string {
		return map[string]string{"host": addr, "port": strconv.Itoa(shift(161)), "community": simCommunitySecret}
	},
	"newtron-nvp": func(addr string, _ func(int) int) map[string]string {
		return map[string]string{"host": addr} // 51325, the spec's default, needs no privilege
	},
}

// probeFor picks a reachability probe from what the profile serves: HTTP
// where it serves plain HTTP, else a TCP port check (a TLS-only API, a line
// protocol).
func probeFor(prof *profile.DeviceProfile, addr string, shift func(int) int) (simEndpoint, bool) {
	target := func(port int) string { return fmt.Sprintf("%s:%d", addr, shift(port)) }
	for _, iface := range prof.Interfaces {
		if iface.Protocol == "http" && !iface.TLS {
			return simEndpoint{Transport: "http", Target: target(iface.Port)}, true
		}
	}
	for _, iface := range prof.Interfaces {
		if iface.Protocol == "http" || iface.Protocol == "tcp" {
			return simEndpoint{Transport: "tcp", Target: target(iface.Port)}, true
		}
	}
	return simEndpoint{}, false
}

// seedSimFleet lands the plan through the gateway: the community secret,
// then each planned component's endpoints on the dev node. A component that
// already has an endpoint is left alone, so a re-run changes nothing.
func seedSimFleet(ctx context.Context, gw storage.Gateway, actorID string) error {
	all := scope.Set{All: true}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return fmt.Errorf("devseed: load simulator catalog: %w", err)
	}
	if _, err := gw.CreateSecret(ctx, actorID, storage.SecretSpec{
		Name: simCommunitySecret, Label: "Simulated fleet SNMP community", SecretType: "snmp-community",
		OwnerKind: "platform", Fields: map[string]string{"community": "public"},
	}, all, true); err != nil && !errors.Is(err, storage.ErrSecretExists) {
		return fmt.Errorf("devseed: create simulator community secret: %w", err)
	}
	products, err := gw.ListProducts(ctx)
	if err != nil {
		return fmt.Errorf("devseed: list products: %w", err)
	}
	drivers := map[string]string{}
	for _, p := range products {
		if p.DriverName != nil {
			drivers[p.Name] = *p.DriverName
		}
	}
	comps, err := gw.ListComponents(ctx, all)
	if err != nil {
		return fmt.Errorf("devseed: list components: %w", err)
	}
	var cands []simComponent
	for _, c := range comps {
		if c.ProductHandle != nil {
			cands = append(cands, simComponent{ID: c.ID, Product: *c.ProductHandle})
		}
	}
	profileFor := func(p string) (*profile.DeviceProfile, bool) {
		name, _, ok := store.ForProduct(p)
		if !ok {
			return nil, false
		}
		return store.Devices[name], true
	}
	// Components already wired keep their endpoints; every address any
	// endpoint targets is taken.
	used := map[string]bool{}
	var unwired []simComponent
	for _, c := range cands {
		existing, err := gw.ListComponentEndpoints(ctx, c.ID)
		if err != nil {
			return fmt.Errorf("devseed: check simulated endpoints: %w", err)
		}
		for _, e := range existing {
			var params struct {
				Target string `json:"target"`
			}
			if json.Unmarshal(e.Params, &params) == nil && params.Target != "" {
				used[storage.TargetHost(params.Target)] = true
			}
		}
		if len(existing) == 0 {
			unwired = append(unwired, c)
		}
	}
	plan, err := planSimFleet(unwired, used, profileFor, func(p string) string { return drivers[p] })
	if err != nil {
		return err
	}
	node := reachNode
	for _, dev := range plan {
		comp := dev.ComponentID
		for _, e := range dev.Endpoints {
			spec := storage.EndpointSpec{Component: &comp, Node: &node, SecretRead: all, CanAdmin: true}
			if e.Driver != "" {
				driver := e.Driver
				spec.Driver, spec.Inputs = &driver, e.Inputs
			} else {
				spec.Transport, spec.Params = e.Transport, []byte(fmt.Sprintf(`{"target":%q}`, e.Target))
			}
			if _, err := gw.CreateEndpoint(ctx, actorID, spec, all); err != nil {
				return fmt.Errorf("devseed: create simulated endpoint on %s: %w", dev.ComponentID, err)
			}
		}
	}
	return nil
}
