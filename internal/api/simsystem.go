package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type simRoleFillBody struct {
	Role      string `json:"role"`
	Alternate string `json:"alternate,omitempty" doc:"The choice alternate the role belongs to, choice/alternate; empty when unconditional."`
	Product   string `json:"product,omitempty" doc:"The simulated product chosen for the role; empty when nothing simulated fits."`
	Count     int    `json:"count" doc:"How many simulated components the role gets: its quorum, at least one, capped at its capacity."`
	Missing   int    `json:"missing,omitempty" doc:"How many the role stays short when nothing simulated fits."`
	Reason    string `json:"reason,omitempty" doc:"Why the role is left unstaffed."`
}

type simSystemPlanOutput struct {
	Body struct {
		Standard   string            `json:"standard"`
		Alternates map[string]string `json:"alternates" doc:"The alternate taken for each of the standard's choices."`
		Roles      []simRoleFillBody `json:"roles"`
		Components int               `json:"components" doc:"How many simulated components the system would get."`
	}
}

type simStandardInput struct {
	Standard string `path:"standard" doc:"The standard to staff, by name"`
}

type addSimSystemInput struct {
	Name string `path:"name" doc:"The node whose simulator serves the system's components"`
	Body struct {
		Standard   string  `json:"standard" minLength:"1" doc:"The standard the system is built to; its roles decide the components"`
		SystemType string  `json:"system_type" minLength:"1" doc:"The system's type, which also mints its name when none is given"`
		Location   *string `json:"location,omitempty" required:"false" doc:"Where the system and its components sit, by name or dotted path"`
		SystemName string  `json:"name,omitempty" required:"false" doc:"The system's name; minted from its type when empty"`
	}
}

type addSimSystemComponentBody struct {
	Component string `json:"component"`
	Role      string `json:"role"`
	Product   string `json:"product"`
	Address   string `json:"address" doc:"The loopback address the simulated component answers on."`
}

type addSimSystemOutput struct {
	Body struct {
		System     string                      `json:"system"`
		SystemID   string                      `json:"system_id" doc:"The new system's id, the stable address a link to it uses."`
		Node       string                      `json:"node"`
		Components []addSimSystemComponentBody `json:"components"`
		Unstaffed  []simRoleFillBody           `json:"unstaffed" doc:"The roles nothing simulated could fill, left short."`
	}
}

// simDrivers maps each product to its driver.
func simDrivers(products []storage.Product) map[string]string {
	drivers := map[string]string{}
	for _, p := range storage.InEffect(products, func(p storage.Product) (string, string) { return p.Namespace, p.Name }) {
		if p.DriverName != nil {
			drivers[p.Name] = *p.DriverName
		}
	}
	return drivers
}

// simAttachable says whether a driver can be attached for this caller: an
// attach that references a platform secret (the SNMP community, the http
// drivers' credentials, #902) needs that secret to exist in the caller's
// reach.
func simAttachable(ctx context.Context, a *authenticator, gw storage.Gateway) func(string) bool {
	return func(driver string) bool {
		name, needs := simfleet.AttachSecret[driver]
		if !needs {
			return true
		}
		secrets, err := gw.ListSecrets(ctx, a.scopeFor(ctx, "secret", "read"), a.canSecretAdmin(ctx, "read"))
		if err != nil {
			return false
		}
		for _, s := range secrets {
			if s.Name == name {
				return true
			}
		}
		return false
	}
}

// simEndpointSpecs turns planned endpoints into endpoint creates; the
// component, node and secret reach are filled by the caller.
func simEndpointSpecs(eps []simfleet.Endpoint) []storage.EndpointSpec {
	out := make([]storage.EndpointSpec, 0, len(eps))
	for _, e := range eps {
		if e.Driver != "" {
			driver := e.Driver
			out = append(out, storage.EndpointSpec{Driver: &driver, Inputs: e.Inputs})
		} else {
			out = append(out, storage.EndpointSpec{Transport: e.Transport, Params: []byte(fmt.Sprintf(`{"target":%q}`, e.Target))})
		}
	}
	return out
}

// planSimSystem staffs a standard's roles with simulated products.
func planSimSystem(ctx context.Context, gw storage.Gateway, standard string, store *profile.Store, products []storage.Product) (simfleet.SystemPlan, error) {
	if _, err := gw.GetStandard(ctx, standard); err != nil {
		return simfleet.SystemPlan{}, mapStandardErr(err)
	}
	roles, err := gw.ListSystemRoles(ctx, "standard", standard)
	if err != nil {
		return simfleet.SystemPlan{}, huma.Error500InternalServerError("list the standard's roles")
	}
	types, err := gw.ListComponentTypes(ctx)
	if err != nil {
		return simfleet.SystemPlan{}, huma.Error500InternalServerError("list component types")
	}
	// By name, as a bare name reaches them (#1432): one row per name.
	typeIDs := map[string]storage.ComponentType{}
	for _, t := range storage.InEffect(types, func(t storage.ComponentType) (string, string) { return t.Namespace, t.Name }) {
		typeIDs[t.Name] = t
	}
	in := make([]simfleet.Role, 0, len(roles))
	for _, r := range roles {
		in = append(in, simfleet.Role{Name: r.Name, Quorum: r.Quorum, Capacity: r.Capacity,
			AcceptedTypes: r.AcceptedTypes, PinnedProducts: r.PinnedProducts, Alternate: r.Alternate})
	}
	ps := make([]simfleet.Product, 0, len(products))
	for _, p := range products {
		ps = append(ps, simfleet.Product{Name: p.Name, ComponentType: p.ComponentType})
	}
	return simfleet.PlanSystem(in, ps, simfleet.SystemCatalog{
		Simulated: func(p string) bool { _, _, ok := store.ForProduct(p); return ok },
		Within: func(t, accepted string) bool {
			a, ok1 := typeIDs[t]
			b, ok2 := typeIDs[accepted]
			if !ok1 || !ok2 {
				return false
			}
			ok, err := gw.TypeIsWithin(ctx, a.ID, b.ID)
			return err == nil && ok
		},
	}), nil
}

// mapSimSystemErr maps a simulated system's failure the way the failing
// piece's own route maps it: a component refused is the component route's
// 403, a role shortfall the role route's 422, and so on.
func mapSimSystemErr(err error) error {
	var se *storage.SimSystemError
	if !errors.As(err, &se) {
		return mapSystemErr(err)
	}
	switch se.Stage {
	case "component":
		return mapComponentErr(se.Err)
	case "endpoint":
		return mapEndpointErr(se.Err)
	case "role":
		return mapRoleErr(se.Err)
	default:
		return mapSystemErr(se.Err)
	}
}

func fillBody(f simfleet.RoleFill) simRoleFillBody {
	return simRoleFillBody{Role: f.Role, Alternate: f.Alternate, Product: f.Product, Count: f.Count, Missing: f.Missing, Reason: f.Reason}
}

func registerSimSystemRoutes(api huma.API, a *authenticator, gw storage.Gateway, hub SimHub) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "plan-simulation-system",
		Method:      http.MethodGet,
		Path:        "/simulation/standards/{standard}:plan",
		Summary:     "Plan a simulated system to a standard",
		Description: "Previews a simulated system built to a standard: each of the standard's roles (one alternate taken per choice) staffed with the first simulated product that fits its accepted types and pins, as many as its quorum and at least one, or left short with the reason when nothing simulated fits. Creates nothing. Gated by simulation:read.",
	}, "simulation", "read"), func(ctx context.Context, in *simStandardInput) (*simSystemPlanOutput, error) {
		store, err := simCatalog()
		if err != nil {
			return nil, huma.Error500InternalServerError("load simulator catalog")
		}
		products, err := gw.ListProducts(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list products")
		}
		plan, err := planSimSystem(ctx, gw, in.Standard, store, productsInEffect(products))
		if err != nil {
			return nil, err
		}
		out := &simSystemPlanOutput{}
		out.Body.Standard, out.Body.Alternates, out.Body.Components = in.Standard, plan.Alternates, plan.Components()
		out.Body.Roles = make([]simRoleFillBody, 0, len(plan.Roles))
		for _, f := range plan.Roles {
			out.Body.Roles = append(out.Body.Roles, fillBody(f))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "add-node-simulation-system",
		Method:        http.MethodPost,
		Path:          "/nodes/{name}/simulation/systems",
		DefaultStatus: http.StatusCreated,
		Summary:       "Add a simulated system",
		Description:   "Creates a system built to a standard, with a simulated component for each staffed role in the standard's plan (see plan-simulation-system), each at its own free address in the simulated block with its endpoint placed on the node, and assigned its role, all in one transaction: it lands whole or not at all. Component names are minted as an unnamed create mints them; roles nothing simulated fits are left short and listed. The simulator serves the components from its next pull. Gated by simulation:update, and also needs system:create, system:update, system:read, component:create, component:update, endpoint:create and node:read (and location:read when it names a place), each in the caller's own scope; the components are created without a parent, so component:create must be all-scope, and so must component:update, since assigning each its role writes onto it as an assignment route does. A refused piece is mapped as its own route maps it (a component refused is a 403). A node with no simulator connected is a 409, and a standard nothing simulated can staff a 422.",
	}, "simulation", "update"), func(ctx context.Context, in *addSimSystemInput) (*addSimSystemOutput, error) {
		needs := [][2]string{{"system", "create"}, {"system", "update"}, {"system", "read"}, {"component", "create"}, {"component", "update"}, {"endpoint", "create"}, {"node", "read"}}
		if in.Body.Location != nil && *in.Body.Location != "" {
			needs = append(needs, [2]string{"location", "read"})
		}
		for _, need := range needs {
			if !a.allows(ctx, need[0], need[1]) {
				return nil, huma.Error403Forbidden(fmt.Sprintf("adding a simulated system also needs %s:%s", need[0], need[1]))
			}
		}
		if _, err := gw.GetNode(ctx, in.Name, a.scopeFor(ctx, "node", "read")); err != nil {
			return nil, mapNodeErr(err)
		}
		var st struct {
			seen       bool
			lastSeen   time.Time
			portOffset int
		}
		if hub != nil {
			s, seen := hub.SimStatus(in.Name)
			st.seen, st.lastSeen, st.portOffset = seen, s.LastSeen, s.Capabilities.PortOffset
		}
		if !st.seen || time.Since(st.lastSeen) > simConnectedWindow {
			return nil, huma.Error409Conflict(fmt.Sprintf("no simulator is connected beside %s; start one with omniglass sim run", in.Name))
		}
		store, err := simCatalog()
		if err != nil {
			return nil, huma.Error500InternalServerError("load simulator catalog")
		}
		products, err := gw.ListProducts(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list products")
		}
		plan, err := planSimSystem(ctx, gw, in.Body.Standard, store, productsInEffect(products))
		if err != nil {
			return nil, err
		}
		if plan.Components() == 0 {
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("nothing the simulator serves fits a role of %s", in.Body.Standard))
		}
		drivers := simDrivers(products)
		attachable := simAttachable(ctx, a, gw)
		out := &addSimSystemOutput{}
		out.Body.Node = in.Name
		out.Body.Unstaffed = []simRoleFillBody{}
		for _, f := range plan.Roles {
			if f.Count == 0 {
				out.Body.Unstaffed = append(out.Body.Unstaffed, fillBody(f))
			}
		}
		// Addresses are planned and claimed under the lock, so a concurrent
		// add cannot be handed the same one.
		err = gw.WithSimAddressLock(ctx, func() error {
			used, err := gw.SimAddressesInUse(ctx)
			if err != nil {
				return huma.Error500InternalServerError("plan simulated system")
			}
			// Each component gets a placeholder id in slot order, so the
			// address planner hands out addresses in that order.
			type slot struct {
				role, product, id string
			}
			var slots []slot
			var comps []simfleet.Component
			for _, f := range plan.Roles {
				for i := 0; i < f.Count; i++ {
					id := fmt.Sprintf("%06d", len(slots))
					slots = append(slots, slot{f.Role, f.Product, id})
					comps = append(comps, simfleet.Component{ID: id, Product: f.Product})
				}
			}
			addrs, err := simfleet.Plan(comps, used, simfleet.Catalog{
				ProfileFor: func(p string) (*profile.DeviceProfile, bool) {
					name, _, ok := store.ForProduct(p)
					return store.Devices[name], ok
				},
				DriverOf:   func(p string) string { return drivers[p] },
				Attachable: attachable,
				PortOffset: st.portOffset,
			})
			if err != nil || len(addrs) != len(slots) {
				return huma.Error409Conflict("not enough addresses are free in the simulated block")
			}
			systemType, standard := in.Body.SystemType, in.Body.Standard
			spec := storage.SimSystemSpec{
				System: storage.SystemSpec{Name: in.Body.SystemName, SystemTypeID: &systemType, StandardID: &standard, LocationName: in.Body.Location},
				Node:   in.Name,
			}
			for i, s := range slots {
				if len(addrs[i].Endpoints) == 0 {
					return huma.Error409Conflict(fmt.Sprintf("the collector has no way to reach a simulated %s", s.product))
				}
				spec.Components = append(spec.Components, storage.SimSystemComponent{Role: s.role, Product: s.product, Endpoints: simEndpointSpecs(addrs[i].Endpoints)})
			}
			made, err := gw.CreateSimSystem(ctx, actorID(ctx), spec, storage.SimSystemScopes{
				SystemCreate: a.scopeFor(ctx, "system", "create"), LocationRead: a.scopeFor(ctx, "location", "read"),
				ComponentCreate: a.scopeFor(ctx, "component", "create"), SystemRead: a.scopeFor(ctx, "system", "read"),
				SystemUpdate: a.scopeFor(ctx, "system", "update"), EndpointCreate: a.scopeFor(ctx, "endpoint", "create"),
				SecretRead: a.scopeFor(ctx, "secret", "read"), CanAdmin: a.canSecretAdmin(ctx, "read"),
				ComponentRead: a.scopeFor(ctx, "component", "read"), ComponentUpdate: a.scopeFor(ctx, "component", "update"),
			})
			if err != nil {
				return mapSimSystemErr(err)
			}
			out.Body.System, out.Body.SystemID = made.System.Name, made.System.ID
			for i, c := range made.Components {
				out.Body.Components = append(out.Body.Components, addSimSystemComponentBody{Component: c.Name,
					Role: slots[i].role, Product: slots[i].product, Address: addrs[i].Address})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	})
}

// productsInEffect is the products a bare name reaches (#1432), one row per
// name, for a planner that addresses products by name.
func productsInEffect(products []storage.Product) []storage.Product {
	return storage.InEffect(products, func(p storage.Product) (string, string) { return p.Namespace, p.Name })
}
