package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// SimHub is the server's line to the simulators beside its nodes
// (bus.Server): their status, which nodes have one, and a way to ask one to
// run an operation.
type SimHub interface {
	SimStatus(node string) (simhub.Status, bool)
	Nodes() []string
	FireSimOperation(ctx context.Context, node string, op collection.SimOperation) (collection.SimOperationResult, error)
}

// WithSimHub supplies the server's simulators: the node simulation view, the
// fleet view, and firing operations all read through it.
func WithSimHub(h SimHub) Option {
	return func(o *options) {
		o.simHub = h
		o.simStatus = h.SimStatus
	}
}

// simCatalog is the simulator's profile catalog, the same one the simulator
// embeds, loaded once.
var simCatalog = sync.OnceValues(func() (*profile.Store, error) { return profile.LoadStoreFS(profiles.FS) })

type simNodeBody struct {
	Node         string                         `json:"node"`
	Connected    bool                           `json:"connected"`
	LastSeen     time.Time                      `json:"last_seen"`
	Capabilities collection.SimCapabilities     `json:"capabilities"`
	Impairments  collection.SimImpairmentStatus `json:"impairments"`
	Devices      []simDeviceBody                `json:"devices"`
}

type simFleetOutput struct {
	Body struct {
		Connected bool          `json:"connected" doc:"Any simulator pulled its device list recently; the console shows its Simulation page only then."`
		Nodes     []simNodeBody `json:"nodes" doc:"Each node a simulator has pulled for since the server started, that the caller may read."`
	}
}

type simProfileBody struct {
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	DeviceType   string   `json:"device_type"`
	Summary      string   `json:"summary"`
	Products     []string `json:"products" doc:"The products this profile simulates, its own first, then its variants."`
	Protocols    []string `json:"protocols" doc:"The protocols its surfaces speak."`
}

type simProfilesOutput struct {
	Body struct {
		Profiles []simProfileBody `json:"profiles"`
	}
}

type simRouteBody struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary,omitempty"`
	Example string `json:"example,omitempty"`
	Private bool   `json:"private,omitempty" doc:"A route the device's own web UI calls but does not advertise."`
}

type simCommandBody struct {
	Match   string `json:"match"`
	Summary string `json:"summary,omitempty"`
	Example string `json:"example,omitempty"`
	Action  string `json:"action,omitempty" doc:"subscribe or unsubscribe on a persistent session; empty for a request and its answer."`
}

type simOIDBody struct {
	OID  string `json:"oid"`
	Path string `json:"path" doc:"The state path the OID reads."`
}

type simSurfaceBody struct {
	Name        string           `json:"name"`
	Protocol    string           `json:"protocol"`
	Port        int              `json:"port" doc:"The port the simulator binds, after any lift of ports below 1024."`
	TLS         bool             `json:"tls,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Persistent  bool             `json:"persistent,omitempty" doc:"A session that stays open across commands and may push."`
	AuthType    string           `json:"auth_type,omitempty"`
	DefaultUser string           `json:"default_user,omitempty"`
	Fireable    bool             `json:"fireable" doc:"Whether the console can fire an operation on this surface."`
	Routes      []simRouteBody   `json:"routes,omitempty"`
	Commands    []simCommandBody `json:"commands,omitempty"`
	OIDs        []simOIDBody     `json:"oids,omitempty"`
}

type componentSimOutput struct {
	Body struct {
		Simulated   bool                           `json:"simulated" doc:"The component has an endpoint on a node and a product a profile simulates."`
		Node        string                         `json:"node,omitempty"`
		Connected   bool                           `json:"connected" doc:"The node's simulator pulled recently."`
		Device      *simDeviceBody                 `json:"device,omitempty"`
		Product     string                         `json:"product,omitempty"`
		Profile     string                         `json:"profile,omitempty"`
		Address     string                         `json:"address,omitempty"`
		Summary     string                         `json:"summary,omitempty"`
		Variants    []string                       `json:"variants,omitempty" doc:"The products the same profile simulates; changing among them keeps the device's endpoints valid."`
		Impairments collection.SimImpairmentStatus `json:"impairments"`
		Surface     []simSurfaceBody             `json:"surface,omitempty" doc:"Every protocol surface the device serves, from its profile."`
	}
}

type fireSimInput struct {
	Name string `path:"name" doc:"The simulated component's name, or a dotted address"`
	Body struct {
		Surface   string `json:"surface" minLength:"1" doc:"The surface to fire on, by its name in the device's surface list"`
		Method    string `json:"method,omitempty" required:"false" doc:"HTTP: the method; GET when empty"`
		Path      string `json:"path,omitempty" required:"false" doc:"HTTP: the request path"`
		Body      string `json:"body,omitempty" required:"false" doc:"HTTP: the request body"`
		Command   string `json:"command,omitempty" required:"false" doc:"SSH, Telnet or TCP: the command line"`
		OID       string `json:"oid,omitempty" required:"false" doc:"SNMP: the OID to get"`
	}
}

type fireSimOutput struct {
	Body collection.SimOperationResult
}

type addSimDeviceInput struct {
	Name string `path:"name" doc:"The node whose simulator serves the new device"`
	Body struct {
		Name     string  `json:"name" minLength:"1" doc:"The new component's name"`
		Product  string  `json:"product" minLength:"1" doc:"A product a simulator profile simulates"`
		Location *string `json:"location,omitempty" required:"false" doc:"Where the component sits, by name or dotted path"`
	}
}

type addSimDeviceOutput struct {
	Body struct {
		Component string `json:"component"`
		Node      string `json:"node"`
		Address   string `json:"address" doc:"The loopback address the simulated device answers on."`
		Endpoints int    `json:"endpoints" doc:"How many endpoints were created for the collector to reach it."`
	}
}

// fireKind maps a surface's protocol to the operation the simulator runs.
func fireKind(protocol string) (string, bool) {
	switch protocol {
	case "http":
		return "http", true
	case "snmp":
		return "snmp", true
	case "ssh", "telnet", "tcp":
		return "command", true
	}
	return "", false
}

// simSurface renders a profile's protocol surfaces as the console lists them, with
// each port as the simulator binds it.
func simSurface(p *profile.DeviceProfile, portOffset int) []simSurfaceBody {
	shift := sim.PrivilegedPortShift(portOffset)
	out := make([]simSurfaceBody, 0, len(p.Interfaces))
	for _, iface := range p.Interfaces {
		port := iface.Port
		if shift != nil && port > 0 {
			port = shift(port)
		}
		_, fireable := fireKind(iface.Protocol)
		b := simSurfaceBody{Name: iface.Name, Protocol: iface.Protocol, Port: port, TLS: iface.TLS,
			Summary: iface.Doc.Summary, Persistent: iface.Persistent, Fireable: fireable}
		if iface.Auth.Type != "" {
			b.AuthType, b.DefaultUser = iface.Auth.Type, iface.Auth.DefaultUser
		}
		for _, r := range iface.Routes {
			method, path, ok := strings.Cut(r.Match, " ")
			if !ok {
				method, path = "GET", r.Match
			}
			b.Routes = append(b.Routes, simRouteBody{Method: method, Path: path, Summary: r.Doc.Summary,
				Example: r.Example, Private: r.Visibility == "private"})
		}
		for _, c := range iface.Commands {
			match := c.Match
			if c.MatchHex != "" {
				match = "hex " + c.MatchHex
			}
			b.Commands = append(b.Commands, simCommandBody{Match: match, Summary: c.Doc.Summary, Example: c.Example, Action: c.Action})
		}
		for path, oid := range iface.OIDMap {
			b.OIDs = append(b.OIDs, simOIDBody{OID: oid, Path: path})
		}
		sort.Slice(b.OIDs, func(i, j int) bool { return oidBefore(b.OIDs[i].OID, b.OIDs[j].OID) })
		out = append(out, b)
	}
	return out
}

// oidBefore orders OIDs arc by arc, numerically.
func oidBefore(a, b string) bool {
	as, bs := strings.Split(strings.TrimPrefix(a, "."), "."), strings.Split(strings.TrimPrefix(b, "."), ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			if len(as[i]) != len(bs[i]) {
				return len(as[i]) < len(bs[i])
			}
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

// variantsOf is the products a profile simulates, its own first.
func variantsOf(p *profile.DeviceProfile) []string {
	out := []string{p.Name}
	for _, v := range p.Products {
		out = append(out, v.Name)
	}
	return out
}

func registerSimFleetRoutes(api huma.API, a *authenticator, gw storage.Gateway, hub SimHub) {
	status := func(node string) (simhub.Status, bool) {
		if hub == nil {
			return simhub.Status{}, false
		}
		return hub.SimStatus(node)
	}

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-simulation",
		Method:      http.MethodGet,
		Path:        "/simulation",
		Summary:     "Get the simulated fleet",
		Description: "Every node a simulator has pulled for since the server started (that the caller may read), each with its capabilities, its account of impairments, and the devices it serves within the caller's component read scope, joined to their live hits. Connected is true when any simulator pulled recently. Hits are per server process, not stored. Gated by simulation:read.",
	}, "simulation", "read"), func(ctx context.Context, _ *struct{}) (*simFleetOutput, error) {
		out := &simFleetOutput{}
		out.Body.Nodes = []simNodeBody{}
		if hub == nil {
			return out, nil
		}
		now := time.Now()
		for _, node := range hub.Nodes() {
			if _, err := gw.GetNode(ctx, node, a.scopeFor(ctx, "node", "read")); err != nil {
				continue // a node the caller may not read is not listed
			}
			devices, err := gw.NodeSimDevices(ctx, node, a.scopeFor(ctx, "component", "read"))
			if err != nil {
				return nil, huma.Error500InternalServerError("get simulation")
			}
			st, seen := status(node)
			b := simulationBody(devices, st, seen, now).Body
			out.Body.Nodes = append(out.Body.Nodes, simNodeBody{Node: node, Connected: b.Connected, LastSeen: st.LastSeen,
				Capabilities: b.Capabilities, Impairments: b.Impairments, Devices: b.Devices})
			out.Body.Connected = out.Body.Connected || b.Connected
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-simulation-profiles",
		Method:      http.MethodGet,
		Path:        "/simulation/profiles",
		Summary:     "List the simulator's profiles",
		Description: "The simulator's profile catalog, embedded in the binary: each profile with the products it simulates and the protocols it speaks. Adding a simulated device names one of these products. Gated by simulation:read.",
	}, "simulation", "read"), func(ctx context.Context, _ *struct{}) (*simProfilesOutput, error) {
		store, err := simCatalog()
		if err != nil {
			return nil, huma.Error500InternalServerError("load simulator catalog")
		}
		out := &simProfilesOutput{}
		out.Body.Profiles = make([]simProfileBody, 0, len(store.Devices))
		for _, p := range store.Devices {
			protos := []string{}
			for _, iface := range p.Interfaces {
				protos = append(protos, iface.Protocol)
			}
			out.Body.Profiles = append(out.Body.Profiles, simProfileBody{Name: p.Name, Manufacturer: p.Manufacturer, Model: p.Model,
				DeviceType: p.DeviceType, Summary: p.Doc.Summary, Products: variantsOf(p), Protocols: protos})
		}
		sort.Slice(out.Body.Profiles, func(i, j int) bool { return out.Body.Profiles[i].Name < out.Body.Profiles[j].Name })
		return out, nil
	})

	// simulated resolves a component's simulation: its node, its device row
	// (within the read scope), and the profile that simulates its product.
	type simulated struct {
		id, node string
		device   storage.SimDevice
		prof     *profile.DeviceProfile
	}
	resolve := func(ctx context.Context, name string, action scope.Set) (*simulated, error) {
		id, err := gw.ResolveActionTarget(ctx, "component", name, a.scopeFor(ctx, "component", "read"), action)
		if err != nil {
			return nil, mapComponentErr(err)
		}
		node, err := gw.ComponentSimNode(ctx, id)
		if err != nil {
			return nil, huma.Error500InternalServerError("get component simulation")
		}
		if node == "" {
			return &simulated{id: id}, nil
		}
		devices, err := gw.NodeSimDevices(ctx, node, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("get component simulation")
		}
		s := &simulated{id: id, node: node}
		for _, d := range devices {
			if d.ComponentID == id {
				s.device = d
			}
		}
		store, err := simCatalog()
		if err != nil {
			return nil, huma.Error500InternalServerError("load simulator catalog")
		}
		if name, _, ok := store.ForProduct(s.device.Product); ok {
			s.prof = store.Devices[name]
		}
		return s, nil
	}

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-simulation",
		Method:      http.MethodGet,
		Path:        "/components/{name}/simulation",
		Summary:     "Get a component's simulation",
		Description: "Whether a component is simulated (it has an endpoint on a node and a product a simulator profile simulates) and, when it is, its node, address, live hits and impairment, the products its profile simulates, and its full API surface: every protocol surface, with its HTTP routes, commands and SNMP OIDs. A component that is not simulated reads simulated false. Gated by simulation:read; the component resolves within the caller's component read scope.",
	}, "simulation", "read"), func(ctx context.Context, in *impairmentPathInput) (*componentSimOutput, error) {
		s, err := resolve(ctx, in.Name, a.scopeFor(ctx, "simulation", "read"))
		if err != nil {
			return nil, err
		}
		out := &componentSimOutput{}
		if s.node == "" || s.prof == nil {
			return out, nil
		}
		st, seen := status(s.node)
		view := simulationBody([]storage.SimDevice{s.device}, st, seen, time.Now()).Body
		b := &out.Body
		b.Simulated, b.Node, b.Connected = true, s.node, view.Connected
		b.Device = &view.Devices[0]
		b.Product, b.Profile, b.Address, b.Summary = s.device.Product, s.prof.Name, s.device.Address, s.prof.Doc.Summary
		b.Variants, b.Impairments = variantsOf(s.prof), view.Impairments
		b.Surface = simSurface(s.prof, view.Capabilities.PortOffset)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "fire-component-simulation",
		Method:      http.MethodPost,
		Path:        "/components/{name}/simulation:fire",
		Summary:     "Fire an operation at a simulated device",
		Description: "Asks the simulator beside the component's node to run one operation against the device, on one of its protocol surfaces: an HTTP call, a command line (SSH, Telnet, TCP) or an SNMP get, and returns the device's answer. The device answers from its live state in process, as its listeners would, so a command's change sticks and the collector sees it; an impairment does not stop it. Audited. Gated by simulation:update, resolved on the component tier. A component that is not simulated is a 409, a surface it does not serve a 422, and no simulator answering a 503.",
	}, "simulation", "update"), func(ctx context.Context, in *fireSimInput) (*fireSimOutput, error) {
		s, err := resolve(ctx, in.Name, a.scopeFor(ctx, "simulation", "update"))
		if err != nil {
			return nil, err
		}
		if s.node == "" || s.prof == nil {
			return nil, huma.Error409Conflict(fmt.Sprintf("%s is not simulated: it needs an endpoint on a node and a product a simulator profile simulates", in.Name))
		}
		var iface *profile.Interface
		for i := range s.prof.Interfaces {
			if s.prof.Interfaces[i].Name == in.Body.Surface {
				iface = &s.prof.Interfaces[i]
			}
		}
		if iface == nil {
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("%s serves no surface %q", in.Name, in.Body.Surface))
		}
		kind, ok := fireKind(iface.Protocol)
		if !ok {
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("the console cannot fire on a %s surface", iface.Protocol))
		}
		op := collection.SimOperation{ComponentID: s.id, Kind: kind, Method: in.Body.Method, Path: in.Body.Path,
			Body: in.Body.Body, Command: in.Body.Command, Protocol: iface.Protocol, OID: in.Body.OID}
		if err := gw.AuditSimOperation(ctx, actorID(ctx), s.id, op); err != nil {
			return nil, huma.Error500InternalServerError("audit sim operation")
		}
		if hub == nil {
			return nil, huma.Error503ServiceUnavailable("no simulator answered")
		}
		res, err := hub.FireSimOperation(ctx, s.node, op)
		if errors.Is(err, collection.ErrNoSimulator) {
			return nil, huma.Error503ServiceUnavailable(fmt.Sprintf("no simulator beside %s answered", s.node))
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("fire sim operation")
		}
		return &fireSimOutput{Body: res}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "add-node-simulation-device",
		Method:        http.MethodPost,
		Path:          "/nodes/{name}/simulation/devices",
		DefaultStatus: http.StatusCreated,
		Summary:       "Add a simulated device",
		Description:   "Creates a component of a simulated product, at the lowest free address in the simulated block (127.60.0.0/16), with the endpoint the collector reaches it through placed on the node: a driver attach where the driver's inputs are known (an SNMP attach needs the sim-community secret, else it falls back), otherwise a reachability probe on a port the profile serves, lifted as the node's simulator lifts its ports. The simulator serves it from its next pull, with no restart. Gated by simulation:update, and also needs component:create and endpoint:create, each in the caller's own scope. A node with no simulator connected is a 409, and a product no profile simulates a 422.",
	}, "simulation", "update"), func(ctx context.Context, in *addSimDeviceInput) (*addSimDeviceOutput, error) {
		if !a.allows(ctx, "component", "create") || !a.allows(ctx, "endpoint", "create") {
			return nil, huma.Error403Forbidden("adding a simulated device also needs component:create and endpoint:create")
		}
		if _, err := gw.GetNode(ctx, in.Name, a.scopeFor(ctx, "node", "read")); err != nil {
			return nil, mapNodeErr(err)
		}
		st, seen := status(in.Name)
		if !seen || time.Since(st.LastSeen) > simConnectedWindow {
			return nil, huma.Error409Conflict(fmt.Sprintf("no simulator is connected beside %s; start one with omniglass sim run", in.Name))
		}
		store, err := simCatalog()
		if err != nil {
			return nil, huma.Error500InternalServerError("load simulator catalog")
		}
		if _, _, ok := store.ForProduct(in.Body.Product); !ok {
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("no simulator profile simulates the product %q", in.Body.Product))
		}
		products, err := gw.ListProducts(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list products")
		}
		drivers := map[string]string{}
		for _, p := range products {
			if p.DriverName != nil {
				drivers[p.Name] = *p.DriverName
			}
		}
		secretRead, canAdmin := a.scopeFor(ctx, "secret", "read"), a.canSecretAdmin(ctx, "read")
		attachable := func(driver string) bool {
			if driver != "snmp-generic" {
				return true
			}
			secrets, err := gw.ListSecrets(ctx, secretRead, canAdmin)
			if err != nil {
				return false
			}
			for _, s := range secrets {
				if s.Name == simfleet.CommunitySecret {
					return true
				}
			}
			return false
		}
		used, err := gw.SimAddressesInUse(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("plan simulated device")
		}
		product := in.Body.Product
		c, err := gw.CreateComponent(ctx, actorID(ctx), storage.ComponentSpec{Name: in.Body.Name, ProductName: &product, LocationName: in.Body.Location},
			a.scopeFor(ctx, "component", "create"), a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		plan, err := simfleet.Plan([]simfleet.Component{{ID: c.ID, Product: product}}, used, simfleet.Catalog{
			ProfileFor: func(p string) (*profile.DeviceProfile, bool) {
				name, _, ok := store.ForProduct(p)
				return store.Devices[name], ok
			},
			DriverOf:   func(p string) string { return drivers[p] },
			Attachable: attachable,
			PortOffset: st.Capabilities.PortOffset,
		})
		// The component stands only with its endpoints: a plan or endpoint
		// that fails takes the new component back out.
		undo := func() {
			all := scope.Set{All: true}
			_ = gw.DeleteComponent(context.WithoutCancel(ctx), actorID(ctx), c.Name, all, all)
		}
		if err != nil || len(plan) != 1 {
			undo()
			return nil, huma.Error409Conflict("no address is free in the simulated block")
		}
		node, name := in.Name, c.Name
		for _, e := range plan[0].Endpoints {
			spec := storage.EndpointSpec{Component: &name, Node: &node, SecretRead: secretRead, CanAdmin: canAdmin}
			if e.Driver != "" {
				driver := e.Driver
				spec.Driver, spec.Inputs = &driver, e.Inputs
			} else {
				spec.Transport, spec.Params = e.Transport, []byte(fmt.Sprintf(`{"target":%q}`, e.Target))
			}
			if _, err := gw.CreateEndpoint(ctx, actorID(ctx), spec, a.scopeFor(ctx, "endpoint", "create")); err != nil {
				undo()
				return nil, mapEndpointErr(err)
			}
		}
		out := &addSimDeviceOutput{}
		out.Body.Component, out.Body.Node, out.Body.Address, out.Body.Endpoints = c.Name, node, plan[0].Address, len(plan[0].Endpoints)
		return out, nil
	})
}
