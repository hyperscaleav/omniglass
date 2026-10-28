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
// planned-system primitive. A plan names places, systems, the components
// that fill each system's roles (with their endpoints and declared values)
// and standalone components, and CreatePlan lands all of it in one
// transaction or none of it. Every piece is the ordinary create, under the
// caller's own scope, writing its own audit row, so the composite is exactly
// the writes an operator could make one at a time. The simulator's add
// (CreateSimSystem) and the template instantiations are its consumers; the
// pure half that decides what a system's roles take is internal/systemplan.

// Plan is what CreatePlan lands.
type Plan struct {
	// Template is the template the plan came from, written into every audit
	// row the plan writes (WithTemplate); empty for none.
	Template string
	// Standards are forked, in order, unless a standard of the fork's name
	// already exists (then the existing one is used as it is).
	Standards []StandardFork
	// Places are created in order; a place's ParentKey names an earlier
	// place of the plan, else its Location.ParentName is used as given.
	Places     []PlannedPlace
	Systems    []PlannedSystem
	Components []PlannedComponent
}

// PlannedPlace is one place of a plan.
type PlannedPlace struct {
	Key       string
	ParentKey string
	Location  LocationSpec
}

// PlannedSystem is one system of a plan and the components that fill its
// roles. PlaceKey names a place of the plan to sit in, else the system's
// own LocationName is used.
type PlannedSystem struct {
	PlaceKey   string
	System     SystemSpec
	Components []PlannedComponent
	Properties []PlannedValue
}

// PlannedComponent is one component of a plan. Role is the role it is
// assigned in its system (empty for a standalone component). A component of
// a system sits where the system sits; a standalone one where its spec says,
// or in the place PlaceKey names. Each endpoint's Component is filled in.
type PlannedComponent struct {
	Role       string
	PlaceKey   string
	Component  ComponentSpec
	Endpoints  []EndpointSpec
	Properties []PlannedValue
}

// PlannedValue is a declared property value.
type PlannedValue struct {
	Name  string
	Value json.RawMessage
}

// PlanScopes are the caller's scopes for each kind of write a plan makes.
// ComponentRead and ComponentUpdate are the component end of a role
// assignment (#510) and the scope a declared component value is written
// under; SystemUpdate is the scope a declared system value is written under.
type PlanScopes struct {
	LocationCreate, LocationRead                    scope.Set
	SystemCreate, SystemRead, SystemUpdate          scope.Set
	ComponentCreate, ComponentRead, ComponentUpdate scope.Set
	EndpointCreate, SecretRead                      scope.Set
	CanAdmin                                        bool
	// CanForkStandard says the caller holds standard:create, which a fork
	// the plan needs requires; without it a fork is ErrPlanForkForbidden.
	CanForkStandard bool
}

// ErrPlanForkForbidden is a plan whose standard must be forked, by a caller
// who may not create a standard.
var ErrPlanForkForbidden = errors.New("storage: forking the standard needs standard:create")

// PlanError says which piece of a plan failed, so a caller maps the cause
// the way that piece's own route maps it. Stage is standard, place, system,
// component, endpoint, role or property; Ref names the piece (a role, a
// place key, a standard, a property).
type PlanError struct {
	Stage string
	Role  string
	Ref   string
	Err   error
}

func (e *PlanError) Error() string {
	switch {
	case e.Role != "":
		return fmt.Sprintf("storage: plan %s for role %s: %v", e.Stage, e.Role, e.Err)
	case e.Ref != "":
		return fmt.Sprintf("storage: plan %s %s: %v", e.Stage, e.Ref, e.Err)
	}
	return fmt.Sprintf("storage: plan %s: %v", e.Stage, e.Err)
}

func (e *PlanError) Unwrap() error { return e.Err }

// Planned is what CreatePlan made, in plan order.
type Planned struct {
	Forked     []*Standard
	Places     []*Location
	Systems    []PlannedSystemResult
	Components []*Component
}

// PlannedSystemResult is one system CreatePlan made and its components, in
// the order the plan named them.
type PlannedSystemResult struct {
	System     *System
	Components []*Component
}

// CreatePlan lands a plan whole, in one transaction: the standards it forks,
// its places parents first, each system then each of its components (with
// their endpoints, their declared values and their role assignment) and the
// system's declared values, then the standalone components. Every piece is
// the ordinary create with the caller's own scope and its own audit row, so
// a refused piece refuses the whole plan, mapped as that piece's own route
// maps it (a PlanError names it), and nothing lands.
func (p *PG) CreatePlan(ctx context.Context, actorID string, plan Plan, sc PlanScopes) (*Planned, error) {
	ctx = WithTemplate(ctx, plan.Template)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create plan: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, err := p.createPlanTx(ctx, tx, actorID, plan, sc)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create plan: %w", err)
	}
	return out, nil
}

func (p *PG) createPlanTx(ctx context.Context, tx pgx.Tx, actorID string, plan Plan, sc PlanScopes) (*Planned, error) {
	out := &Planned{}
	for _, f := range plan.Standards {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from standard where name = $1)`, f.Name).Scan(&exists); err != nil {
			return nil, fmt.Errorf("storage: plan standard %s: %w", f.Name, err)
		}
		if exists {
			continue
		}
		if !sc.CanForkStandard {
			return nil, &PlanError{Stage: "standard", Ref: f.Name, Err: ErrPlanForkForbidden}
		}
		st, err := p.forkStandardTx(ctx, tx, actorID, f)
		if err != nil {
			return nil, &PlanError{Stage: "standard", Ref: f.Name, Err: err}
		}
		out.Forked = append(out.Forked, st)
	}

	places := map[string]*Location{}
	for _, pl := range plan.Places {
		spec := pl.Location
		if pl.ParentKey != "" {
			parent, ok := places[pl.ParentKey]
			if !ok {
				return nil, &PlanError{Stage: "place", Ref: pl.Key, Err: fmt.Errorf("parent %q is not an earlier place of the plan", pl.ParentKey)}
			}
			id := parent.ID
			spec.ParentName = &id
		}
		l, err := p.createLocationTx(ctx, tx, actorID, spec, sc.LocationCreate)
		if err != nil {
			return nil, &PlanError{Stage: "place", Ref: pl.Key, Err: err}
		}
		places[pl.Key] = l
		out.Places = append(out.Places, l)
	}
	placeOf := func(key string, given *string) (*string, error) {
		if key == "" {
			return given, nil
		}
		l, ok := places[key]
		if !ok {
			return nil, fmt.Errorf("place %q is not a place of the plan", key)
		}
		id := l.ID
		return &id, nil
	}

	for _, ps := range plan.Systems {
		spec := ps.System
		loc, err := placeOf(ps.PlaceKey, spec.LocationName)
		if err != nil {
			return nil, &PlanError{Stage: "system", Err: err}
		}
		spec.LocationName = loc
		sys, err := p.createSystemTx(ctx, tx, actorID, spec, sc.SystemCreate, sc.LocationRead)
		if err != nil {
			return nil, &PlanError{Stage: "system", Err: err}
		}
		res := PlannedSystemResult{System: sys}
		for _, pc := range ps.Components {
			// A system's component sits where the system sits.
			pc.Component.LocationName = spec.LocationName
			c, err := p.createPlannedComponentTx(ctx, tx, actorID, pc, sc)
			if err != nil {
				return nil, err
			}
			if pc.Role != "" {
				if err := p.assignRoleTx(ctx, tx, actorID, sys.ID, pc.Role, c.ID, sc.SystemRead, sc.SystemUpdate, sc.ComponentRead, sc.ComponentUpdate); err != nil {
					return nil, &PlanError{Stage: "role", Role: pc.Role, Err: err}
				}
			}
			res.Components = append(res.Components, c)
		}
		for _, v := range ps.Properties {
			if _, err := p.setPropertyTx(ctx, tx, actorID, "system", sys.ID, v.Name, "", v.Value, sc.SystemRead, sc.SystemUpdate); err != nil {
				return nil, &PlanError{Stage: "property", Ref: v.Name, Err: err}
			}
		}
		out.Systems = append(out.Systems, res)
	}

	for _, pc := range plan.Components {
		loc, err := placeOf(pc.PlaceKey, pc.Component.LocationName)
		if err != nil {
			return nil, &PlanError{Stage: "component", Err: err}
		}
		pc.Component.LocationName = loc
		c, err := p.createPlannedComponentTx(ctx, tx, actorID, pc, sc)
		if err != nil {
			return nil, err
		}
		out.Components = append(out.Components, c)
	}
	return out, nil
}

// createPlannedComponentTx creates one planned component, its endpoints and
// its declared values.
func (p *PG) createPlannedComponentTx(ctx context.Context, tx pgx.Tx, actorID string, pc PlannedComponent, sc PlanScopes) (*Component, error) {
	c, err := p.createComponentTx(ctx, tx, actorID, pc.Component, sc.ComponentCreate, sc.LocationRead, sc.SystemRead, sc.SystemUpdate)
	if err != nil {
		return nil, &PlanError{Stage: "component", Role: pc.Role, Err: err}
	}
	for _, e := range pc.Endpoints {
		id := c.ID
		e.Component, e.SecretRead, e.CanAdmin = &id, sc.SecretRead, sc.CanAdmin
		if _, err := p.createEndpointTx(ctx, tx, actorID, e, sc.EndpointCreate); err != nil {
			return nil, &PlanError{Stage: "endpoint", Role: pc.Role, Err: err}
		}
	}
	for _, v := range pc.Properties {
		if _, err := p.setPropertyTx(ctx, tx, actorID, "component", c.ID, v.Name, "", v.Value, sc.ComponentRead, sc.ComponentUpdate); err != nil {
			return nil, &PlanError{Stage: "property", Role: pc.Role, Ref: v.Name, Err: err}
		}
	}
	return c, nil
}
