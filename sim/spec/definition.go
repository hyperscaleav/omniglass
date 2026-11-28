package spec

import (
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// Definition is what every consumer of the simulator catalog reads: the
// engine, the simulated fleet's planner, the dev seed, the lab scenario's
// catalog, the footprint sweep and the simulation routes. A Go family is the
// one thing that satisfies it, and a consumer reads the interface so a test
// can stand a definition of its own beside the shipped ones.
type Definition interface {
	// ID names the definition: the family name a fleet snapshot carries.
	ID() string
	Identity() Identity
	// Surfaces are the interfaces and ports it serves.
	Surfaces() []wire.Interface
	// InitialState is its state tree with the defaults; callers do not
	// modify it.
	InitialState() map[string]any
	// Products is every product it simulates: its model products, never the
	// family's own name, which is the wire API it serves, not a product.
	Products() []string
	// ElapsedList is the read-derived uptimes it reports.
	ElapsedList() []Elapsed
	// PushFace and WebhookFace are the outbound faces it declares, nil when
	// it has none.
	PushFace() *PushFace
	WebhookFace() *wire.WebhookSubscribe
}

// Served is a Definition whose protocol servers call Go: a family.
type Served interface {
	Definition
	Handlers() Handlers
}

// Resolved is what a product resolves to.
type Resolved struct {
	Definition Definition
	Product    string
	// Identity is this product's: its model row's for a family, the
	// definition's for a profile.
	Identity Identity
	// Overlay is deep-merged over the definition's InitialState for this
	// product, nil for the definition's own name.
	Overlay map[string]any
	// Model is the product's table row, the zero Model for a profile or for
	// a family's own name.
	Model Model
}

// Family is a validated Spec presented as a Definition.
type Family struct{ s Spec }

// NewFamily validates a Spec.
func NewFamily(s Spec) (*Family, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &Family{s: s}, nil
}

func (f *Family) ID() string                          { return f.s.Name }
func (f *Family) Identity() Identity                  { return f.s.Identity }
func (f *Family) InitialState() map[string]any        { return f.s.State }
func (f *Family) ElapsedList() []Elapsed              { return f.s.Elapsed }
func (f *Family) WebhookFace() *wire.WebhookSubscribe { return f.s.Webhook }

// PushFace is the family's push face, nil when it reports nothing.
func (f *Family) PushFace() *PushFace { return f.s.Push }

// Surfaces are the family's interfaces, each listing the routes and commands
// its protocol answers: the listing the console shows, built from the
// family's own declarations so it cannot differ from what answers.
func (f *Family) Surfaces() []wire.Interface {
	out := make([]wire.Interface, len(f.s.Interfaces))
	for i, iface := range f.s.Interfaces {
		iface.Routes = append([]wire.Route(nil), iface.Routes...)
		iface.Commands = append([]wire.Command(nil), iface.Commands...)
		switch iface.Protocol {
		case "snmp":
			iface.OIDs = f.nativeOIDs()
		case "http":
			for _, r := range f.s.HTTP {
				route := wire.Route{Match: r.Match, Doc: wire.Doc{Summary: r.Summary}, Example: r.Example, Sets: r.Sets}
				if r.Private {
					route.Visibility = "private"
				}
				iface.Routes = append(iface.Routes, route)
			}
		case "ssh", "telnet", "tcp", "udp":
			for _, c := range f.s.Commands {
				if c.Face != "" && c.Face != iface.Name {
					continue
				}
				iface.Commands = append(iface.Commands, wire.Command{Match: c.Match, Doc: wire.Doc{Summary: c.Summary}, Example: c.Example})
			}
			iface.Commands = append(iface.Commands, f.surfaceCommands(iface.Name)...)
		}
		out[i] = iface
	}
	return out
}

// surfaceCommands is the lines the family's surface answers, as the console
// lists them under the interface the surface names: each command with the
// subscription verb it carries. The surface is built against a device that
// holds nothing, so asking it to answer a line writes nowhere.
func (f *Family) surfaceCommands(iface string) []wire.Command {
	if f.s.Surface == nil {
		return nil
	}
	sf := f.s.Surface(listingDevice{})
	if sf.ListedOn != iface {
		return nil
	}
	var out []wire.Command
	for _, c := range sf.Commanders {
		for _, line := range c.Commands() {
			if !typed(line) {
				// A binary frame (a hex poll) is no line an operator types; the
				// family lists it on its own band.
				continue
			}
			cmd := wire.Command{Match: line}
			if d, ok := c.(surface.Described); ok {
				cmd.Doc.Summary = d.Summary(line)
			}
			if reply, ok := c.Handle(line); ok {
				cmd.Action = string(reply.Action)
			}
			out = append(out, cmd)
		}
	}
	return out
}

// typed reports whether a command line is printable text, as an operator
// types it.
func typed(line string) bool {
	for _, r := range line {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// nativeOIDs is the OIDs the family's surface serves, as the console lists
// them: each state path it reads with its OID. The surface is built against a
// device that holds nothing, so a SurfaceFunc reads state only when an OID is
// read, never as it is built; an OID that reads no one path is not listed.
func (f *Family) nativeOIDs() map[string]string {
	if f.s.Surface == nil {
		return nil
	}
	out := map[string]string{}
	for _, o := range f.s.Surface(listingDevice{}).OIDs {
		if o.Path != "" {
			out[o.Path] = o.OID
		}
	}
	return out
}

func (f *Family) Handlers() Handlers {
	return Handlers{HTTP: f.s.HTTP, Commands: f.s.Commands, Animate: f.s.Animate, Surface: f.s.Surface, Ticks: f.s.Ticks, Push: f.s.Push}
}

// Products lists the products the family simulates, its model rows in order.
// The family's own name is the wire API it serves, not a product a component
// could be assigned, so it is not listed (#2846 review).
func (f *Family) Products() []string {
	out := make([]string, 0, len(f.s.Models))
	for _, m := range f.s.Models {
		out = append(out, m.Product)
	}
	return out
}

// Catalog resolves products and definition ids over the Go families: a product
// resolves to the family that claims it, by its name or one of its models.
type Catalog struct {
	defs     map[string]Definition
	products map[string]Resolved
}

// NewCatalog builds the catalog over the families. A family may not share its
// name with another definition, nor claim a product another family claims.
func NewCatalog(families ...Spec) (*Catalog, error) {
	c := &Catalog{defs: map[string]Definition{}, products: map[string]Resolved{}}
	claimed := map[string]string{}
	for _, s := range families {
		f, err := NewFamily(s)
		if err != nil {
			return nil, err
		}
		if _, clash := c.defs[f.ID()]; clash {
			return nil, fmt.Errorf("family %q has the name of another definition", f.ID())
		}
		c.defs[f.ID()] = f
		claim := func(product string, r Resolved) error {
			if prev, dup := claimed[product]; dup {
				return fmt.Errorf("product %q is claimed by both families %q and %q", product, prev, f.ID())
			}
			claimed[product] = f.ID()
			c.products[product] = r
			return nil
		}
		if err := claim(f.ID(), Resolved{Definition: f, Product: f.ID(), Identity: f.Identity()}); err != nil {
			return nil, err
		}
		for _, m := range s.Models {
			id := f.Identity()
			if m.Manufacturer != "" {
				id.Manufacturer = m.Manufacturer
			}
			if m.Model != "" {
				id.Model = m.Model
			}
			if m.DeviceType != "" {
				id.DeviceType = m.DeviceType
			}
			if m.Summary != "" {
				id.Summary = m.Summary
			}
			r := Resolved{Definition: f, Product: m.Product, Identity: id, Overlay: m.State, Model: m}
			if m.Product == f.ID() {
				// A family of one, named for its product: the row is the
				// family's own entry.
				c.products[m.Product] = r
				continue
			}
			if err := claim(m.Product, r); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

// Lookup is the definition with this id, the profile name a snapshot carries.
func (c *Catalog) Lookup(id string) (Definition, bool) {
	d, ok := c.defs[id]
	return d, ok
}

// ForProduct resolves the definition that simulates a product.
func (c *Catalog) ForProduct(product string) (Resolved, bool) {
	r, ok := c.products[product]
	return r, ok
}

// ProfileFor is the definition that simulates a product: the planner's lookup.
func (c *Catalog) ProfileFor(product string) (Definition, bool) {
	r, ok := c.products[product]
	return r.Definition, ok
}

// Definitions lists every definition by id.
func (c *Catalog) Definitions() []Definition {
	out := make([]Definition, 0, len(c.defs))
	for _, d := range c.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
