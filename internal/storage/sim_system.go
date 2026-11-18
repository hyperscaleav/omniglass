package storage

import (
	"context"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// SimSystemSpec is a simulated system to create: the system (to a standard,
// typically), the outpost whose simulator serves its components, and one entry
// per component, each with its slot and product and the endpoints the
// collector reaches it through.
type SimSystemSpec struct {
	System     SystemSpec
	Outpost    string
	Components []SimSystemComponent
}

// SimSystemComponent is one planned component of a simulated system. Its
// name is minted (the platform's naming, as an unnamed create mints it), it
// sits where the system sits, and its endpoints are placed on the outpost.
type SimSystemComponent struct {
	Slot      string
	Product   string
	Endpoints []EndpointSpec
}

// SimSystemScopes are the caller's scopes for each write a simulated system
// makes: the system, its components, their endpoints, and the slot
// assignments (which make each component a member, and so need both ends in
// scope, #510: ComponentRead and ComponentUpdate are the component end).
type SimSystemScopes struct {
	SystemCreate, LocationRead, ComponentCreate, SystemRead, SystemUpdate, EndpointCreate, SecretRead, SecretBind scope.Set
	ComponentRead, ComponentUpdate                                                                                scope.Set
	CanAdmin                                                                                                      bool
}

// SimSystem is what CreateSimSystem made.
type SimSystem struct {
	System     *System
	Components []*Component
}

// SimSystemError says which piece of a simulated system failed, so a caller
// can map the cause the way that piece's own route maps it. It is the
// planned create's own error (Stage system, component, endpoint or slot).
type SimSystemError = PlanError

// CreateSimSystem creates a simulated system whole, in one transaction: the
// system, then for each planned component the component, its endpoints on
// the outpost, and its slot assignment. It is a consumer of the planned create
// (CreatePlan): every piece is the ordinary create with the caller's own
// scope and its own audit row, landing together or not at all.
func (p *PG) CreateSimSystem(ctx context.Context, actorID string, spec SimSystemSpec, sc SimSystemScopes) (*SimSystem, error) {
	outpost := spec.Outpost
	ps := PlannedSystem{System: spec.System}
	for _, slot := range spec.Components {
		product := slot.Product
		pc := PlannedComponent{Slot: slot.Slot, Component: ComponentSpec{ProductName: &product}}
		for _, e := range slot.Endpoints {
			n := outpost
			e.Outpost = &n
			pc.Endpoints = append(pc.Endpoints, e)
		}
		ps.Components = append(ps.Components, pc)
	}
	made, err := p.CreatePlan(ctx, actorID, Plan{Systems: []PlannedSystem{ps}}, PlanScopes{
		LocationRead: sc.LocationRead, SystemCreate: sc.SystemCreate, SystemRead: sc.SystemRead, SystemUpdate: sc.SystemUpdate,
		ComponentCreate: sc.ComponentCreate, ComponentRead: sc.ComponentRead, ComponentUpdate: sc.ComponentUpdate,
		EndpointCreate: sc.EndpointCreate, SecretRead: sc.SecretRead, SecretBind: sc.SecretBind, CanAdmin: sc.CanAdmin,
	})
	if err != nil {
		return nil, err
	}
	return &SimSystem{System: made.Systems[0].System, Components: made.Systems[0].Components}, nil
}
