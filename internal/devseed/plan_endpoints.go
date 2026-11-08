package devseed

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// plannedEndpoint is one endpoint a plan's component takes: the simulated
// fleet's decision (an address, a driver attach or a probe) before any row.
type plannedEndpoint struct {
	Comp int // index into the plan's components
	simfleet.Endpoint
}

// planDevices decides every component's address and endpoints with the
// simulated fleet's own planner (the one seedSimFleet and the console's
// add-a-device use). The planner takes components in id order; a seeded plan
// has no ids yet, so each is given its creation index, zero padded, which sorts
// in creation order: the same plan then hands out the same addresses to the same
// components on every run, which a uuid sort would not.
func planDevices(ctx context.Context, gw storage.Gateway, rows planRows, portOffset int) ([]plannedEndpoint, error) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return nil, fmt.Errorf("devseed: load simulator catalog: %w", err)
	}
	products, err := gw.ListProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("devseed: list products: %w", err)
	}
	drivers := map[string]string{}
	for _, p := range storage.InEffect(products, func(p storage.Product) (string, string) { return p.Namespace, p.Name }) {
		if p.DriverName != nil {
			drivers[p.Name] = *p.DriverName
		}
	}
	cands := make([]simfleet.Component, len(rows.Components))
	for i, c := range rows.Components {
		cands[i] = simfleet.Component{ID: fmt.Sprintf("%09d", i), Product: c.Product}
	}
	devs, err := simfleet.Plan(cands, nil, simfleet.Catalog{
		ProfileFor: func(p string) (*profile.DeviceProfile, bool) {
			name, _, ok := store.ForProduct(p)
			if !ok {
				return nil, false
			}
			return store.Devices[name], true
		},
		DriverOf:   func(p string) string { return drivers[p] },
		PortOffset: portOffset,
	})
	if err != nil {
		return nil, err
	}
	var out []plannedEndpoint
	for _, d := range devs {
		i, _ := strconv.Atoi(d.ComponentID)
		for _, e := range d.Endpoints {
			out = append(out, plannedEndpoint{Comp: i, Endpoint: e})
		}
	}
	return out, nil
}

// seedPlanEndpoints creates the simulated fleet's endpoints for a plan's
// components and returns how many it made.
func seedPlanEndpoints(ctx context.Context, gw storage.Gateway, actorID string, rows planRows, compIDs []string, opts PlanOptions, run *planRun) (int, error) {
	all := scope.Set{All: true}
	for _, sec := range simSecrets {
		if _, err := gw.CreateSecret(ctx, actorID, sec, all, true); err != nil && !isSecretExists(err) {
			return 0, fmt.Errorf("devseed: create simulator secret %s: %w", sec.Name, err)
		}
	}
	planned, err := planDevices(ctx, gw, rows, opts.PortOffset)
	if err != nil {
		return 0, err
	}
	run.done("endpoint plan")
	for _, e := range planned {
		comp := compIDs[e.Comp]
		node := opts.nodeOf(rows.Components[e.Comp].Site)
		spec := storage.EndpointSpec{Component: &comp, Node: node, SecretRead: all, CanAdmin: true}
		if e.Driver != "" {
			drv := e.Driver
			spec.Driver, spec.Inputs = &drv, e.Inputs
		} else {
			spec.Transport, spec.Params = e.Transport, []byte(fmt.Sprintf(`{"target":%q}`, e.Target))
		}
		if _, err := gw.CreateEndpoint(ctx, actorID, spec, all); err != nil {
			return 0, fmt.Errorf("devseed: create endpoint on %s: %w", rows.Components[e.Comp].Key, err)
		}
	}
	run.done("endpoints")
	return len(planned), nil
}

// nodeOf is the node a site's endpoints are placed on, nil for none: NodeFor's
// answer for the site, else Node.
func (o PlanOptions) nodeOf(site string) *string {
	n := o.Node
	if o.NodeFor != nil {
		if f := o.NodeFor(site); f != "" {
			n = f
		}
	}
	if n == "" {
		return nil
	}
	return &n
}

func isSecretExists(err error) bool { return errors.Is(err, storage.ErrSecretExists) }
