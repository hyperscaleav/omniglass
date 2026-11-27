// Package spec is how a simulated device is defined, whichever way it is
// written. A YAML profile (sim/profile) and a Go device family (sim/device/*)
// both present one Definition, and a Catalog resolves a product to the one
// that serves it: a Go family when one claims the product, else its YAML
// profile (#2829, the first slice of the Go-families epic #2826).
//
// A family implements one wire API once, against a Spec, and its models are
// rows of a typed table: the five sizes of one display are five Models, not
// five packages. Its protocol handlers are Go funcs the engine's protocol
// servers call (HTTPHandler, LineHandler); its responses are typed Go values.
//
// This package is a leaf: it imports sim/profile and nothing of the engine, so
// a family imports only spec, and the engine imports spec to run what it
// defines.
package spec

import (
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// Identity is what a definition says about the product it simulates.
type Identity struct {
	Manufacturer string
	Model        string
	DeviceType   string
	Summary      string
}

// Model is one row of a family's model table: one product the family serves.
// The family writes its table in its own typed row (a struct with Diagonal,
// Resolution, PowerW, ...) and wraps each row with Table; the engine reads the
// identity and state here, and a handler reads its own row back with Row.
type Model struct {
	// Product is the catalog product the row simulates: the name a seeded
	// product, a component and a fleet snapshot carry.
	Product      string
	Manufacturer string
	Model        string
	DeviceType   string
	// Summary describes this product where the console lists it; the family's
	// own summary stands when empty.
	Summary string
	// State is deep-merged over the family's State for this product, the
	// per-model difference (its model name, serial prefix, size), the way a
	// YAML profile's products: overrides are.
	State map[string]any
	// Row is the family's own typed row, the one the table was written in.
	Row any
}

// Table wraps a family's typed rows as Models. core fills each row's catalog
// fields (Product and identity, State); Table keeps the row beside them.
func Table[T any](rows []T, core func(T) Model) []Model {
	out := make([]Model, len(rows))
	for i, r := range rows {
		m := core(r)
		m.Row = r
		out[i] = m
	}
	return out
}

// Row reads a model's own typed row back; ok is false for a model of another
// type (or none).
func Row[T any](m Model) (T, bool) {
	r, ok := m.Row.(T)
	return r, ok
}

// Device is the handle a protocol handler is given: the device it answers
// for, and its live state. It is the engine's device seen from a family; a
// family never imports the engine.
type Device interface {
	DeviceID() string
	Hostname() string
	// IP is the device's address as text.
	IP() string
	// Model is the table row of the product this device simulates; the zero
	// Model for a device of the family itself.
	Model() Model
	// Read is the value at a dotted state path, nil when none.
	Read(path string) any
	// Default is the value a state path holds in the device's defaults: what a
	// reset restores, with the device's own identity in it.
	Default(path string) any
	// State is a deep copy of the device's state.
	State() map[string]any
	// Write sets a state path as a request would: the device's version
	// advances and the change reaches everything that watches state (the diff
	// stream, a subscribed session, an event stream).
	Write(path string, value any) error
	// WriteMany sets several state paths as one change, as a YAML route's
	// mutations are applied: one version, one diff, and either every path is
	// written or none.
	WriteMany(changes ...surface.Change) error
	// Now is the time the device reads: the simulated world's clock, which a
	// handler stamps a reboot's anchor with.
	Now() time.Time
}

// HTTPRequest is one request as a handler sees it.
type HTTPRequest struct {
	Method string
	Path   string
	// Query is the first value of each query parameter.
	Query map[string]string
	// Body is the JSON-parsed request body, nil on an empty or non-JSON one.
	Body map[string]any
	// Raw is the body as sent.
	Raw []byte
}

// HTTPResponse is a handler's answer. A Body of string or []byte is sent
// as is; any other value is a typed Go value and is encoded with
// encoding/json. A zero Status is 200; a zero ContentType is JSON for an
// encoded value and text for a string or bytes.
type HTTPResponse struct {
	Status      int
	ContentType string
	Body        any
}

// JSON answers 200 with v encoded as JSON.
func JSON(v any) HTTPResponse { return HTTPResponse{Body: v} }

// Text answers 200 with s as text.
func Text(s string) HTTPResponse { return HTTPResponse{Body: s} }

// Failure answers with status and a plain message.
func Failure(status int, msg string) HTTPResponse {
	return HTTPResponse{Status: status, Body: msg}
}

// HTTPHandler answers one HTTP route.
type HTTPHandler func(d Device, r HTTPRequest) HTTPResponse

// HTTPRoute is one route a family serves on its http interfaces. Match is
// "METHOD /path", exactly as a YAML profile writes it.
type HTTPRoute struct {
	Match string
	// Summary and Example document the route where the console lists a
	// device's surface; Example is a request body for the operations panel.
	Summary string
	Example string
	// Sets are the state paths the route sets from a field of its body, each
	// to a fixed list of values: what the console offers as quick sets and
	// the simulator reads to tell a path driven away from its default. A route
	// built by httpapi.Control carries its own.
	Sets   []profile.Set
	Handle HTTPHandler
}

// LineHandler answers one command of a line protocol (SSH, Telnet, TCP, UDP)
// with the text to send back.
type LineHandler func(d Device) string

// Command is one command of a line protocol a family serves, matched exactly
// against the trimmed line, as a YAML profile's commands are.
type Command struct {
	Match   string
	Summary string
	Example string
	Handle  LineHandler
}

// Animator runs while its device serves: the engine calls it when the device
// starts and calls the stop it returns when the device stops. It is the seam
// the behavior primitives (sim/behave, #2830) plug into; a family with none
// leaves it nil.
type Animator func(d Device) (stop func())

// TickContext is what a Tick reads beside the device: the room the device
// stands in as the engine reads it (an idle room when it has no calendar), the
// simulated world's time, and the device's seeded random source, which the
// sim/behave primitives take.
type TickContext struct {
	Room calendar.RoomState
	Now  time.Time
	Rand behave.Source
	// Hours is the building's day the device stands in at Now: whether the lab
	// holds hours for it and whether it is inside them (behave.ReadBuildingHours
	// over behave.BuildingSpan).
	Hours behave.HoursReading
}

// Tick is a typed behavior of a Go family: a function that runs on an
// interval, reads the device's typed state and the room, calls sim/behave
// primitives, and returns the state paths it wants written. The engine applies
// them the way a YAML behavior's mutations are applied: only what changed is
// written, so the version advances, the diff stream and subscribed sessions
// see it, and a path an operator has overridden is left alone.
type Tick struct {
	Name     string
	Interval time.Duration
	// Writes are the state paths the tick may write, listed where the console
	// lists what a device changes by itself; a test holds a tick to them.
	Writes []string
	// OnStart runs the tick once as the device starts, before it serves, so
	// what it derives from the room is right from the first request.
	OnStart bool
	Run     func(d Device, c TickContext) []surface.Change
}

// Surface is the shared protocol surfaces (sim/surface, #2831) a family
// serves for one device: the helpers in sim/surface/{nvp,snmp,httpapi} build
// these from the device's typed state.
type Surface struct {
	// Commanders answer line-protocol commands, after the family's Commands.
	Commanders []surface.Commander
	// Routes are HTTP routes, tried before the family's HTTP routes.
	Routes []surface.Route
	// OIDs are SNMP scalars served beside the interface's OID map.
	OIDs []surface.OID
	// Subjects are the subscription subjects of a persistent interface, by
	// the interface's name: each face declares its own set.
	Subjects map[string][]surface.Subject
}

// SurfaceFunc builds a device's Surface. It runs once per device, as the
// device is built, so the surfaces close over that device's state (a
// surface's typed accessors read and write through d).
type SurfaceFunc func(d Device) Surface

// Handlers is what a family's protocol servers call.
type Handlers struct {
	HTTP     []HTTPRoute
	Commands []Command
	Animate  Animator
	Surface  SurfaceFunc
	Ticks    []Tick
}

// Spec is a Go device family: one API, its models as a table. A family
// package exports a func Spec() spec.Spec; the registry (sim/device, whose
// list make gen writes) collects them.
type Spec struct {
	// Name is the family's id: what a fleet snapshot carries as the profile
	// name, and a product of the same name resolves to the family itself.
	Name string
	// Identity is the family's own; a Model's identity overrides it.
	Identity Identity
	// Models is the table of products the family serves.
	Models []Model
	// Interfaces are the surfaces and ports the family serves. They declare
	// protocol, port, auth and sessions; what answers on them is Handlers.
	Interfaces []profile.Interface
	// State is the state tree with its defaults.
	State map[string]any
	// HTTP and Commands are the protocol handlers.
	HTTP     []HTTPRoute
	Commands []Command
	// Animate is the behavior seam (see Animator).
	Animate Animator
	// Ticks are the family's typed behaviors (see Tick).
	Ticks []Tick
	// Surface builds the shared protocol surfaces the family serves (see
	// SurfaceFunc).
	Surface SurfaceFunc
	// Elapsed are the read-derived uptimes the device reports (see Elapsed).
	Elapsed []Elapsed
	// Behaviors are YAML-style, run by the engine's existing behavior
	// machinery until the shared primitives replace them (#2830).
	Behaviors []profile.Behavior
}

// validate refuses a family the engine could not serve faithfully.
func (s Spec) validate() error {
	if s.Name == "" {
		return fmt.Errorf("a family has no name")
	}
	seen := map[string]bool{s.Name: true}
	for _, m := range s.Models {
		if m.Product == "" {
			return fmt.Errorf("family %q has a model with no product", s.Name)
		}
		if seen[m.Product] {
			return fmt.Errorf("family %q lists product %q twice (or as its own name)", s.Name, m.Product)
		}
		seen[m.Product] = true
	}
	if err := validateElapsed(s.Name, s.Elapsed); err != nil {
		return err
	}
	routes := map[string]bool{}
	for _, r := range s.HTTP {
		if routes[r.Match] {
			return fmt.Errorf("family %q lists route %q twice", s.Name, r.Match)
		}
		routes[r.Match] = true
		method, path, ok := strings.Cut(r.Match, " ")
		if !ok || method == "" || !strings.HasPrefix(path, "/") {
			return fmt.Errorf("family %q route %q: expected 'METHOD /path'", s.Name, r.Match)
		}
		if r.Handle == nil {
			return fmt.Errorf("family %q route %q has no handler", s.Name, r.Match)
		}
		if method != strings.ToUpper(method) {
			return fmt.Errorf("family %q route %q: the method is upper case", s.Name, r.Match)
		}
	}
	for _, t := range s.Ticks {
		if t.Name == "" || t.Run == nil || t.Interval <= 0 {
			return fmt.Errorf("family %q has a tick with no name, no func or no interval", s.Name)
		}
	}
	cmds := map[string]bool{}
	for _, c := range s.Commands {
		if c.Match == "" || c.Handle == nil {
			return fmt.Errorf("family %q has a command with no match or no handler", s.Name)
		}
		if cmds[c.Match] {
			return fmt.Errorf("family %q lists command %q twice", s.Name, c.Match)
		}
		cmds[c.Match] = true
	}
	if len(s.HTTP) > 0 && !hasProtocol(s.Interfaces, "http") {
		return fmt.Errorf("family %q has HTTP routes and no http interface", s.Name)
	}
	if len(s.Commands) > 0 && !hasProtocol(s.Interfaces, "ssh", "telnet", "tcp", "udp") {
		return fmt.Errorf("family %q has commands and no ssh, telnet, tcp or udp interface", s.Name)
	}
	return nil
}

func hasProtocol(ifaces []profile.Interface, protocols ...string) bool {
	for _, i := range ifaces {
		for _, p := range protocols {
			if i.Protocol == p {
				return true
			}
		}
	}
	return false
}

// listingDevice is the Device a family's SurfaceFunc is built against to list
// what it serves (the console's surface): it holds no state, so a surface that
// reads only as it is read lists without a device behind it.
type listingDevice struct{}

func (listingDevice) DeviceID() string                  { return "" }
func (listingDevice) Hostname() string                  { return "" }
func (listingDevice) IP() string                        { return "" }
func (listingDevice) Model() Model                      { return Model{} }
func (listingDevice) Read(string) any                   { return nil }
func (listingDevice) Default(string) any                { return nil }
func (listingDevice) State() map[string]any             { return map[string]any{} }
func (listingDevice) Write(string, any) error           { return nil }
func (listingDevice) WriteMany(...surface.Change) error { return nil }
func (listingDevice) Now() time.Time                    { return time.Time{} }
