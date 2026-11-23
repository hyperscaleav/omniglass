package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/template"
)

// Applying a plan (#2368, ADR-2368): POST /plans:apply, the planned-create
// primitive (storage.CreatePlan, ADR-0827) exposed to a client directly. A
// client submits the plan it built (locations with parents, standard
// templates, systems with slot-assigned components, standalone components,
// declared values) and it lands in one transaction under the caller's own
// scope, every piece the ordinary create with its own audit row, every row
// stamped from_import with the one import id the response returns.
// validate_only runs the same transaction and rolls it back, so a dry run
// refuses exactly what the apply would.

// planRowLimit is the most rows one plan may carry (each standard, location,
// system, component and declared value is a row). A plan is one transaction,
// so a bigger import is chunked by the client (by building), each chunk its
// own plan and its own import id; the limit keeps a chunk's lock span and the
// request body bounded.
const planRowLimit = 1000

type planValueBody struct {
	Name  string `json:"name" minLength:"1" doc:"The property, by name"`
	Value any    `json:"value" doc:"The declared value, shaped by the property's data type"`
}

type planLocationBody struct {
	Key       string `json:"key" minLength:"1" doc:"The plan's own handle for this location, which a later location's parent_key or a system's or component's location_key names; never stored"`
	ParentKey string `json:"parent_key,omitempty" doc:"The key of an earlier location of this plan this one sits under"`
	Parent    string `json:"parent,omitempty" doc:"An existing location this one sits under, by name, dotted path or id; omitted with parent_key makes it a new top"`
	Name      string `json:"name,omitempty" doc:"The location's name; minted by its type's name rule when empty"`
	Label     string `json:"label,omitempty" doc:"The location's label"`
	Type      string `json:"type" minLength:"1" doc:"Its location type" example:"building"`
}

type planSlotComponentBody struct {
	Slot       string          `json:"slot" minLength:"1" doc:"The slot of the system it fills; a component in no system is a standalone component"`
	Name       string          `json:"name,omitempty" doc:"The component's name; minted when empty"`
	Label      string          `json:"label,omitempty" doc:"The component's label"`
	Product    string          `json:"product" minLength:"1" doc:"Its product, by name"`
	Properties []planValueBody `json:"properties,omitempty" doc:"Values declared on the component"`
}

type planSystemBody struct {
	LocationKey string                  `json:"location_key,omitempty" doc:"The key of a location of this plan the system sits in"`
	Location    string                  `json:"location,omitempty" doc:"An existing location the system sits in, by name, dotted path or id"`
	Name        string                  `json:"name,omitempty" doc:"The system's name; minted from its type when empty"`
	Label       string                  `json:"label,omitempty" doc:"The system's label"`
	Standard    string                  `json:"standard,omitempty" doc:"The standard it is built to, by name: one the fleet holds or one this plan's standards create"`
	Type        string                  `json:"type,omitempty" doc:"Its system type, by name"`
	Components  []planSlotComponentBody `json:"components,omitempty" doc:"The components that staff it; each sits where the system sits"`
	Properties  []planValueBody         `json:"properties,omitempty" doc:"Values declared on the system"`
}

type planComponentBody struct {
	LocationKey string          `json:"location_key,omitempty" doc:"The key of a location of this plan the component sits in"`
	Location    string          `json:"location,omitempty" doc:"An existing location the component sits in, by name, dotted path or id"`
	Name        string          `json:"name,omitempty" doc:"The component's name; minted when empty"`
	Label       string          `json:"label,omitempty" doc:"The component's label"`
	Product     string          `json:"product" minLength:"1" doc:"Its product, by name"`
	Properties  []planValueBody `json:"properties,omitempty" doc:"Values declared on the component"`
}

type applyPlanInput struct {
	Body struct {
		ValidateOnly bool                `json:"validate_only,omitempty" doc:"Run the whole plan and roll it back: the response says what an apply would create, or the piece that would refuse it, and nothing is written"`
		Standards    []string            `json:"standards,omitempty" doc:"Standard templates to create standards from, by name, each with any parent it hangs off; one the fleet already holds is used as it is"`
		Locations    []planLocationBody  `json:"locations,omitempty" doc:"The locations to create, in order: a parent before its children"`
		Systems      []planSystemBody    `json:"systems,omitempty" doc:"The systems to create, each with the components that staff it"`
		Components   []planComponentBody `json:"components,omitempty" doc:"Standalone components, in no system"`
	}
}

type planCountsBody struct {
	StandardsCreated int `json:"standards_created" doc:"Standards created from their templates"`
	Locations        int `json:"locations" doc:"Locations created"`
	Systems          int `json:"systems" doc:"Systems created"`
	Components       int `json:"components" doc:"Components created, in systems and standalone"`
}

type appliedLocationBody struct {
	Key        string `json:"key" doc:"The plan's key for it"`
	Location   string `json:"location" doc:"The location's name"`
	LocationID string `json:"location_id,omitempty" doc:"The location's id; omitted on a dry run, which keeps nothing"`
	Parent     string `json:"parent,omitempty" doc:"The location it sits under"`
}

type appliedComponentBody struct {
	Component   string `json:"component" doc:"The component's name"`
	ComponentID string `json:"component_id,omitempty" doc:"The component's id; omitted on a dry run"`
	Slot        string `json:"slot,omitempty" doc:"The slot it fills in its system"`
	Product     string `json:"product" doc:"Its product"`
}

type appliedSystemBody struct {
	System     string                 `json:"system" doc:"The system's name"`
	SystemID   string                 `json:"system_id,omitempty" doc:"The system's id; omitted on a dry run"`
	Standard   string                 `json:"standard,omitempty" doc:"The standard it is built to"`
	Components []appliedComponentBody `json:"components" doc:"The components that staff it, in plan order"`
}

type applyPlanOutput struct {
	Status int
	Body   struct {
		ImportID         string                 `json:"import_id,omitempty" doc:"The import every audit row of this apply carries as from_import; omitted on a dry run"`
		ValidateOnly     bool                   `json:"validate_only" doc:"Whether this was a dry run that wrote nothing"`
		Counts           planCountsBody         `json:"counts" doc:"How many of each piece the plan created (or would create)"`
		StandardsCreated []string               `json:"standards_created" doc:"The standards created from their templates, parents first"`
		Locations        []appliedLocationBody  `json:"locations" doc:"The locations, in plan order"`
		Systems          []appliedSystemBody    `json:"systems" doc:"The systems, in plan order"`
		Components       []appliedComponentBody `json:"components" doc:"The standalone components, in plan order"`
	}
}

// planRows counts the rows a plan body carries, the measure planRowLimit
// bounds. Pure.
func planRows(in *applyPlanInput) int {
	n := len(in.Body.Standards) + len(in.Body.Locations) + len(in.Body.Systems) + len(in.Body.Components)
	for _, s := range in.Body.Systems {
		n += len(s.Properties) + len(s.Components)
		for _, c := range s.Components {
			n += len(c.Properties)
		}
	}
	for _, c := range in.Body.Components {
		n += len(c.Properties)
	}
	return n
}

// planNeeds is every permission beyond the route's gate a plan's pieces need,
// in a fixed order, so a caller missing one is refused before anything is
// read: each piece needs what its ordinary create needs. Pure.
func planNeeds(in *applyPlanInput) [][2]string {
	var needs [][2]string
	add := func(res, action string) {
		if !containsNeed(needs, res, action) {
			needs = append(needs, [2]string{res, action})
		}
	}
	if len(in.Body.Locations) > 0 {
		add("location", "create")
	}
	for _, s := range in.Body.Systems {
		add("system", "create")
		if len(s.Properties) > 0 {
			add("system", "update")
		}
		for range s.Components {
			add("component", "create")
			add("system", "read")
			add("system", "update")
			add("component", "read")
			add("component", "update")
		}
	}
	for _, c := range in.Body.Components {
		add("component", "create")
		if len(c.Properties) > 0 {
			add("component", "update")
		}
	}
	return needs
}

// planPlace resolves where a system or a component of the request sits: a
// location key of the plan, or an existing location; naming both is a 422.
func planPlace(path, key, given string) (string, *string, error) {
	if key != "" && given != "" {
		return "", nil, huma.Error422UnprocessableEntity(path + ": give location_key or location, not both")
	}
	return key, ptrOrNil(given), nil
}

func plannedValues(path string, vs []planValueBody) ([]storage.PlannedValue, error) {
	out := make([]storage.PlannedValue, 0, len(vs))
	for i, v := range vs {
		raw, err := encodePropertyJSON(v.Value, fmt.Sprintf("%s.properties[%d].value", path, i))
		if err != nil {
			return nil, err
		}
		out = append(out, storage.PlannedValue{Name: v.Name, Value: raw})
	}
	return out, nil
}

func plannedComponent(path, slot, locationKey string, location *string, name, label, product string, props []planValueBody) (storage.PlannedComponent, error) {
	vals, err := plannedValues(path, props)
	if err != nil {
		return storage.PlannedComponent{}, err
	}
	prod := product
	return storage.PlannedComponent{
		Slot: slot, LocationKey: locationKey, Properties: vals,
		Component: storage.ComponentSpec{Name: name, Label: label, ProductName: &prod, LocationName: location},
	}, nil
}

// toPlan turns the request into the storage plan: the shape only, with the
// standard templates resolved from the catalog. What a piece may do is
// CreatePlan's, under the caller's scope.
func toPlan(ctx context.Context, gw storage.Gateway, cat *template.Catalog, in *applyPlanInput) (storage.Plan, error) {
	plan := storage.Plan{ValidateOnly: in.Body.ValidateOnly}
	for _, name := range in.Body.Standards {
		chain, err := template.PlanStandards(ctx, gw, cat, name)
		if err != nil {
			return plan, err
		}
		plan.Standards = append(plan.Standards, chain...)
	}
	for i, l := range in.Body.Locations {
		if l.ParentKey != "" && l.Parent != "" {
			return plan, huma.Error422UnprocessableEntity(fmt.Sprintf("locations[%d]: give parent_key or parent, not both", i))
		}
		plan.Locations = append(plan.Locations, storage.PlannedLocation{Key: l.Key, ParentKey: l.ParentKey,
			Location: storage.LocationSpec{Name: l.Name, Label: l.Label, LocationType: l.Type, ParentName: ptrOrNil(l.Parent)}})
	}
	for i, s := range in.Body.Systems {
		path := fmt.Sprintf("systems[%d]", i)
		key, loc, err := planPlace(path, s.LocationKey, s.Location)
		if err != nil {
			return plan, err
		}
		vals, err := plannedValues(path, s.Properties)
		if err != nil {
			return plan, err
		}
		ps := storage.PlannedSystem{LocationKey: key, Properties: vals, System: storage.SystemSpec{
			Name: s.Name, Label: s.Label, LocationName: loc, StandardID: ptrOrNil(s.Standard), SystemTypeID: ptrOrNil(s.Type)}}
		for j, c := range s.Components {
			pc, err := plannedComponent(fmt.Sprintf("%s.components[%d]", path, j), c.Slot, "", nil, c.Name, c.Label, c.Product, c.Properties)
			if err != nil {
				return plan, err
			}
			ps.Components = append(ps.Components, pc)
		}
		plan.Systems = append(plan.Systems, ps)
	}
	for i, c := range in.Body.Components {
		path := fmt.Sprintf("components[%d]", i)
		key, loc, err := planPlace(path, c.LocationKey, c.Location)
		if err != nil {
			return plan, err
		}
		pc, err := plannedComponent(path, "", key, loc, c.Name, c.Label, c.Product, c.Properties)
		if err != nil {
			return plan, err
		}
		plan.Components = append(plan.Components, pc)
	}
	return plan, nil
}

// mapPlanErr maps a refused plan as its piece's own route maps it, with the
// piece's path in the plan leading the detail and set as the error's
// location, so a client finds the row of its plan that refused.
func mapPlanErr(err error) error {
	var pe *storage.PlanError
	if !errors.As(err, &pe) {
		if errors.Is(err, template.ErrNotFound) || errors.Is(err, template.ErrInvalid) {
			return mapFromTemplateErr(err)
		}
		var se huma.StatusError
		if errors.As(err, &se) {
			return err
		}
		return huma.Error500InternalServerError("apply plan")
	}
	mapped := mapFromTemplateErr(err)
	if errors.Is(err, storage.ErrPlanKey) {
		mapped = huma.Error422UnprocessableEntity(strings.TrimPrefix(pe.Err.Error(), storage.ErrPlanKey.Error()+": "))
	}
	var em *huma.ErrorModel
	if pe.Path != "" && errors.As(mapped, &em) {
		em.Errors = append(em.Errors, &huma.ErrorDetail{Location: "body." + pe.Path, Message: em.Detail})
		em.Detail = pe.Path + ": " + em.Detail
	}
	return mapped
}

// appliedBody renders what CreatePlan made in the request's order. A dry run
// leaves the ids out: the rows they named were rolled back.
func appliedBody(out *applyPlanOutput, in *applyPlanInput, made *storage.Planned) {
	keep := func(id string) string {
		if in.Body.ValidateOnly {
			return ""
		}
		return id
	}
	b := &out.Body
	b.StandardsCreated = createdStandardNames(made)
	b.Locations, b.Systems, b.Components = []appliedLocationBody{}, []appliedSystemBody{}, []appliedComponentBody{}
	names := map[string]string{}
	for _, l := range made.Locations {
		names[l.ID] = l.Name
	}
	for i, l := range made.Locations {
		lb := appliedLocationBody{Key: in.Body.Locations[i].Key, Location: l.Name, LocationID: keep(l.ID), Parent: in.Body.Locations[i].Parent}
		if l.ParentID != nil && names[*l.ParentID] != "" {
			lb.Parent = names[*l.ParentID]
		}
		b.Locations = append(b.Locations, lb)
	}
	for i, s := range made.Systems {
		sb := appliedSystemBody{System: s.System.Name, SystemID: keep(s.System.ID), Components: []appliedComponentBody{}}
		if s.System.StandardName != nil {
			sb.Standard = *s.System.StandardName
		}
		for j, c := range s.Components {
			req := in.Body.Systems[i].Components[j]
			sb.Components = append(sb.Components, appliedComponentBody{Component: c.Name, ComponentID: keep(c.ID), Slot: req.Slot, Product: req.Product})
		}
		b.Counts.Components += len(s.Components)
		b.Systems = append(b.Systems, sb)
	}
	for i, c := range made.Components {
		b.Components = append(b.Components, appliedComponentBody{Component: c.Name, ComponentID: keep(c.ID), Product: in.Body.Components[i].Product})
	}
	b.Counts.StandardsCreated, b.Counts.Locations, b.Counts.Systems = len(made.CreatedStandards), len(made.Locations), len(made.Systems)
	b.Counts.Components += len(made.Components)
}

func registerPlanRoutes(api huma.API, a *authenticator, gw storage.Gateway, cat *template.Catalog) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "apply-plan",
		Method:        http.MethodPost,
		Path:          "/plans:apply",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a plan of locations, systems and components in one write",
		Description: "Lands a plan you built in one transaction: standards created from their templates (with any parent, unless you hold one of that name), locations parents first (a parent_key names an earlier location of the plan, a parent an existing one), systems at a location of the plan or an existing one with the components that staff their slots, standalone components, and the values declared on each. Every piece is the ordinary create in your own scope with its own audit row, and every row carries the one import_id the response returns, under from_import. A refused piece refuses the whole plan, mapped as its own route maps it, with its path in the plan (systems[3].components[1]) leading the detail; nothing lands. validate_only runs the same transaction and rolls it back, returning the counts and the names the apply would mint (200, no ids). " +
			fmt.Sprintf("A plan carries at most %d rows (each standard, location, system, component and declared value is one); a bigger one is a 413 naming the limit, so chunk an import (by building), one plan each. ", planRowLimit) +
			"Gated by location:read, and each piece needs what its own create needs: location:create for locations, system:create for systems, component:create for components, system:read, system:update, component:read and component:update for a slot assignment, the update permission of what a value is declared on, and standard:create when a standard is created from its template. A location outside your scope for the piece there refuses that piece as its own create does (a location you cannot read reads as not found, 422; one you can read but not create in is a 403). A parent_key or location_key the plan does not hold, or a key given twice, is a 422 naming the row. The request body is held to the server's 1 MiB cap as every route's is, also a 413.",
	}, "location", "read"), func(ctx context.Context, in *applyPlanInput) (*applyPlanOutput, error) {
		if n := planRows(in); n > planRowLimit {
			return nil, huma.NewError(http.StatusRequestEntityTooLarge, fmt.Sprintf("the plan carries %d rows, over the limit of %d; split it into plans of at most %d rows (by building) and apply each", n, planRowLimit, planRowLimit))
		}
		if err := requireAlso(ctx, a, "this plan", planNeeds(in)); err != nil {
			return nil, err
		}
		plan, err := toPlan(ctx, gw, cat, in)
		if err != nil {
			return nil, mapPlanErr(err)
		}
		if !plan.ValidateOnly {
			plan.Import = uuid.Must(uuid.NewV7()).String()
		}
		made, err := gw.CreatePlan(ctx, actorID(ctx), plan, planScopes(ctx, a))
		if err != nil {
			return nil, mapPlanErr(err)
		}
		out := &applyPlanOutput{Status: http.StatusCreated}
		if plan.ValidateOnly {
			out.Status = http.StatusOK
		}
		out.Body.ImportID, out.Body.ValidateOnly = plan.Import, plan.ValidateOnly
		appliedBody(out, in, made)
		return out, nil
	})
	declareDryRunStatus(api)
}

// declareDryRunStatus adds the 200 a validate_only call answers beside the 201
// of an apply, with the same body, so the spec and the typed client name both.
func declareDryRunStatus(api huma.API) {
	item := api.OpenAPI().Paths["/plans:apply"]
	if item == nil || item.Post == nil || item.Post.Responses["201"] == nil {
		return
	}
	ok := *item.Post.Responses["201"]
	ok.Description = "The dry run: the counts and names an apply would mint, nothing written"
	item.Post.Responses["200"] = &ok
}
