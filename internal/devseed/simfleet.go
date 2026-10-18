package devseed

import (
	"context"
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

// simAddress is the i-th address of the simulated block, 127.60.0.1 onward.
func simAddress(i int) string {
	return fmt.Sprintf("127.60.%d.%d", i/250, i%250+1)
}

// planSimFleet decides each simulated component's address and endpoints.
// Components are taken in id order, so a component added later takes the
// next address and existing ones keep theirs. A product on a driver with a
// spec gets that driver attached with the inputs it declares; a product
// whose driver is still a stub gets a reachability probe on the port its
// profile serves.
func planSimFleet(comps []simComponent, profileFor func(string) (*profile.DeviceProfile, bool), driverOf func(string) string) []simDevice {
	sorted := append([]simComponent(nil), comps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	shift := sim.PrivilegedPortShift(SimPortOffset)
	var out []simDevice
	for _, c := range sorted {
		prof, ok := profileFor(c.Product)
		if !ok {
			continue
		}
		addr := simAddress(len(out))
		dev := simDevice{ComponentID: c.ID, Address: addr}
		switch driverOf(c.Product) {
		case "snmp-generic":
			dev.Endpoints = append(dev.Endpoints, simEndpoint{Driver: "snmp-generic", Inputs: map[string]string{
				"host": addr, "port": strconv.Itoa(shift(161)), "community": simCommunitySecret,
			}})
		case "newtron-nvp":
			dev.Endpoints = append(dev.Endpoints, simEndpoint{Driver: "newtron-nvp", Inputs: map[string]string{"host": addr}})
		default:
			if e, ok := probeFor(prof, addr, shift); ok {
				dev.Endpoints = append(dev.Endpoints, e)
			}
		}
		out = append(out, dev)
	}
	return out
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
	node := reachNode
	for _, dev := range planSimFleet(cands, profileFor, func(p string) string { return drivers[p] }) {
		existing, err := gw.ListComponentEndpoints(ctx, dev.ComponentID)
		if err != nil {
			return fmt.Errorf("devseed: check simulated endpoints: %w", err)
		}
		if len(existing) > 0 {
			continue
		}
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
