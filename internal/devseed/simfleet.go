package devseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The simulated fleet: every dev-seed component whose product the simulator
// serves gets a loopback address and endpoints on the dev node, so `make dev`
// beside `make dev-sim` (the simulator and the node) collects for real.
const (
	// SimPortOffset lifts the simulated devices' privileged ports (SNMP 161,
	// HTTP 80, SSH 22) above 1024 so the dev simulator runs unprivileged; the
	// endpoints target the lifted ports. It is seed-dev's default, and `make
	// dev-sim` passes the same value (a test holds the Makefile to it).
	SimPortOffset = 10000
)

// seedSimFleet lands the plan through the gateway: the community secret,
// then each planned component's endpoints on the dev node. A component that
// already has an endpoint is left alone, so a re-run changes nothing. Only the
// components whose ids are in only are planned.
func seedSimFleet(ctx context.Context, gw storage.Gateway, actorID string, portOffset int, only map[string]bool) error {
	all := scope.Set{All: true}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return fmt.Errorf("devseed: load simulator catalog: %w", err)
	}
	if _, err := gw.CreateSecret(ctx, actorID, storage.SecretSpec{
		Name: simfleet.CommunitySecret, Label: "Simulated fleet SNMP community", SecretType: "snmp-community",
		OwnerKind: "platform", Fields: map[string]string{"community": "public"},
	}, all, true); err != nil && !errors.Is(err, storage.ErrSecretExists) {
		return fmt.Errorf("devseed: create simulator community secret: %w", err)
	}
	// The http attaches' credentials (#902): what the simulated faces demand.
	for _, sec := range []storage.SecretSpec{
		{Name: simfleet.KestrelLoginSecret, Label: "Simulated Kestrel bar login", SecretType: "basic-auth",
			OwnerKind: "platform", Fields: map[string]string{"username": "admin", "password": "kestrel"}},
		{Name: simfleet.CensusKeySecret, Label: "Simulated Tessellate sensor api key", SecretType: "api-key",
			OwnerKind: "platform", Fields: map[string]string{"key": "census-lab-key"}},
	} {
		if _, err := gw.CreateSecret(ctx, actorID, sec, all, true); err != nil && !errors.Is(err, storage.ErrSecretExists) {
			return fmt.Errorf("devseed: create simulator secret %s: %w", sec.Name, err)
		}
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
	var cands []simfleet.Component
	for _, c := range comps {
		if c.ProductHandle != nil && only[c.ID] {
			cands = append(cands, simfleet.Component{ID: c.ID, Product: *c.ProductHandle})
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
	endpoints, err := gw.ListEndpoints(ctx, all)
	if err != nil {
		return fmt.Errorf("devseed: list endpoints: %w", err)
	}
	used := map[string]bool{}
	wired := map[string]bool{}
	var demo []storage.Endpoint
	for _, e := range endpoints {
		target := endpointTarget(e.Params)
		if target != "" {
			used[storage.TargetHost(target)] = true
		}
		if e.ComponentID != nil {
			wired[*e.ComponentID] = true
		}
		if storage.TargetHost(target) == reachHost && e.ComponentID != nil {
			demo = append(demo, e)
		}
	}
	if err := simulateReachabilityDemo(ctx, gw, actorID, demo, used, portOffset); err != nil {
		return err
	}
	var unwired []simfleet.Component
	for _, c := range cands {
		if !wired[c.ID] {
			unwired = append(unwired, c)
		}
	}
	plan, err := simfleet.Plan(unwired, used, simfleet.Catalog{ProfileFor: profileFor, DriverOf: func(p string) string { return drivers[p] }, PortOffset: portOffset})
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

func endpointTarget(params []byte) string {
	var p struct {
		Target string `json:"target"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Target
}

// simulateReachabilityDemo re-aims the worked reachability check at a
// simulated device. The demo DSP targets a fixed address nothing answers,
// which reads as down the moment make dev-sim runs a real node; with the
// simulated fleet it becomes the simulated DSP it was modeled on (HTTP 80
// and the line protocol on 1710), at a free simulated address, so a live
// node finds it up. Its seeded history stays, as history. Without the
// simulated fleet (the docs screenshots) this never runs.
func simulateReachabilityDemo(ctx context.Context, gw storage.Gateway, actorID string, demo []storage.Endpoint, used map[string]bool, portOffset int) error {
	if len(demo) == 0 {
		return nil // already simulated on an earlier run, or no demo
	}
	all := scope.Set{All: true}
	addr, ok := simfleet.FreeAddress(used)
	if !ok {
		return fmt.Errorf("devseed: the simulated block is full; no address for the reachability demo")
	}
	used[addr] = true
	product := reachProduct
	if _, err := gw.UpdateComponent(ctx, actorID, *demo[0].ComponentID, storage.ComponentPatch{ProductName: &product}, all, all); err != nil {
		return fmt.Errorf("devseed: make the reachability demo a simulated DSP: %w", err)
	}
	shift := sim.PrivilegedPortShift(portOffset)
	if shift == nil {
		shift = func(p int) int { return p } // the simulator binds the profiles' own ports
	}
	for _, e := range demo {
		_, portText, err := net.SplitHostPort(endpointTarget(e.Params))
		if err != nil {
			return fmt.Errorf("devseed: reachability demo endpoint %s: %w", e.Name, err)
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return fmt.Errorf("devseed: reachability demo endpoint %s port: %w", e.Name, err)
		}
		params := []byte(fmt.Sprintf(`{"target":"%s:%d"}`, addr, shift(port)))
		if _, err := gw.UpdateEndpoint(ctx, actorID, e.ID, storage.EndpointPatch{Params: params}, all, all); err != nil {
			return fmt.Errorf("devseed: re-aim reachability demo endpoint %s: %w", e.Name, err)
		}
	}
	return nil
}

// seedSimEndpoints lands the fixture's extra simulated endpoints (#1059): the
// secrets they reference, then each endpoint as a driver attach at its
// component's simulated address (the host an endpoint the plan gave it already
// targets), the driver's declared port lifted as the simulator lifts it, on
// the dev node, with its ssh host key pinned where the fixture names one
// (#1078). Idempotent: a secret that exists is kept, and a component that
// already holds an endpoint of the driver's transport is left alone, so a
// re-run changes nothing. Runs after seedSimFleet, which gives every simulated
// component its address.
func seedSimEndpoints(ctx context.Context, gw storage.Gateway, actorID string, doc Doc, compIDs map[string]string, portOffset int) error {
	all := scope.Set{All: true}
	for _, s := range doc.SimSecrets {
		if _, err := gw.CreateSecret(ctx, actorID, storage.SecretSpec{
			Name: s.Name, Label: s.Label, SecretType: s.SecretType, OwnerKind: "platform", Fields: s.Fields,
		}, all, true); err != nil && !errors.Is(err, storage.ErrSecretExists) {
			return fmt.Errorf("devseed: create simulated secret %s: %w", s.Name, err)
		}
	}
	if len(doc.SimEndpoints) == 0 {
		return nil
	}
	drivers, err := gw.ListDrivers(ctx)
	if err != nil {
		return fmt.Errorf("devseed: list drivers: %w", err)
	}
	specs := map[string]*driver.Spec{}
	for _, d := range drivers {
		if len(d.Spec) == 0 {
			continue
		}
		spec, err := driver.Parse(d.Spec)
		if err != nil {
			return fmt.Errorf("devseed: parse driver %s: %w", d.Name, err)
		}
		specs[d.Name] = spec
	}
	endpoints, err := gw.ListEndpoints(ctx, all)
	if err != nil {
		return fmt.Errorf("devseed: list endpoints: %w", err)
	}
	shift := sim.PrivilegedPortShift(portOffset)
	if shift == nil {
		shift = func(p int) int { return p } // the simulator binds the profiles' own ports
	}
	node := reachNode
	for _, se := range doc.SimEndpoints {
		compID, ok := compIDs[se.Component]
		if !ok {
			return fmt.Errorf("devseed: simulated endpoint references unknown component key %q", se.Component)
		}
		spec, ok := specs[se.Driver]
		if !ok {
			return fmt.Errorf("devseed: simulated endpoint on %s names driver %q, which has no spec", se.Component, se.Driver)
		}
		host, held := "", false
		for _, e := range endpoints {
			if e.ComponentID == nil || *e.ComponentID != compID {
				continue
			}
			if e.Transport == spec.Transport {
				held = true
			}
			if h := storage.TargetHost(endpointTarget(e.Params)); strings.HasPrefix(h, "127.60.") {
				host = h
			}
		}
		if held {
			continue
		}
		if host == "" {
			return fmt.Errorf("devseed: simulated endpoint on %s: the component has no simulated address", se.Component)
		}
		inputs := map[string]string{"host": host}
		for _, in := range spec.Inputs {
			if in.Name == "port" && in.Default != "" {
				port, err := strconv.Atoi(in.Default)
				if err != nil {
					return fmt.Errorf("devseed: driver %s port default %q: %w", se.Driver, in.Default, err)
				}
				inputs["port"] = strconv.Itoa(shift(port))
			}
		}
		for k, v := range se.Inputs {
			inputs[k] = v
		}
		comp, drv := compID, se.Driver
		created, err := gw.CreateEndpoint(ctx, actorID, storage.EndpointSpec{
			Driver: &drv, Inputs: inputs, Label: se.Label, Component: &comp, Node: &node, SecretRead: all, CanAdmin: true,
		}, all)
		if err != nil {
			return fmt.Errorf("devseed: create simulated %s endpoint on %s: %w", se.Driver, se.Component, err)
		}
		// Pinned once, at creation: a re-run skips the endpoint above, so a
		// key an operator trusted since is never put back.
		if se.HostKey != "" {
			if _, err := gw.RetryEndpoint(ctx, actorID, created.ID, storage.RetryPin{HostKey: se.HostKey}, all, all); err != nil {
				return fmt.Errorf("devseed: pin the host key of the simulated %s endpoint on %s: %w", se.Driver, se.Component, err)
			}
		}
	}
	return nil
}
