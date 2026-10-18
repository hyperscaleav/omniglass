package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// SimSystemSpec is a simulated system to create: the system (to a standard,
// typically), the node whose simulator serves its components, and one entry
// per component, each with its role and product and the endpoints the
// collector reaches it through.
type SimSystemSpec struct {
	System     SystemSpec
	Node       string
	Components []SimSystemComponent
}

// SimSystemComponent is one planned component of a simulated system. Its
// name is minted (the platform's naming, as an unnamed create mints it), it
// sits where the system sits, and its endpoints are placed on the node.
type SimSystemComponent struct {
	Role      string
	Product   string
	Endpoints []EndpointSpec
}

// SimSystemScopes are the caller's scopes for each write a simulated system
// makes: the system, its components, their endpoints, and the role
// assignments (which make each component a member).
type SimSystemScopes struct {
	SystemCreate, LocationRead, ComponentCreate, SystemRead, SystemUpdate, EndpointCreate, SecretRead scope.Set
	CanAdmin                                                                                          bool
}

// SimSystem is what CreateSimSystem made.
type SimSystem struct {
	System     *System
	Components []*Component
}

// SimSystemError says which piece of a simulated system failed, so a caller
// can map the cause the way that piece's own route maps it.
type SimSystemError struct {
	Stage string // system, component, endpoint, role
	Role  string
	Err   error
}

func (e *SimSystemError) Error() string {
	if e.Role == "" {
		return fmt.Sprintf("storage: sim system %s: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("storage: sim system %s for role %s: %v", e.Stage, e.Role, e.Err)
}

func (e *SimSystemError) Unwrap() error { return e.Err }

// CreateSimSystem creates a simulated system whole, in one transaction: the
// system, then for each planned component the component, its endpoints on
// the node, and its role assignment. Every piece is the ordinary create
// with the caller's own scope and its own audit row, so the composite is
// exactly the writes an operator could make one at a time, landing together
// or not at all.
func (p *PG) CreateSimSystem(ctx context.Context, actorID string, spec SimSystemSpec, sc SimSystemScopes) (*SimSystem, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create sim system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sys, err := p.createSystemTx(ctx, tx, actorID, spec.System, sc.SystemCreate, sc.LocationRead)
	if err != nil {
		return nil, &SimSystemError{Stage: "system", Err: err}
	}
	out := &SimSystem{System: sys}
	node := spec.Node
	for _, slot := range spec.Components {
		product := slot.Product
		c, err := p.createComponentTx(ctx, tx, actorID, ComponentSpec{ProductName: &product, LocationName: spec.System.LocationName},
			sc.ComponentCreate, sc.LocationRead, sc.SystemRead, sc.SystemUpdate)
		if err != nil {
			return nil, &SimSystemError{Stage: "component", Role: slot.Role, Err: err}
		}
		for _, e := range slot.Endpoints {
			id := c.ID
			e.Component, e.Node, e.SecretRead, e.CanAdmin = &id, &node, sc.SecretRead, sc.CanAdmin
			if _, err := p.createEndpointTx(ctx, tx, actorID, e, sc.EndpointCreate); err != nil {
				return nil, &SimSystemError{Stage: "endpoint", Role: slot.Role, Err: err}
			}
		}
		if err := p.assignRoleTx(ctx, tx, actorID, sys.ID, slot.Role, c.ID, sc.SystemRead, sc.SystemUpdate); err != nil {
			return nil, &SimSystemError{Stage: "role", Role: slot.Role, Err: err}
		}
		out.Components = append(out.Components, c)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create sim system: %w", err)
	}
	return out, nil
}
