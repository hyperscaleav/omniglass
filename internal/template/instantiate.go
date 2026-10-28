package template

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/systemplan"
)

// Instantiation (#827, ADR-0827): a component, a system or a subtree of
// places written from a template, as one storage.Plan the planned create
// lands whole. This file only reads (the catalog, and the fleet's own
// standards, products, types and drivers) and turns a template plus the
// caller's request into that plan; the write, the scope checks and the audit
// rows are storage.CreatePlan's, each piece the ordinary create.

// ErrInvalid is a request or a template the fleet cannot instantiate: a
// fill no product of the right type answers, a place type the fleet lacks or
// refuses, an attach with no node. Its message names what to fix.
var ErrInvalid = errors.New("template: cannot instantiate")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Inputs is one component's attach in a request: the driver inputs the
// caller supplies (the address, and any secret by name), laid over the
// template's preset.
type Inputs map[string]string

// ComponentRequest is POST /components:instantiate's input.
type ComponentRequest struct {
	Template string
	Name     string
	Label    string
	Location *string
	// Node places the endpoint; required when Inputs carries an address.
	Node   string
	Inputs Inputs
}

// PlannedPiece reports one component a plan creates: the role it fills (in
// a system), its product, and whether it attaches, so an answer can list
// what is still to attach.
type PlannedPiece struct {
	Role     string
	Product  string
	Attached bool
}

// componentPiece is the plan of one component from a component template or
// a bare product, attached when the supplied inputs carry an address.
func componentPiece(ctx context.Context, gw storage.Gateway, c *Catalog, compTemplate, product string, inputs Inputs, node string) (storage.PlannedComponent, PlannedPiece, error) {
	var preset map[string]string
	var values Values
	driver := ""
	if compTemplate != "" {
		t, err := c.GetKind(compTemplate, KindComponent)
		if err != nil {
			return storage.PlannedComponent{}, PlannedPiece{}, err
		}
		product, preset, values, driver = t.Component.Product, t.Component.Attach.Inputs, t.Component.Properties, t.Component.Attach.Driver
	}
	p := product
	pc := storage.PlannedComponent{Component: storage.ComponentSpec{ProductName: &p}}
	for _, name := range values.Names() {
		pc.Properties = append(pc.Properties, storage.PlannedValue{Name: name, Value: values[name]})
	}
	piece := PlannedPiece{Product: product}
	if inputs[AddressInput] == "" {
		if len(inputs) > 0 {
			return pc, piece, invalid("the inputs for a %s carry no %s, the address an attach needs", product, AddressInput)
		}
		return pc, piece, nil
	}
	if node == "" {
		return pc, piece, invalid("attaching a %s needs the node its endpoint is placed on", product)
	}
	if driver == "" {
		prod, err := gw.GetProduct(ctx, product)
		if errors.Is(err, storage.ErrTypeNotFound) {
			return pc, piece, invalid("product %s is not in this fleet's catalog", product)
		}
		if err != nil {
			return pc, piece, fmt.Errorf("template: read product %s: %w", product, err)
		}
		if prod.DriverName == nil {
			return pc, piece, invalid("product %s names no driver to attach", product)
		}
		driver = *prod.DriverName
	}
	merged := map[string]string{}
	for k, v := range preset {
		merged[k] = v
	}
	for k, v := range inputs {
		merged[k] = v
	}
	n := node
	pc.Endpoints = []storage.EndpointSpec{{Driver: &driver, Inputs: merged, Node: &n}}
	piece.Attached = true
	return pc, piece, nil
}

// PlanComponent turns a component template and a request into a plan of
// one standalone component.
func PlanComponent(ctx context.Context, gw storage.Gateway, c *Catalog, req ComponentRequest) (storage.Plan, PlannedPiece, error) {
	t, err := c.GetKind(req.Template, KindComponent)
	if err != nil {
		return storage.Plan{}, PlannedPiece{}, err
	}
	pc, piece, err := componentPiece(ctx, gw, c, t.Name, "", req.Inputs, req.Node)
	if err != nil {
		return storage.Plan{}, piece, err
	}
	pc.Component.Name, pc.Component.Label, pc.Component.LocationName = req.Name, req.Label, req.Location
	return storage.Plan{Template: t.Name, Components: []storage.PlannedComponent{pc}}, piece, nil
}

// SystemRequest is POST /systems:instantiate's input.
type SystemRequest struct {
	Template string
	// Standard is the standard to build to, the caller's own by name;
	// empty takes the template's, forked from its standard template in the
	// same write when the fleet holds no standard of that name.
	Standard string
	Name     string
	Label    string
	Location *string
	Node     string
	// Attach is the inputs per role, one entry per component of the role
	// in order; a component with none lands unattached.
	Attach map[string][]Inputs
}

// SystemPlan is a system template's plan: what CreatePlan lands, the
// standard it builds to (and whether the plan forks it), and a piece per
// component in the plan's order.
type SystemPlan struct {
	Plan     storage.Plan
	Standard string
	Forks    bool
	Pieces   []PlannedPiece
}

// PlanSystem turns a system template and a request into a plan of one
// system, its standard (forked when absent) and a component per role, each
// role filled to its quorum with the product the template names for it.
func PlanSystem(ctx context.Context, gw storage.Gateway, c *Catalog, req SystemRequest) (SystemPlan, error) {
	t, err := c.GetKind(req.Template, KindSystem)
	if err != nil {
		return SystemPlan{}, err
	}
	ps, out, err := plannedSystem(ctx, gw, c, t, req.Standard, req.Attach, req.Node)
	if err != nil {
		return out, err
	}
	ps.System.Name, ps.System.Label, ps.System.LocationName = req.Name, req.Label, req.Location
	out.Plan.Template = t.Name
	out.Plan.Systems = []storage.PlannedSystem{ps}
	for role := range req.Attach {
		if t.System.Roles[role] == (RoleFill{}) {
			return out, invalid("the request attaches role %s, which %s does not fill", role, t.Name)
		}
	}
	return out, nil
}

// plannedSystem plans one system from a system template: its standard
// (resolving the fork), the planner's fill of each role, and the
// components. It sets everything but the system's own name, label and
// place.
func plannedSystem(ctx context.Context, gw storage.Gateway, c *Catalog, t *Template, standard string, attach map[string][]Inputs, node string) (storage.PlannedSystem, SystemPlan, error) {
	out := SystemPlan{Standard: standard}
	var roles []systemplan.Role
	if out.Standard == "" {
		out.Standard = t.System.Standard
	}
	held, err := gw.GetStandard(ctx, out.Standard)
	switch {
	case err == nil:
		out.Standard = held.Name
		rs, err := gw.ListSystemRoles(ctx, "standard", held.Name)
		if err != nil {
			return storage.PlannedSystem{}, out, fmt.Errorf("template: read %s's roles: %w", held.Name, err)
		}
		for _, r := range rs {
			roles = append(roles, systemplan.Role{Name: r.Name, Quorum: r.Quorum, Capacity: r.Capacity,
				AcceptedTypes: r.AcceptedTypes, PinnedProducts: r.PinnedProducts, Alternate: r.Alternate})
		}
	case errors.Is(err, storage.ErrTypeNotFound) && standard == "":
		st, err := c.GetKind(t.System.Standard, KindStandard)
		if err != nil {
			return storage.PlannedSystem{}, out, err
		}
		chain, err := forkChain(ctx, gw, c, st)
		if err != nil {
			return storage.PlannedSystem{}, out, err
		}
		out.Plan.Standards = chain
		out.Forks = true
		for _, r := range st.Standard.Roles {
			alt := ""
			if r.Choice != "" {
				alt = r.Choice + "/" + r.Alternate
			}
			roles = append(roles, systemplan.Role{Name: r.Name, Quorum: r.Quorate(), Capacity: r.Capacity,
				AcceptedTypes: r.AcceptedTypes, PinnedProducts: r.PinnedProducts, Alternate: alt})
		}
	case errors.Is(err, storage.ErrTypeNotFound):
		return storage.PlannedSystem{}, out, fmt.Errorf("%w: standard %q", ErrNotFound, standard)
	default:
		return storage.PlannedSystem{}, out, fmt.Errorf("template: read standard %s: %w", out.Standard, err)
	}

	declared := map[string]bool{}
	for _, r := range roles {
		declared[r.Name] = true
	}
	fills := map[string]string{}
	for _, role := range sortedKeys(t.System.Roles) {
		if !declared[role] {
			return storage.PlannedSystem{}, out, invalid("%s fills role %s, which standard %s does not declare", t.Name, role, out.Standard)
		}
		f := t.System.Roles[role]
		if f.Component != "" {
			ct, err := c.GetKind(f.Component, KindComponent)
			if err != nil {
				return storage.PlannedSystem{}, out, err
			}
			fills[role] = ct.Component.Product
		} else {
			fills[role] = f.Product
		}
	}
	plan, err := planFills(ctx, gw, roles, fills)
	if err != nil {
		return storage.PlannedSystem{}, out, err
	}
	var short []string
	for _, f := range plan.Roles {
		if f.Count == 0 {
			short = append(short, fmt.Sprintf("%s (%s)", f.Role, f.Reason))
		}
	}
	if len(short) > 0 {
		return storage.PlannedSystem{}, out, invalid("%s cannot staff standard %s: %s", t.Name, out.Standard, strings.Join(short, "; "))
	}

	sysType := t.System.SystemType
	std := out.Standard
	ps := storage.PlannedSystem{System: storage.SystemSpec{StandardID: &std}}
	if sysType != "" {
		ps.System.SystemTypeID = &sysType
	}
	for _, name := range t.System.Properties.Names() {
		ps.Properties = append(ps.Properties, storage.PlannedValue{Name: name, Value: t.System.Properties[name]})
	}
	for _, f := range plan.Roles {
		if n := len(attach[f.Role]); n > f.Count {
			return storage.PlannedSystem{}, out, invalid("the request attaches %d components to role %s, which takes %d", n, f.Role, f.Count)
		}
		fill := t.System.Roles[f.Role]
		for i := 0; i < f.Count; i++ {
			var in Inputs
			if i < len(attach[f.Role]) {
				in = attach[f.Role][i]
			}
			pc, piece, err := componentPiece(ctx, gw, c, fill.Component, fill.Product, in, node)
			if err != nil {
				return storage.PlannedSystem{}, out, err
			}
			pc.Role, piece.Role = f.Role, f.Role
			ps.Components = append(ps.Components, pc)
			out.Pieces = append(out.Pieces, piece)
		}
	}
	return ps, out, nil
}

// planFills runs the shared planner with each role's one candidate, the
// product the template names for it.
func planFills(ctx context.Context, gw storage.Gateway, roles []systemplan.Role, fills map[string]string) (systemplan.SystemPlan, error) {
	products, err := gw.ListProducts(ctx)
	if err != nil {
		return systemplan.SystemPlan{}, fmt.Errorf("template: list products: %w", err)
	}
	types, err := gw.ListComponentTypes(ctx)
	if err != nil {
		return systemplan.SystemPlan{}, fmt.Errorf("template: list component types: %w", err)
	}
	byName := map[string]storage.ComponentType{}
	for _, ct := range types {
		byName[ct.Name] = ct
	}
	ps := make([]systemplan.Product, 0, len(products))
	known := map[string]bool{}
	for _, p := range products {
		ps = append(ps, systemplan.Product{Name: p.Name, ComponentType: p.ComponentType})
		known[p.Name] = true
	}
	for _, role := range sortedKeys(fills) {
		if !known[fills[role]] {
			return systemplan.SystemPlan{}, invalid("role %s is filled with product %s, which this fleet's catalog does not hold", role, fills[role])
		}
	}
	return systemplan.PlanSystem(roles, ps, systemplan.Catalog{
		Candidate: func(role, product string) bool { return fills[role] == product },
		Within: func(have, accepted string) bool {
			a, ok1 := byName[have]
			b, ok2 := byName[accepted]
			if !ok1 || !ok2 {
				return false
			}
			ok, err := gw.TypeIsWithin(ctx, a.ID, b.ID)
			return err == nil && ok
		},
		Noun: "template",
	}), nil
}

// PlaceRequest is POST /locations:instantiate's input.
type PlaceRequest struct {
	Template string
	// Parent is the place the subtree sits under; empty for a new top.
	Parent *string
	// Name and Label name the subtree's root; an empty name takes the
	// template's, else its type's name rule mints one.
	Name  string
	Label string
}

// PlacePlan is a place template's plan: what CreatePlan lands, and a piece
// per component of the systems it carries (none attached: a place template
// attaches nothing, so every component is listed as still to attach).
type PlacePlan struct {
	Plan   storage.Plan
	Pieces []PlannedPiece
}

// PlanPlaces turns a place template and a request into a plan of a subtree
// of places and the systems in them. Every place's type must be one the
// fleet holds, and each nested place's type must allow its parent's (the
// fleet's effective allowed_parent_types, a fork of a shipped type
// included); the root's placement under the parent is the create's own
// check. Nothing is keyed on any particular type.
func PlanPlaces(ctx context.Context, gw storage.Gateway, c *Catalog, req PlaceRequest) (PlacePlan, error) {
	t, err := c.GetKind(req.Template, KindPlace)
	if err != nil {
		return PlacePlan{}, err
	}
	out := PlacePlan{Plan: storage.Plan{Template: t.Name}}
	forks := map[string]bool{}
	var walk func(p *Place, key, parentKey, parentType string, root bool) error
	walk = func(p *Place, key, parentKey, parentType string, root bool) error {
		lt, err := gw.GetLocationType(ctx, p.Type)
		if errors.Is(err, storage.ErrTypeNotFound) {
			return invalid("%s names location type %s, which this fleet does not hold", t.Name, p.Type)
		}
		if err != nil {
			return fmt.Errorf("template: read location type %s: %w", p.Type, err)
		}
		if parentType != "" && len(lt.AllowedParentTypes) > 0 && !contains(lt.AllowedParentTypes, parentType) {
			return invalid("%s places a %s under a %s, which this fleet's %s type does not allow", t.Name, p.Type, parentType, p.Type)
		}
		name, label := p.Name, p.Label
		if root {
			if req.Name != "" {
				name = req.Name
			}
			if req.Label != "" {
				label = req.Label
			}
		}
		if name == "" && lt.NameRule == nil {
			return invalid("a %s needs a name: the type has no name rule to mint one", p.Type)
		}
		spec := storage.LocationSpec{Name: name, Label: label, LocationType: p.Type}
		if root {
			spec.ParentName = req.Parent
		}
		out.Plan.Places = append(out.Plan.Places, storage.PlannedPlace{Key: key, ParentKey: parentKey, Location: spec})
		for _, s := range p.Systems {
			st, err := c.GetKind(s.Template, KindSystem)
			if err != nil {
				return err
			}
			ps, sp, err := plannedSystem(ctx, gw, c, st, "", nil, "")
			if err != nil {
				return err
			}
			ps.PlaceKey, ps.System.Label = key, s.Label
			for _, f := range sp.Plan.Standards {
				if !forks[f.Name] {
					forks[f.Name] = true
					out.Plan.Standards = append(out.Plan.Standards, f)
				}
			}
			out.Plan.Systems = append(out.Plan.Systems, ps)
			out.Pieces = append(out.Pieces, sp.Pieces...)
		}
		for i := range p.Places {
			if err := walk(&p.Places[i], fmt.Sprintf("%s.%d", key, i), key, p.Type, false); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(t.Place, "0", "", "", true); err != nil {
		return out, err
	}
	return out, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// forkChain is the forks a system template's own standard needs, parents
// first: the standard template, and each ancestor template the fleet holds
// no standard of that name for. A variant's chain lands with it, since a
// system cannot be built to half of one.
func forkChain(ctx context.Context, gw storage.Gateway, c *Catalog, st *Template) ([]storage.StandardFork, error) {
	var chain []storage.StandardFork
	for cur := st; cur != nil; {
		f, err := StandardFork(cur, ForkOptions{})
		if err != nil {
			return nil, err
		}
		chain = append([]storage.StandardFork{f}, chain...)
		parent := cur.Standard.Parent
		if parent == "" {
			break
		}
		if _, err := gw.GetStandard(ctx, parent); err == nil {
			break
		} else if !errors.Is(err, storage.ErrTypeNotFound) {
			return nil, fmt.Errorf("template: read standard %s: %w", parent, err)
		}
		next, err := c.GetKind(parent, KindStandard)
		if err != nil {
			return nil, err
		}
		cur = next
		if len(chain) > 32 {
			return nil, invalid("the standard templates above %s form a cycle", st.Name)
		}
	}
	return chain, nil
}
