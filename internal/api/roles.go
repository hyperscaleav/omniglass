package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The role surface, in three parts. Declaration: what a standard says every
// conforming system needs filled, and what one system declares ad-hoc.
// Resolution: the per-system read that merges both arcs with who fills each role
// today. Staffing: assign and unassign a component, refused when the component
// is not a typed match, its product's component_type outside every type the role
// accepts, or (if the role pins products) its product not one of them (#626).
//
// The refusal is the point of the model, so it is never bare: a shortfall is a
// 422 that NAMES both parties, what the component actually is (or which product
// it is) and what the role wants, because "no" that does not say why leaves the
// operator nothing to do. Capability retired with the family (#626): once
// assigned, an occupant satisfies its slot as long as its own verdict is not
// outage (internal/health); a merely degraded occupant still counts, nothing
// more.
//
// Gating follows the owner: the standard arc rides standard:read/update/delete,
// the system arc rides system:read/update. Every system arc route resolves its
// owner within the caller's scope first, so an out-of-scope target is a
// non-disclosing 404 rather than a forbidden or a silent write.

type systemRoleBody struct {
	Name           string   `json:"name" doc:"The role's name within its owner (the address)"`
	DisplayName    string   `json:"display_name" doc:"The role's human label"`
	Quorum         int      `json:"quorum" doc:"How many components must fill the role"`
	Capacity       *int     `json:"capacity,omitempty" doc:"The most components the role will accept; null means no upper bound beyond quorum"`
	PositionLabels []string `json:"position_labels" doc:"Human labels for each position within the role, by index; empty when unlabeled"`
	AcceptedTypes  []string `json:"accepted_types" doc:"The component_types a filling component's product must be classified within (self or a descendant); empty accepts any type"`
	PinnedProducts []string `json:"pinned_products" doc:"If set, a filling component's product must be one of these; empty accepts any product of an accepted type"`
	Impact         string   `json:"impact" doc:"What an impaired role means for its system: outage, degraded, or none"`
}

func toSystemRoleBody(r *storage.SystemRole) systemRoleBody {
	types, products, labels := r.AcceptedTypes, r.PinnedProducts, r.PositionLabels
	if types == nil {
		types = []string{}
	}
	if products == nil {
		products = []string{}
	}
	if labels == nil {
		labels = []string{}
	}
	return systemRoleBody{
		Name:           r.Name,
		DisplayName:    r.DisplayName,
		Quorum:         r.Quorum,
		Capacity:       r.Capacity,
		PositionLabels: labels,
		AcceptedTypes:  types,
		PinnedProducts: products,
		Impact:         r.Impact,
	}
}

// effectiveRoleBody is one resolved role for a system: the declaration, where it
// came from, and its staffing today. assigned and understaffed are served rather
// than left to the client so every surface reads staffing the same way.
type effectiveRoleBody struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name"`
	Quorum         int      `json:"quorum"`
	Capacity       *int     `json:"capacity,omitempty" doc:"The most components the role will accept; null means no upper bound beyond quorum"`
	PositionLabels []string `json:"position_labels" doc:"Human labels for each position within the role, by index; empty when unlabeled"`
	AcceptedTypes  []string `json:"accepted_types" doc:"The component_types a filling component's product must be classified within (self or a descendant); empty accepts any type"`
	PinnedProducts []string `json:"pinned_products" doc:"If set, a filling component's product must be one of these; empty accepts any product of an accepted type"`
	Impact         string   `json:"impact" doc:"What an impaired role means for its system: outage, degraded, or none"`
	FromStandard   bool     `json:"from_standard" doc:"True when the role is inherited from the system's standard; false when declared on the system"`
	AssignedTo     []string `json:"assigned_to" doc:"The component names filling this role in this system"`
	Assigned       int      `json:"assigned" doc:"How many components fill the role"`
	// Understaffed counts assignment rows against quorum regardless of
	// health, so it can disagree with the health read's short for the same
	// role at the same instant when an assigned component's own verdict is
	// outage: this answers "did enough components sign up", short answers
	// "is enough of it actually occupying the slot". Both are correct under
	// their own definition.
	Understaffed int `json:"understaffed" doc:"How many more assignment rows the role wants before quorum, health-blind; zero when enough are assigned regardless of their own condition. See the health read's short for the occupancy-aware figure"`
}

func toEffectiveRoleBody(e *storage.EffectiveRole) effectiveRoleBody {
	types, products, to, labels := e.AcceptedTypes, e.PinnedProducts, e.AssignedTo, e.PositionLabels
	if types == nil {
		types = []string{}
	}
	if products == nil {
		products = []string{}
	}
	if to == nil {
		to = []string{}
	}
	if labels == nil {
		labels = []string{}
	}
	return effectiveRoleBody{
		Name:           e.Name,
		DisplayName:    e.DisplayName,
		Quorum:         e.Quorum,
		Capacity:       e.Capacity,
		PositionLabels: labels,
		AcceptedTypes:  types,
		PinnedProducts: products,
		Impact:         e.Impact,
		FromStandard:   e.FromStandard,
		AssignedTo:     to,
		Assigned:       e.Assigned(),
		Understaffed:   e.Understaffed(),
	}
}

type listStandardRolesOutput struct {
	Body struct {
		Roles []systemRoleBody `json:"roles"`
	}
}

type systemRoleOutput struct {
	Body systemRoleBody
}

type listSystemRolesOutput struct {
	Body struct {
		System string              `json:"system"`
		Roles  []effectiveRoleBody `json:"roles"`
	}
}

// roleSpecBody is the declaration payload, shared by the standard and system
// arcs: the role is addressed by the path, so the body carries only how it
// presents and what it requires.
type roleSpecBody struct {
	DisplayName    string   `json:"display_name,omitempty" doc:"The role's human label; defaults to the role name"`
	Quorum         int      `json:"quorum,omitempty" minimum:"0" doc:"How many components must fill the role; omit for one"`
	Capacity       *int     `json:"capacity,omitempty" minimum:"1" doc:"The most components the role will accept; must be at least quorum. Omit to leave whatever is already declared unchanged (or unbounded on first declare); there is no way to explicitly clear a capacity back to unbounded once set"`
	PositionLabels []string `json:"position_labels,omitempty" doc:"Human labels for each position within the role, by index; replaces the label set wholesale. Omit or empty clears labelling"`
	AcceptedTypes  []string `json:"accepted_types,omitempty" doc:"The component_types a filling component's product must be classified within (self or a descendant); replaces the accepted set wholesale. Omit or empty accepts any type"`
	PinnedProducts []string `json:"pinned_products,omitempty" doc:"If set, a filling component's product must be one of these; replaces the pinned set wholesale. Omit or empty accepts any product of an accepted type"`
	Impact         string   `json:"impact,omitempty" enum:"outage,degraded,none" doc:"What an impaired role means for its system; omit for degraded. The same broken component matters differently depending on the slot it was filling: a dead confidence monitor is not a dead main display"`
}

type standardRolePathInput struct {
	ID   string `path:"id" doc:"The standard id"`
	Role string `path:"role" doc:"The role name"`
}

type setStandardRoleInput struct {
	ID   string `path:"id" doc:"The standard id"`
	Role string `path:"role" doc:"The role name"`
	Body roleSpecBody
}

type systemRolePathInput struct {
	Name string `path:"name" doc:"The system's unique name"`
	Role string `path:"role" doc:"The role name"`
}

type setSystemRoleInput struct {
	Name string `path:"name" doc:"The system's unique name"`
	Role string `path:"role" doc:"The role name"`
	Body roleSpecBody
}

type roleAssignmentPathInput struct {
	Name      string `path:"name" doc:"The system's unique name"`
	Role      string `path:"role" doc:"The role name"`
	Component string `path:"component" doc:"The component's unique name"`
}

// swapRolePositionsInput is the :swapPositions verb's input: the role,
// addressed the same way every other role route addresses it, and the two
// 1-based positions to exchange. Position stays an ordering attribute of an
// assignment rather than a second address for one (identity_shape.go's
// declaration that system_role_assignment is id-only stays true): the verb
// is role-level, not a per-position resource.
type swapRolePositionsInput struct {
	Name string `path:"name" doc:"The system's unique name"`
	Role string `path:"role" doc:"The role name"`
	Body struct {
		// Named position/with, not a/b: a and b generate CLI flags --a and
		// --b, which say nothing about what they take (cligen derives a
		// flag per body field, so the field name IS the flag name).
		Position int `json:"position" minimum:"1" doc:"One of the two positions to exchange"`
		With     int `json:"with" minimum:"1" doc:"The other position to exchange with"`
	}
}

// registerRoleRoutes wires the two arcs of the role surface.
func registerRoleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	registerStandardRoleRoutes(api, a, gw)
	registerSystemRoleRoutes(api, a, gw)
}

// registerStandardRoleRoutes wires the standard's role declarations, the
// system-side counterpart of its property contract and gated with it: read at
// the standard:read viewer floor, writes at standard:update / standard:delete.
func registerStandardRoleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-standard-roles",
		Method:      http.MethodGet,
		Path:        "/standards/{id}/roles",
		Summary:     "List a standard's declared roles",
		Description: "Lists the roles this standard declares (every conforming system inherits them live), ordered by name, each with its quorum and the component_types (accepted_types) and, if pinned, the products a filling component must match. Gated by standard:read.",
	}, "standard", "read"), func(ctx context.Context, in *standardPathInput) (*listStandardRolesOutput, error) {
		roles, err := gw.ListSystemRoles(ctx, "standard", in.ID)
		if err != nil {
			return nil, mapRoleErr(err)
		}
		out := &listStandardRolesOutput{}
		out.Body.Roles = make([]systemRoleBody, 0, len(roles))
		for i := range roles {
			out.Body.Roles = append(out.Body.Roles, toSystemRoleBody(&roles[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-standard-role",
		Method:      http.MethodPut,
		Path:        "/standards/{id}/roles/{role}",
		Summary:     "Declare a role on a standard",
		Description: "Declares a role every conforming system needs filled, or revises it in place (the role is addressed by name, so the write is idempotent). accepted_types and pinned_products each replace their set wholesale. An unknown standard, type, or product is a 422. Gated by standard:update.",
	}, "standard", "update"), func(ctx context.Context, in *setStandardRoleInput) (*systemRoleOutput, error) {
		r, err := gw.SetSystemRole(ctx, actorID(ctx), "standard", in.ID, roleSpec(in.Role, in.Body))
		if err != nil {
			return nil, mapRoleErr(err)
		}
		return &systemRoleOutput{Body: toSystemRoleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-standard-role",
		Method:        http.MethodDelete,
		Path:          "/standards/{id}/roles/{role}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a role from a standard",
		Description:   "Removes the role from the standard, and with it every assignment conforming systems made to it. A role the standard does not declare is a 404. Gated by standard:delete.",
	}, "standard", "delete"), func(ctx context.Context, in *standardRolePathInput) (*struct{}, error) {
		if err := gw.DeleteSystemRole(ctx, actorID(ctx), "standard", in.ID, in.Role); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})
}

// registerSystemRoleRoutes wires the per-system resolved read, the ad-hoc
// declarations, and staffing. Every one resolves the system within the caller's
// scope, so an out-of-scope system is a non-disclosing 404.
func registerSystemRoleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-roles",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/roles",
		Summary:     "List a system's effective roles",
		Description: "Every role this system needs filled: those its standard declares (from_standard true) plus those declared directly on it, each with the types it accepts (and products it pins, if any), the components filling it, and how many more it wants before quorum (understaffed). A one-off system shows only its own. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*listSystemRolesOutput, error) {
		roles, err := gw.EffectiveRoles(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapRoleErr(err)
		}
		out := &listSystemRolesOutput{}
		out.Body.System = in.Name
		out.Body.Roles = make([]effectiveRoleBody, 0, len(roles))
		for i := range roles {
			out.Body.Roles = append(out.Body.Roles, toEffectiveRoleBody(&roles[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-system-role",
		Method:      http.MethodPut,
		Path:        "/systems/{name}/roles/{role}",
		Summary:     "Declare a role on a system",
		Description: "Declares a role directly on this system (how a one-off system gets roles at all, and how a conforming one adds what its standard does not cover), or revises it in place. accepted_types and pinned_products each replace their set wholesale. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *setSystemRoleInput) (*systemRoleOutput, error) {
		if err := requireSystemInScope(ctx, a, gw, in.Name); err != nil {
			return nil, err
		}
		r, err := gw.SetSystemRole(ctx, actorID(ctx), "system", in.Name, roleSpec(in.Role, in.Body))
		if err != nil {
			return nil, mapRoleErr(err)
		}
		return &systemRoleOutput{Body: toSystemRoleBody(r)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-system-role",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/roles/{role}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a role from a system",
		Description:   "Removes a role declared on this system, and with it every assignment to it. A role the system does not declare itself is a 404 (a role inherited from its standard is withdrawn on the standard, not here). Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *systemRolePathInput) (*struct{}, error) {
		if err := requireSystemInScope(ctx, a, gw, in.Name); err != nil {
			return nil, err
		}
		if err := gw.DeleteSystemRole(ctx, actorID(ctx), "system", in.Name, in.Role); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "assign-system-role",
		Method:        http.MethodPut,
		Path:          "/systems/{name}/roles/{role}/assignments/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Assign a component to a role",
		Description:   "Puts this component in the role for this system. Refused with a 422 naming both parties when the component is not a typed match: its product's component_type outside every type the role accepts, or (if the role pins products) its product not one of them. A role with no accepted types takes any type. Idempotent. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *roleAssignmentPathInput) (*struct{}, error) {
		if err := gw.AssignRole(ctx, actorID(ctx), in.Name, in.Role, in.Component,
			a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "unassign-system-role",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/roles/{role}/assignments/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Unassign a component from a role",
		Description:   "Takes this component out of the role, leaving the role understaffed until another fills it. A component that was not filling the role is a 404. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *roleAssignmentPathInput) (*struct{}, error) {
		if err := gw.UnassignRole(ctx, actorID(ctx), in.Name, in.Role, in.Component,
			a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "swap-role-positions",
		Method:        http.MethodPost,
		Path:          "/systems/{name}/roles/{role}:swapPositions",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Exchange two occupants' positions within a role",
		Description:   "Exchanges the positions of whichever components currently hold position and with within this role: an ordering change only, it does not affect who is assigned or the system's health. Either position missing an occupant is a 404. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *swapRolePositionsInput) (*struct{}, error) {
		// No requireSystemInScope here: unlike set/delete-system-role,
		// SwapPositions takes a scope argument and resolves it itself
		// (ownerInScope, the same as AssignRole and UnassignRole), so a
		// second check ahead of it would only duplicate the gate.
		if err := gw.SwapPositions(ctx, actorID(ctx), in.Name, in.Role, in.Body.Position, in.Body.With,
			a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})
}

// roleSpec fills the declaration spec from the path and body, defaulting the
// label to the role name so a minimal write still reads properly on a surface.
func roleSpec(name string, body roleSpecBody) storage.SystemRoleSpec {
	display := body.DisplayName
	if display == "" {
		display = name
	}
	return storage.SystemRoleSpec{
		Name:           name,
		DisplayName:    display,
		Quorum:         body.Quorum,
		Capacity:       body.Capacity,
		PositionLabels: body.PositionLabels,
		AcceptedTypes:  body.AcceptedTypes,
		PinnedProducts: body.PinnedProducts,
		Impact:         body.Impact,
	}
}

// requireSystemInScope resolves the system within the caller's write scope
// before a declaration lands on it. The declaration methods address their owner
// by name and take no scope (a standard is not scope-scoped at all), so the
// system arc gets its ABAC check here, at the only route that can reach it.
func requireSystemInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name string) error {
	if _, err := gw.GetSystem(ctx, name, a.scopeFor(ctx, "system", "update")); err != nil {
		return mapSystemErr(err)
	}
	return nil
}

// requireComponentInScope is its component-arc twin, used by the alarm routes.
func requireComponentInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name, action string) error {
	if _, err := gw.GetComponent(ctx, name, a.scopeFor(ctx, "component", action)); err != nil {
		return mapComponentErr(err)
	}
	return nil
}

// mapRoleErr translates the role sentinels into HTTP status. The one that
// matters is the shortfall: a component that cannot fill a role is a 422 that
// names both parties in operator vocabulary, what the component actually is
// (or which product it is) and what the role wants, so the operator can act
// on the refusal rather than guess. A bare refusal would say nothing.
func mapRoleErr(err error) error {
	var typeShort *storage.TypeShortfall
	if errors.As(err, &typeShort) {
		want := append([]string(nil), typeShort.WantTypes...)
		sort.Strings(want)
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"component %q is a %s; role %q wants a %s",
			typeShort.Component, typeShort.ComponentType, typeShort.Role, strings.Join(want, " or a ")))
	}
	var pinShort *storage.ProductPinShortfall
	if errors.As(err, &pinShort) {
		want := append([]string(nil), pinShort.WantProducts...)
		sort.Strings(want)
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"component %q is product %q; role %q wants product %s",
			pinShort.Component, pinShort.ComponentProduct, pinShort.Role, strings.Join(want, " or ")))
	}
	// A double-staffing or capacity-shortfall refusal depends on OTHER rows
	// (what else is currently assigned), so it is a 409, not a 422: the
	// declaration or assignment request is not invalid on its own, it
	// conflicts with the estate's current state.
	var staffed *storage.ComponentStaffedShortfall
	if errors.As(err, &staffed) {
		return huma.Error409Conflict(fmt.Sprintf(
			"component %q already fills %q in %q; a component fills at most one role per system",
			staffed.Component, staffed.HeldRole, staffed.System))
	}
	var capShort *storage.CapacityShortfall
	if errors.As(err, &capShort) {
		return huma.Error409Conflict(fmt.Sprintf(
			"role %q in system %q is filled by %d components; capacity cannot be lowered to %d",
			capShort.Role, capShort.System, capShort.Have, capShort.Want))
	}
	var capFull *storage.CapacityFullShortfall
	if errors.As(err, &capFull) {
		return huma.Error409Conflict(fmt.Sprintf(
			"role %q in system %q already holds %d of its %d-component capacity",
			capFull.Role, capFull.System, capFull.Have, capFull.Capacity))
	}
	switch {
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("role name may not be a uuid")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("role name must be lowercase letters, digits, and hyphens, starting with a letter or digit")
	case errors.Is(err, storage.ErrRoleNotFound):
		return huma.Error404NotFound("role not found")
	case errors.Is(err, storage.ErrAssignmentMissing):
		return huma.Error404NotFound("component is not filling this role")
	case errors.Is(err, storage.ErrRoleExists):
		return huma.Error409Conflict("a role with this name is already declared here")
	case errors.Is(err, storage.ErrRoleRefNotFound):
		return huma.Error422UnprocessableEntity("unknown owner, type, or product")
	case errors.Is(err, storage.ErrRoleImpact):
		return huma.Error422UnprocessableEntity("impact must be outage, degraded, or none")
	case errors.Is(err, storage.ErrComponentAlreadyStaffed):
		return huma.Error409Conflict("component already fills a different role in this system")
	case errors.Is(err, storage.ErrRolePositionTaken):
		return huma.Error409Conflict("that position is already taken")
	case errors.Is(err, storage.ErrCapacityBelowQuorum):
		return huma.Error422UnprocessableEntity("capacity must be at least the role's quorum")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("role operation failed")
	}
}
