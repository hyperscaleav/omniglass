package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// Creating from a template (#827, ADR-0827): POST
// /components:createFromTemplate, /systems:createFromTemplate and
// /locations:createFromTemplate.
// Each is a custom method on the collection it creates in, gated by that
// resource's create permission, and each turns the template into one plan
// that storage.CreatePlan lands whole: every piece (a standard created from its template when
// absent, a location, a system, a component, its endpoint, a declared value, a
// slot assignment) is the ordinary create in the caller's own scope, with its
// own audit row naming the template, in one transaction. A refused piece is
// mapped as its own route maps it, and nothing lands.

// planScopes are the caller's scopes for every write a plan can make.
func planScopes(ctx context.Context, a *authenticator) storage.PlanScopes {
	use := a.secretBind(ctx)
	return storage.PlanScopes{
		LocationCreate: a.scopeFor(ctx, "location", "create"), LocationRead: a.scopeFor(ctx, "location", "read"),
		SystemCreate: a.scopeFor(ctx, "system", "create"), SystemRead: a.scopeFor(ctx, "system", "read"),
		SystemUpdate:    a.scopeFor(ctx, "system", "update"),
		ComponentCreate: a.scopeFor(ctx, "component", "create"), ComponentRead: a.scopeFor(ctx, "component", "read"),
		ComponentUpdate: a.scopeFor(ctx, "component", "update"),
		EndpointCreate:  a.scopeFor(ctx, "endpoint", "create"), SecretRead: use.Read, SecretBind: use.Bind,
		CanAdmin:          use.CanAdmin,
		CanCreateStandard: a.allows(ctx, "standard", "create"),
	}
}

// requireAlso refuses a call missing a permission a piece it creates needs,
// naming it, before anything is read: the gate stamps one permission per
// route, and the rest are this route's own (403).
func requireAlso(ctx context.Context, a *authenticator, what string, needs [][2]string) error {
	for _, n := range needs {
		if !a.allows(ctx, n[0], n[1]) {
			return huma.Error403Forbidden(fmt.Sprintf("%s also needs %s:%s", what, n[0], n[1]))
		}
	}
	return nil
}

// targetLocation resolves the location a component or a system lands in under the
// caller's location:read scope: a location outside it reads exactly as one that
// does not exist (a non-disclosing 404), before any plan is made.
func targetLocation(ctx context.Context, a *authenticator, gw storage.Gateway, ref *string) (*storage.Location, error) {
	if ref == nil || *ref == "" {
		return nil, nil
	}
	l, err := gw.GetLocation(ctx, *ref, a.scopeFor(ctx, "location", "read"))
	if err != nil {
		if errors.Is(err, storage.ErrLocationNotFound) || errors.Is(err, storage.ErrLocationForbidden) {
			return nil, huma.Error404NotFound(fmt.Sprintf("location %s not found", *ref))
		}
		return nil, mapLocationErr(err)
	}
	return l, nil
}

// mapFromTemplateErr maps a planning or a planned-create failure: a template
// or a named standard the catalog or fleet lacks is a 404, a request the
// template cannot satisfy a 422 naming why, and a refused piece what its own
// route answers.
func mapFromTemplateErr(err error) error {
	if errors.Is(err, template.ErrNotFound) {
		return huma.Error404NotFound(strings.TrimPrefix(err.Error(), "template: "))
	}
	if errors.Is(err, template.ErrInvalid) {
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), template.ErrInvalid.Error()+": "))
	}
	var pe *storage.PlanError
	if !errors.As(err, &pe) {
		return huma.Error500InternalServerError("create from template")
	}
	switch pe.Stage {
	case "standard":
		switch {
		case errors.Is(pe.Err, storage.ErrPlanStandardForbidden):
			return huma.Error403Forbidden(fmt.Sprintf("creating standard %s from its template also needs standard:create", pe.Ref))
		case errors.Is(pe.Err, storage.ErrTemplateParentMissing):
			return huma.Error422UnprocessableEntity(fmt.Sprintf("standard %s is a variant whose parent this fleet does not hold", pe.Ref))
		}
		return mapStandardErr(pe.Err)
	case "location":
		return mapLocationErr(pe.Err)
	case "component":
		return mapComponentErr(pe.Err)
	case "endpoint":
		return mapEndpointErr(pe.Err)
	case "slot":
		return mapSlotErr(pe.Err)
	case "property":
		return mapComponentPropertyErr(pe.Err)
	case "system-property":
		return mapSystemPropertyErr(pe.Err)
	}
	return mapSystemErr(pe.Err)
}

type createdComponentBody struct {
	Component   string `json:"component" doc:"The component's name"`
	ComponentID string `json:"component_id" doc:"The component's id, the stable address a link to it uses"`
	Slot        string `json:"slot,omitempty" doc:"The slot it fills in its system"`
	Product     string `json:"product" doc:"Its product"`
	Attached    bool   `json:"attached" doc:"Whether its endpoint attached from the inputs given; false lists it as still to attach"`
}

type createComponentFromTemplateInput struct {
	Body struct {
		Template string            `json:"template" minLength:"1" doc:"The component template, by name" example:"kestrel-vroom"`
		Location string            `json:"location,omitempty" doc:"The location the component sits in, by name or dotted path"`
		Name     string            `json:"name,omitempty" doc:"The component's name; minted as an unnamed create mints it when empty"`
		Label    string            `json:"label,omitempty" doc:"The component's label"`
		Outpost  string            `json:"outpost,omitempty" doc:"The outpost the endpoint is placed on; needed when inputs carry an address"`
		Inputs   map[string]string `json:"inputs,omitempty" doc:"Driver inputs laid over the template's preset: the address (host) attaches the endpoint, a secret input names a secret you may bind (secret:bind)"`
	}
}

type createComponentFromTemplateOutput struct {
	Body struct {
		Template    string `json:"template" doc:"The template it came from; recorded in the audit detail only"`
		Component   string `json:"component" doc:"The component's name"`
		ComponentID string `json:"component_id" doc:"The component's id, the stable address a link to it uses"`
		Product     string `json:"product" doc:"Its product"`
		Attached    bool   `json:"attached" doc:"Whether its endpoint attached from the inputs given; false means it is still to attach"`
	}
}

type createSystemFromTemplateInput struct {
	Body struct {
		Template string                         `json:"template" minLength:"1" doc:"The system template, by name" example:"mr55-system"`
		Location string                         `json:"location,omitempty" doc:"The location the system and its components sit in, by name or dotted path"`
		Name     string                         `json:"name,omitempty" doc:"The system's name; minted from its type when empty"`
		Label    string                         `json:"label,omitempty" doc:"The system's label"`
		Standard string                         `json:"standard,omitempty" doc:"A standard of your own to build to, by name; omitted takes the template's, created from its template in the same write when you hold none of that name"`
		Outpost  string                         `json:"outpost,omitempty" doc:"The outpost the endpoints are placed on; needed when any component attaches"`
		Attach   map[string][]map[string]string `json:"attach,omitempty" doc:"Driver inputs per slot, one object per component of the slot in order; an object with an address (host) attaches that component, the rest land unattached"`
	}
}

type createSystemFromTemplateOutput struct {
	Body struct {
		Template         string                 `json:"template" doc:"The template it came from; recorded in the audit detail only"`
		System           string                 `json:"system" doc:"The system's name"`
		SystemID         string                 `json:"system_id" doc:"The system's id, the stable address a link to it uses"`
		Standard         string                 `json:"standard" doc:"The standard it is built to"`
		StandardsCreated []string               `json:"standards_created" doc:"The standards this write created from their templates (parents first); empty when you held them"`
		Components       []createdComponentBody `json:"components" doc:"A component per slot, filled to quorum and assigned"`
		ToAttach         []string               `json:"to_attach" doc:"The components that landed with no endpoint, still to attach"`
	}
}

type createLocationsFromTemplateInput struct {
	Body struct {
		Template string `json:"template" minLength:"1" doc:"The location template, by name" example:"meeting-floor"`
		Parent   string `json:"parent,omitempty" doc:"The location the subtree sits under, by name or dotted path; omitted makes it a new top"`
		Name     string `json:"name,omitempty" doc:"The subtree root's name; omitted takes the template's, else its type's name rule mints one"`
		Label    string `json:"label,omitempty" doc:"The subtree root's label"`
	}
}

type createdLocationBody struct {
	Location   string `json:"location" doc:"The location's name"`
	LocationID string `json:"location_id" doc:"The location's id, the stable address a link to it uses"`
	Type       string `json:"type" doc:"Its location type"`
	Parent     string `json:"parent,omitempty" doc:"The location it sits under, by name"`
}

type createdSystemBody struct {
	System   string `json:"system" doc:"The system's name"`
	SystemID string `json:"system_id" doc:"The system's id, the stable address a link to it uses"`
	Location string `json:"location" doc:"The location it sits in"`
	Standard string `json:"standard" doc:"The standard it is built to"`
}

type createLocationsFromTemplateOutput struct {
	Body struct {
		Template         string                `json:"template" doc:"The template it came from; recorded in the audit detail only"`
		Locations        []createdLocationBody `json:"locations" doc:"The locations created, parents first"`
		Systems          []createdSystemBody   `json:"systems" doc:"The systems created in them"`
		StandardsCreated []string              `json:"standards_created" doc:"The standards this write created from their templates"`
		ToAttach         []string              `json:"to_attach" doc:"The components that landed with no endpoint, still to attach"`
	}
}

func createdStandardNames(made *storage.Planned) []string {
	out := []string{}
	for _, s := range made.CreatedStandards {
		out = append(out, s.Name)
	}
	return out
}

func registerCreateFromTemplateRoutes(api huma.API, a *authenticator, gw storage.Gateway, cat *template.Catalog) {
	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "create-component-from-template",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/components:createFromTemplate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Start a component from a template",
		Description: "Creates one component from a component template at a location: its product and the template's declared values, and, when the inputs carry an address (host), its endpoint attached through the driver with the template's preset under your inputs. A secret input names a secret you may bind (secret:bind), resolved as an attach resolves it. One transaction, every piece in your own scope with its own audit row carrying from_template; nothing points back at the template. " +
			"Gated by component:create; declared values also need component:update and an attach endpoint:create. A location outside your location scope is a 404, an unknown template a 404, inputs with no address or an attach with no outpost a 422.",
	}, "component", "create"), "secret", "bind"), func(ctx context.Context, in *createComponentFromTemplateInput) (*createComponentFromTemplateOutput, error) {
		t, err := cat.GetKind(in.Body.Template, template.KindComponent)
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		var needs [][2]string
		if len(t.Component.Properties) > 0 {
			needs = append(needs, [2]string{"component", "update"})
		}
		if in.Body.Inputs[template.AddressInput] != "" {
			needs = append(needs, [2]string{"endpoint", "create"})
		}
		if err := requireAlso(ctx, a, "starting a component from "+t.Name, needs); err != nil {
			return nil, err
		}
		loc := ptrOrNil(in.Body.Location)
		if _, err := targetLocation(ctx, a, gw, loc); err != nil {
			return nil, err
		}
		plan, piece, err := template.PlanComponent(ctx, gw, cat, template.ComponentRequest{
			Template: t.Name, Name: in.Body.Name, Label: in.Body.Label, Location: loc, Outpost: in.Body.Outpost, Inputs: in.Body.Inputs,
		})
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		made, err := gw.CreatePlan(ctx, actorID(ctx), plan, planScopes(ctx, a))
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		out := &createComponentFromTemplateOutput{}
		out.Body.Template = t.Name
		out.Body.Component, out.Body.ComponentID = made.Components[0].Name, made.Components[0].ID
		out.Body.Product, out.Body.Attached = piece.Product, piece.Attached
		return out, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "create-system-from-template",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/systems:createFromTemplate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Start a whole system from a template",
		Description: "Creates a system from a system template at a location, built to the standard you name or the template's own (yours of that name when you hold one, else created from its standard template, with any parent it hangs off, in the same write), with a component for each slot filled to its quorum and assigned, and the declared values. A component whose slot you give an address in attach lands attached; the rest land unattached and are listed in to_attach. One transaction: it lands whole or not at all, every piece in your own scope with its own audit row carrying from_template. " +
			"Gated by system:create, and also needs system:read, system:update, component:create and component:update; endpoint:create when anything attaches, standard:create when a standard is created from its template. A location outside your location scope is a 404, an unknown template or standard a 404, a standard the template cannot staff a 422 naming the slots.",
	}, "system", "create"), "secret", "bind"), func(ctx context.Context, in *createSystemFromTemplateInput) (*createSystemFromTemplateOutput, error) {
		t, err := cat.GetKind(in.Body.Template, template.KindSystem)
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		needs := [][2]string{{"system", "read"}, {"system", "update"}, {"component", "create"}, {"component", "update"}}
		attach := map[string][]template.Inputs{}
		for slot, list := range in.Body.Attach {
			for _, inputs := range list {
				attach[slot] = append(attach[slot], template.Inputs(inputs))
				if inputs[template.AddressInput] != "" && !containsNeed(needs, "endpoint", "create") {
					needs = append(needs, [2]string{"endpoint", "create"})
				}
			}
		}
		if err := requireAlso(ctx, a, "starting a system from "+t.Name, needs); err != nil {
			return nil, err
		}
		loc := ptrOrNil(in.Body.Location)
		if _, err := targetLocation(ctx, a, gw, loc); err != nil {
			return nil, err
		}
		sp, err := template.PlanSystem(ctx, gw, cat, template.SystemRequest{
			Template: t.Name, Standard: in.Body.Standard, Name: in.Body.Name, Label: in.Body.Label,
			Location: loc, Outpost: in.Body.Outpost, Attach: attach,
		})
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		made, err := gw.CreatePlan(ctx, actorID(ctx), sp.Plan, planScopes(ctx, a))
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		out := &createSystemFromTemplateOutput{}
		out.Body.Template, out.Body.Standard, out.Body.StandardsCreated = t.Name, sp.Standard, createdStandardNames(made)
		out.Body.System, out.Body.SystemID = made.Systems[0].System.Name, made.Systems[0].System.ID
		out.Body.Components, out.Body.ToAttach = []createdComponentBody{}, []string{}
		for i, c := range made.Systems[0].Components {
			p := sp.Pieces[i]
			out.Body.Components = append(out.Body.Components, createdComponentBody{Component: c.Name, ComponentID: c.ID, Slot: p.Slot, Product: p.Product, Attached: p.Attached})
			if !p.Attached {
				out.Body.ToAttach = append(out.Body.ToAttach, c.Name)
			}
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-locations-from-template",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/locations:createFromTemplate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Start a subtree of locations from a template",
		Description: "Creates a subtree of locations from a location template under a parent location (or as a new top), each location's type checked against its parent type's allowed parents as this fleet has them (your fork of a shipped type included), each named by the template, the request (the root) or its type's name rule, and the systems the template puts in them, staffed as a system template is, unattached. One transaction: all of it or none, every piece in your own scope with its own audit row carrying from_template. " +
			"Gated by location:create; when the subtree carries systems it also needs system:create, system:read, system:update, component:create and component:update, and standard:create when a standard is created from its template. A type this fleet does not hold, or a placement its types refuse, is a 422 naming the type or the pair.",
	}, "location", "create"), func(ctx context.Context, in *createLocationsFromTemplateInput) (*createLocationsFromTemplateOutput, error) {
		t, err := cat.GetKind(in.Body.Template, template.KindLocation)
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		if locationCarriesSystems(t.Location) {
			if err := requireAlso(ctx, a, "starting locations from "+t.Name, [][2]string{{"system", "create"}, {"system", "read"}, {"system", "update"}, {"component", "create"}, {"component", "update"}}); err != nil {
				return nil, err
			}
		}
		// The parent is resolved under location:read first, as the other two
		// routes resolve their target: a parent the caller cannot read is a
		// non-disclosing 404, never a 403 that says it exists. The plan
		// addresses it by its id from here on.
		parent, err := targetLocation(ctx, a, gw, ptrOrNil(in.Body.Parent))
		if err != nil {
			return nil, err
		}
		var parentRef *string
		if parent != nil {
			id := parent.ID
			parentRef = &id
		}
		pp, err := template.PlanLocations(ctx, gw, cat, template.LocationRequest{
			Template: t.Name, Parent: parentRef, Name: in.Body.Name, Label: in.Body.Label,
		})
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		made, err := gw.CreatePlan(ctx, actorID(ctx), pp.Plan, planScopes(ctx, a))
		if err != nil {
			return nil, mapFromTemplateErr(err)
		}
		out := &createLocationsFromTemplateOutput{}
		out.Body.Template, out.Body.StandardsCreated = t.Name, createdStandardNames(made)
		out.Body.Locations, out.Body.Systems, out.Body.ToAttach = []createdLocationBody{}, []createdSystemBody{}, []string{}
		names := map[string]string{}
		for _, l := range made.Locations {
			names[l.ID] = l.Name
		}
		for _, l := range made.Locations {
			pb := createdLocationBody{Location: l.Name, LocationID: l.ID, Type: l.LocationType}
			if l.ParentID != nil {
				pb.Parent = names[*l.ParentID]
				if pb.Parent == "" && parent != nil {
					pb.Parent = parent.Name
				}
			}
			out.Body.Locations = append(out.Body.Locations, pb)
		}
		i := 0
		for si, s := range made.Systems {
			std := ""
			if s.System.StandardName != nil {
				std = *s.System.StandardName
			}
			loc := ""
			if l := locationOfSystem(pp.Plan, made, si); l != nil {
				loc = l.Name
			}
			out.Body.Systems = append(out.Body.Systems, createdSystemBody{System: s.System.Name, SystemID: s.System.ID, Location: loc, Standard: std})
			for _, c := range s.Components {
				if i < len(pp.Pieces) && !pp.Pieces[i].Attached {
					out.Body.ToAttach = append(out.Body.ToAttach, c.Name)
				}
				i++
			}
		}
		return out, nil
	})
}

func containsNeed(needs [][2]string, res, action string) bool {
	for _, n := range needs {
		if n[0] == res && n[1] == action {
			return true
		}
	}
	return false
}

func locationCarriesSystems(p *template.Location) bool {
	if len(p.Systems) > 0 {
		return true
	}
	for i := range p.Locations {
		if locationCarriesSystems(&p.Locations[i]) {
			return true
		}
	}
	return false
}

// locationOfSystem is the created location the plan's si-th system sits in.
func locationOfSystem(plan storage.Plan, made *storage.Planned, si int) *storage.Location {
	key := plan.Systems[si].LocationKey
	for i, p := range plan.Locations {
		if p.Key == key && i < len(made.Locations) {
			return made.Locations[i]
		}
	}
	return nil
}
