package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The planned create (#827, ADR-0827): the transactional half of the
// planned-system primitive. A plan names locations, systems, the components
// that fill each system's slots (with their endpoints and declared values)
// and standalone components, and CreatePlan lands all of it in one
// transaction or none of it. Every piece is the ordinary create, under the
// caller's own scope, writing its own audit row, so the composite is exactly
// the writes an operator could make one at a time. The simulator's add
// (CreateSimSystem) and the creates from a template are its consumers; the
// pure half that decides what a system's slots take is internal/systemplan.

// Plan is what CreatePlan lands.
type Plan struct {
	// Template is the template the plan came from, written into every audit
	// row the plan writes as from_template (WithTemplate); empty for none.
	Template string
	// Import is the import id an applied plan (POST /plans:apply, #2368) is
	// written under, stamped as from_import into every audit row the plan
	// writes (WithImport); empty for none.
	Import string
	// ValidateOnly runs the whole plan, every piece and its checks, in the
	// transaction and then rolls it back (#2368): what it returns is what an
	// apply would make, and nothing is written.
	ValidateOnly bool
	// Standards are created from their templates, in order, unless a standard of that name
	// already exists (then the existing one is used as it is).
	Standards []StandardFromTemplate
	// Locations are created in order; a location's ParentKey names an earlier
	// place of the plan, else its Location.ParentName is used as given.
	Locations  []PlannedLocation
	Systems    []PlannedSystem
	Components []PlannedComponent
}

// PlannedLocation is one place of a plan.
type PlannedLocation struct {
	Key       string
	ParentKey string
	Location  LocationSpec
}

// PlannedSystem is one system of a plan and the components that fill its
// slots. LocationKey names a location of the plan to sit in, else the system's
// own LocationName is used.
type PlannedSystem struct {
	LocationKey string
	System      SystemSpec
	Components  []PlannedComponent
	Properties  []PlannedValue
}

// PlannedComponent is one component of a plan. Slot is the slot it is
// assigned in its system (empty for a standalone component). A component of
// a system sits where the system sits; a standalone one where its spec says,
// or in the location LocationKey names. Each endpoint's Component is filled in.
type PlannedComponent struct {
	Slot        string
	LocationKey string
	Component   ComponentSpec
	Endpoints   []EndpointSpec
	Properties  []PlannedValue
}

// PlannedValue is a declared property value.
type PlannedValue struct {
	Name  string
	Value json.RawMessage
}

// PlanScopes are the caller's scopes for each kind of write a plan makes.
// ComponentRead and ComponentUpdate are the component end of a slot
// assignment (#510) and the scope a declared component value is written
// under; SystemUpdate is the scope a declared system value is written under.
type PlanScopes struct {
	LocationCreate, LocationRead                    scope.Set
	SystemCreate, SystemRead, SystemUpdate          scope.Set
	ComponentCreate, ComponentRead, ComponentUpdate scope.Set
	EndpointCreate, SecretRead, SecretBind          scope.Set
	CanAdmin                                        bool
	// CanCreateStandard says the caller holds standard:create, which creating a
	// standard the plan needs requires; without it that create is ErrPlanStandardForbidden.
	CanCreateStandard bool
}

// ErrPlanStandardForbidden is a plan whose standard must be created from its template, by a caller
// who may not create a standard.
var ErrPlanStandardForbidden = errors.New("storage: creating the standard from its template needs standard:create")

// ErrPlanKey is a plan that refers to its own pieces wrongly: a parent_key
// that names no earlier location of the plan, a location key no location of
// the plan holds, or one key given to two locations. It is the plan's own
// mistake, not a refusal of any piece's create.
var ErrPlanKey = errors.New("storage: plan key")

// PlanError says which piece of a plan failed, so a caller maps the cause
// the way that piece's own route maps it. Stage is standard, location, system,
// component, endpoint, slot, property (a component's declared value) or
// system-property; Ref names the piece (a slot, a
// location key, a standard, a property). Path is where the piece sits in the
// plan (standards[0], locations[2], systems[1].components[0],
// systems[1].properties[0], components[3], components[3].properties[0]), so a
// client that submitted the plan finds the row it wrote.
type PlanError struct {
	Stage string
	Slot  string
	Ref   string
	Path  string
	Err   error
}

// planErrAt sets the plan path on err when it is a PlanError that has none
// yet (the innermost path wins), and returns err.
func planErrAt(err error, path string) error {
	var pe *PlanError
	if errors.As(err, &pe) && pe.Path == "" {
		pe.Path = path
	}
	return err
}

func (e *PlanError) Error() string {
	switch {
	case e.Slot != "":
		return fmt.Sprintf("storage: plan %s for slot %s: %v", e.Stage, e.Slot, e.Err)
	case e.Ref != "":
		return fmt.Sprintf("storage: plan %s %s: %v", e.Stage, e.Ref, e.Err)
	}
	return fmt.Sprintf("storage: plan %s: %v", e.Stage, e.Err)
}

func (e *PlanError) Unwrap() error { return e.Err }

// Planned is what CreatePlan made, in plan order.
type Planned struct {
	CreatedStandards []*Standard
	Locations        []*Location
	Systems          []PlannedSystemResult
	Components       []*Component
}

// PlannedSystemResult is one system CreatePlan made and its components, in
// the order the plan named them.
type PlannedSystemResult struct {
	System     *System
	Components []*Component
}

// CreatePlan lands a plan whole, in one transaction: the standards it creates from templates,
// its locations parents first, each system then each of its components (with
// their endpoints, their declared values and their slot assignment) and the
// system's declared values, then the standalone components. Every piece is
// the ordinary create with the caller's own scope and its own audit row, so
// a refused piece refuses the whole plan, mapped as that piece's own route
// maps it (a PlanError names it), and nothing lands. A ValidateOnly plan
// runs the same way and is rolled back instead of committed.
func (p *PG) CreatePlan(ctx context.Context, actorID string, plan Plan, sc PlanScopes) (*Planned, error) {
	ctx = WithImport(WithTemplate(ctx, plan.Template), plan.Import)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create plan: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, err := p.createPlanTx(ctx, tx, actorID, plan, sc)
	if err != nil {
		return nil, err
	}
	if plan.ValidateOnly {
		return out, nil // the deferred rollback discards every write
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create plan: %w", err)
	}
	return out, nil
}

func (p *PG) createPlanTx(ctx context.Context, tx pgx.Tx, actorID string, plan Plan, sc PlanScopes) (*Planned, error) {
	out := &Planned{}
	// Health is recomputed once, after the last piece, over everything the
	// plan staffed (#915): see healthDeferral.
	deferred := newHealthDeferral()
	for fi, f := range plan.Standards {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from standard_in_effect() standard where name = $1)`, f.Name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("storage: plan standard %s: %w", f.Name, err)
		}
		if exists {
			continue
		}
		if !sc.CanCreateStandard {
			return nil, &PlanError{Stage: "standard", Ref: f.Name, Path: fmt.Sprintf("standards[%d]", fi), Err: ErrPlanStandardForbidden}
		}
		st, err := p.createStandardFromTemplateTx(ctx, tx, actorID, f)
		if err != nil {
			return nil, &PlanError{Stage: "standard", Ref: f.Name, Path: fmt.Sprintf("standards[%d]", fi), Err: err}
		}
		out.CreatedStandards = append(out.CreatedStandards, st)
	}

	locations := map[string]*Location{}
	for li, pl := range plan.Locations {
		spec := pl.Location
		at := fmt.Sprintf("locations[%d]", li)
		if pl.ParentKey != "" {
			parent, ok := locations[pl.ParentKey]
			if !ok {
				return nil, &PlanError{Stage: "location", Ref: pl.Key, Path: at, Err: fmt.Errorf("%w: parent %q is not an earlier location of the plan", ErrPlanKey, pl.ParentKey)}
			}
			id := parent.ID
			spec.ParentName = &id
		}
		if _, dup := locations[pl.Key]; dup {
			return nil, &PlanError{Stage: "location", Ref: pl.Key, Path: at, Err: fmt.Errorf("%w: key %q is given to an earlier location of the plan", ErrPlanKey, pl.Key)}
		}
		l, err := p.createLocationTx(ctx, tx, actorID, spec, sc.LocationCreate)
		if err != nil {
			return nil, &PlanError{Stage: "location", Ref: pl.Key, Path: at, Err: err}
		}
		locations[pl.Key] = l
		out.Locations = append(out.Locations, l)
	}
	placeOf := func(key string, given *string) (*string, error) {
		if key == "" {
			return given, nil
		}
		l, ok := locations[key]
		if !ok {
			return nil, fmt.Errorf("%w: location %q is not a location of the plan", ErrPlanKey, key)
		}
		id := l.ID
		return &id, nil
	}

	for si, ps := range plan.Systems {
		spec := ps.System
		at := fmt.Sprintf("systems[%d]", si)
		loc, err := placeOf(ps.LocationKey, spec.LocationName)
		if err != nil {
			return nil, &PlanError{Stage: "system", Path: at, Err: err}
		}
		spec.LocationName = loc
		sys, err := p.createSystemTx(ctx, tx, actorID, spec, sc.SystemCreate, sc.LocationRead, deferred)
		if err != nil {
			return nil, &PlanError{Stage: "system", Path: at, Err: err}
		}
		res := PlannedSystemResult{System: sys}
		for ci, pc := range ps.Components {
			cat := fmt.Sprintf("%s.components[%d]", at, ci)
			// A system's component sits where the system sits.
			pc.Component.LocationName = spec.LocationName
			c, err := p.createPlannedComponentTx(ctx, tx, actorID, pc, sc)
			if err != nil {
				return nil, planErrAt(err, cat)
			}
			if pc.Slot != "" {
				if err := p.assignSlotTx(ctx, tx, actorID, sys.ID, pc.Slot, c.ID, sc.SystemRead, sc.SystemUpdate, sc.ComponentRead, sc.ComponentUpdate, deferred); err != nil {
					return nil, &PlanError{Stage: "slot", Slot: pc.Slot, Path: cat, Err: err}
				}
			}
			res.Components = append(res.Components, c)
		}
		for vi, v := range ps.Properties {
			if _, err := p.setPropertyTx(ctx, tx, actorID, "system", sys.ID, v.Name, "", v.Value, sc.SystemRead, sc.SystemUpdate); err != nil {
				return nil, &PlanError{Stage: "system-property", Ref: v.Name, Path: fmt.Sprintf("%s.properties[%d]", at, vi), Err: err}
			}
		}
		out.Systems = append(out.Systems, res)
	}

	for ci, pc := range plan.Components {
		at := fmt.Sprintf("components[%d]", ci)
		loc, err := placeOf(pc.LocationKey, pc.Component.LocationName)
		if err != nil {
			return nil, &PlanError{Stage: "component", Path: at, Err: err}
		}
		pc.Component.LocationName = loc
		c, err := p.createPlannedComponentTx(ctx, tx, actorID, pc, sc)
		if err != nil {
			return nil, planErrAt(err, at)
		}
		out.Components = append(out.Components, c)
	}
	if err := deferred.flush(ctx, p, tx); err != nil {
		return nil, err
	}
	return out, nil
}

// createPlannedComponentTx creates one planned component, its endpoints and
// its declared values.
func (p *PG) createPlannedComponentTx(ctx context.Context, tx pgx.Tx, actorID string, pc PlannedComponent, sc PlanScopes) (*Component, error) {
	c, err := p.createComponentTx(ctx, tx, actorID, pc.Component, sc.ComponentCreate, sc.LocationRead, sc.SystemRead, sc.SystemUpdate)
	if err != nil {
		return nil, &PlanError{Stage: "component", Slot: pc.Slot, Err: err}
	}
	for _, e := range pc.Endpoints {
		id := c.ID
		e.Component, e.SecretRead, e.SecretBind, e.CanAdmin = &id, sc.SecretRead, sc.SecretBind, sc.CanAdmin
		if _, err := p.createEndpointTx(ctx, tx, actorID, e, sc.EndpointCreate); err != nil {
			return nil, &PlanError{Stage: "endpoint", Slot: pc.Slot, Err: err}
		}
	}
	for _, v := range pc.Properties {
		if _, err := p.setPropertyTx(ctx, tx, actorID, "component", c.ID, v.Name, "", v.Value, sc.ComponentRead, sc.ComponentUpdate); err != nil {
			return nil, &PlanError{Stage: "property", Slot: pc.Slot, Ref: v.Name, Err: err}
		}
	}
	return c, nil
}
