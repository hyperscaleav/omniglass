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
	"github.com/hyperscaleav/omniglass/internal/updatemask"
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
	Alternate      string   `json:"alternate,omitempty" doc:"The choice/alternate this role joins, addressed as \"choice-name/alternate-name\" (#626); absent when the role is unconditional. The same form the write body takes, so a read round-trips into a write"`
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
		Alternate:      r.Alternate,
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
	Alternate      string   `json:"alternate,omitempty" doc:"The choice/alternate this role joins, addressed as \"choice-name/alternate-name\" (#626); absent when the role is unconditional. The same form the write body takes, so a read round-trips into a write"`
	FromStandard   bool     `json:"from_standard" doc:"True when the role is inherited from the system's standard; false when declared on the system"`
	AssignedTo     []string `json:"assigned_to" doc:"The component names filling this role in this system"`
	// Positions is index-for-index with AssignedTo: Positions[i] is
	// AssignedTo[i]'s own 1-based position. A gap left by an unassign is
	// never compacted (#626), so the array is not assumed dense; a caller
	// reordering occupants (the console's drag-to-reorder) must read the
	// real position here rather than assume AssignedTo[i] sits at i+1.
	Positions []int `json:"positions" doc:"AssignedTo's own 1-based position, index for index; not necessarily i+1, since an unassign leaves a gap rather than compacting"`
	Assigned  int   `json:"assigned" doc:"How many components fill the role"`
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
	positions := e.Positions
	if positions == nil {
		positions = []int{}
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
		Alternate:      e.Alternate,
		FromStandard:   e.FromStandard,
		Positions:      positions,
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
//
// Every field is patched under the mask (#666, AIP-134): with no update_mask,
// the fields this body populated are the ones that change, so an omitted field
// keeps whatever the role already declares. Naming a field in update_mask is
// what writes it regardless, which is the only way to CLEAR one, since a
// cleared field and an omitted field carry the same empty value on the wire.
type roleSpecBody struct {
	UpdateMask     []string `json:"update_mask,omitempty" doc:"Which fields this write changes (AIP-134). Omit it and the fields present in the body change and nothing else; name a field here and it is written even when the body leaves it empty, which is how a field is CLEARED; send [\"*\"] for full replacement, where every field the body omits goes back to its default. A field this resource does not patch is a 422 naming it"`
	DisplayName    string   `json:"display_name,omitempty" doc:"The role's human label; defaults to the role name on first declare"`
	Quorum         int      `json:"quorum,omitempty" minimum:"0" doc:"How many components must fill the role; one on first declare"`
	Capacity       *int     `json:"capacity,omitempty" minimum:"1" doc:"The most components the role will accept; must be at least quorum, and unbounded on first declare. Name capacity in update_mask with no value here to clear it back to unbounded"`
	PositionLabels []string `json:"position_labels,omitempty" doc:"Human labels for each position within the role, by index; replaces the label set wholesale when written. An empty list is not a populated field, so clearing the labels means naming position_labels in update_mask"`
	AcceptedTypes  []string `json:"accepted_types,omitempty" doc:"The component_types a filling component's product must be classified within (self or a descendant); replaces the accepted set wholesale when written, and an empty set accepts any type. Clearing it means naming accepted_types in update_mask"`
	PinnedProducts []string `json:"pinned_products,omitempty" doc:"If set, a filling component's product must be one of these; replaces the pinned set wholesale when written, and an empty set accepts any product of an accepted type. Clearing it means naming pinned_products in update_mask"`
	Impact         string   `json:"impact,omitempty" enum:"outage,degraded,none" doc:"What an impaired role means for its system; degraded on first declare. The same broken component matters differently depending on the slot it was filling: a dead confidence monitor is not a dead main display"`
	// Alternate is *string, not string, because the empty string is the
	// explicit DETACH here rather than an absent value: a role's alternate
	// silently detaching on an unrelated edit (#626) took a shipped
	// standard's systems from healthy to degraded with no audit trail and no
	// way back except raw SQL, so a set pointer always means the caller said
	// something about this field.
	Alternate *string `json:"alternate,omitempty" doc:"The choice/alternate this role joins, addressed as \"choice-name/alternate-name\" (#626). An empty string detaches the role, making it unconditional; an unknown choice or alternate is a 422"`
}

// resolveRoleWrite turns the body's update_mask into the write set the write
// obeys, refusing a mask this resource cannot honor with the field named.
//
// The implied mask (an absent update_mask) is read off the SPEC the write is
// about to store, not off the body field by field, so "which fields did the
// caller send" is defined once for both layers instead of once per layer,
// where the two definitions would drift.
func resolveRoleWrite(body roleSpecBody, spec storage.SystemRoleSpec) (storage.SystemRoleSpec, error) {
	write, err := updatemask.Resolve(body.UpdateMask, storage.RolePatchFields, spec.Populated())
	if err != nil {
		return spec, err
	}
	spec.Write = write
	return spec, nil
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
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Role string `path:"role" doc:"The role name"`
}

type setSystemRoleInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Role string `path:"role" doc:"The role name"`
	Body roleSpecBody
}

type roleAssignmentPathInput struct {
	Name      string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Role      string `path:"role" doc:"The role name"`
	Component string `path:"component" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
}

// swapRolePositionsInput is the :swapPositions verb's input: the role,
// addressed the same way every other role route addresses it, and the two
// 1-based positions to exchange. Position stays an ordering attribute of an
// assignment rather than a second address for one (identity_shape.go's
// declaration that system_role_assignment is id-only stays true): the verb
// is role-level, not a per-position resource.
type swapRolePositionsInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
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
		Method:      http.MethodPatch,
		Path:        "/standards/{id}/roles/{role}",
		Summary:     "Declare a role on a standard",
		Description: "Declares a role every conforming system needs filled, or revises it in place (the role is addressed by name, so the write is idempotent and declaring is this same route). Partial by default: the fields present in the body change and the rest of the declaration is left alone. update_mask overrides that, writing exactly the fields it names, which is how a field is cleared, and [\"*\"] replaces the whole declaration. An unknown standard, type, or product is a 422, as is a mask naming a field this resource does not patch. Gated by standard:update.",
	}, "standard", "update"), func(ctx context.Context, in *setStandardRoleInput) (*systemRoleOutput, error) {
		altID, err := resolveAlternateBody(ctx, gw, "standard", in.ID, in.Body)
		if err != nil {
			return nil, mapRoleErr(err)
		}
		spec, err := resolveRoleWrite(in.Body, roleSpec(in.Role, in.Body, altID))
		if err != nil {
			return nil, mapRoleErr(err)
		}
		r, err := gw.SetSystemRole(ctx, actorID(ctx), "standard", in.ID, spec)
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
// read scope and then requires its update scope, so a system outside the read
// scope is a non-disclosing 404 and one the caller can read but not write is a 403.
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
		Method:      http.MethodPatch,
		Path:        "/systems/{name}/roles/{role}",
		Summary:     "Declare a role on a system",
		Description: "Declares a role directly on this system (how a one-off system gets roles at all, and how a conforming one adds what its standard does not cover), or revises it in place. Partial by default: the fields present in the body change and the rest of the declaration is left alone. update_mask overrides that, writing exactly the fields it names, which is how a field is cleared, and [\"*\"] replaces the whole declaration. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *setSystemRoleInput) (*systemRoleOutput, error) {
		sysID, err := requireSystemInScope(ctx, a, gw, in.Name)
		if err != nil {
			return nil, err
		}
		altID, err := resolveAlternateBody(ctx, gw, "system", sysID, in.Body)
		if err != nil {
			return nil, mapRoleErr(err)
		}
		spec, err := resolveRoleWrite(in.Body, roleSpec(in.Role, in.Body, altID))
		if err != nil {
			return nil, mapRoleErr(err)
		}
		r, err := gw.SetSystemRole(ctx, actorID(ctx), "system", sysID, spec)
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
		Description:   "Removes a role declared on this system, and with it every assignment to it. A role the system does not declare itself is a 404 (a role inherited from its standard is withdrawn on the standard, not here). Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *systemRolePathInput) (*struct{}, error) {
		sysID, err := requireSystemInScope(ctx, a, gw, in.Name)
		if err != nil {
			return nil, err
		}
		if err := gw.DeleteSystemRole(ctx, actorID(ctx), "system", sysID, in.Role); err != nil {
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
		Description:   "Puts this component in the role for this system. Refused with a 422 naming both parties when the component is not a typed match: its product's component_type outside every type the role accepts, or (if the role pins products) its product not one of them. A role with no accepted types takes any type. Idempotent. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *roleAssignmentPathInput) (*struct{}, error) {
		if err := gw.AssignRole(ctx, actorID(ctx), in.Name, in.Role, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update")); err != nil {
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
		Description:   "Takes this component out of the role, leaving the role understaffed until another fills it. A component that was not filling the role is a 404. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *roleAssignmentPathInput) (*struct{}, error) {
		if err := gw.UnassignRole(ctx, actorID(ctx), in.Name, in.Role, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update")); err != nil {
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
		Description:   "Exchanges the positions of whichever components currently hold position and with within this role: an ordering change only, it does not affect who is assigned or the system's health. Either position missing an occupant is a 404. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *swapRolePositionsInput) (*struct{}, error) {
		// No requireSystemInScope here: unlike set/delete-system-role,
		// SwapPositions takes a scope argument and resolves it itself
		// (ownerInScope, the same as AssignRole and UnassignRole), so a
		// second check ahead of it would only duplicate the gate.
		if err := gw.SwapPositions(ctx, actorID(ctx), in.Name, in.Role, in.Body.Position, in.Body.With,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})
}

// roleSpec fills the declaration spec from the path and body, field for field:
// nothing is defaulted here, because a default applied on the way in is
// indistinguishable from a value the caller sent, and the implied mask is
// exactly that distinction (the role-name default for an absent display_name
// lives in SetSystemRole, on the value, where it applies only to a write that
// actually writes the field). altID is the already-resolved AlternateID
// (resolveAlternateBody), not read from body directly: resolution needs the
// gateway and the owner, neither of which this function has, and keeping it a
// pure function here keeps roleSpec testable without a database.
func roleSpec(name string, body roleSpecBody, altID *string) storage.SystemRoleSpec {
	return storage.SystemRoleSpec{
		Name:           name,
		DisplayName:    body.DisplayName,
		Quorum:         body.Quorum,
		Capacity:       body.Capacity,
		PositionLabels: body.PositionLabels,
		AcceptedTypes:  body.AcceptedTypes,
		PinnedProducts: body.PinnedProducts,
		Impact:         body.Impact,
		AlternateID:    altID,
	}
}

// resolveAlternateBody turns a request body's optional "choice/alternate"
// wire reference into the id storage.SystemRoleSpec.AlternateID expects,
// nil when the caller omitted the field (SetSystemRole then leaves whatever
// is already declared alone, the fix for #626's silent-detach-on-edit
// defect: every route that declares a role used to construct AlternateID
// from nothing, which always wrote NULL over it). An unknown or malformed
// reference is storage.ErrRoleRefNotFound, mapped to 422 by mapRoleErr the
// same way a bad accepted type or pinned product is.
func resolveAlternateBody(ctx context.Context, gw storage.Gateway, ownerKind, ownerID string, body roleSpecBody) (*string, error) {
	if body.Alternate == nil {
		return nil, nil
	}
	id, err := gw.ResolveAlternate(ctx, ownerKind, ownerID, *body.Alternate)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// requireSystemInScope resolves the system a declaration is about to land on
// and returns its id. The declaration methods address their owner by kind and
// id and take no scope (a standard is not scope-scoped at all), so the system
// arc gets its ABAC check here, at the only route that can reach it. The id is
// returned rather than discarded so the caller can pass it straight on to the
// gateway calls that follow, instead of handing back the caller's raw reference
// for a second, redundant resolve: once names are no longer unique (#627), a
// second name-based resolve is not just wasted work, it is a second surface
// that can raise storage.ErrAmbiguousName with no mapping, on a reference the
// first resolve already proved unambiguous.
//
// It resolves through ResolveActionTarget with BOTH sets rather than through
// GetSystem with the update set alone (#736). Passing an action scope into a
// parameter that means read is what made a system the caller reads every day
// but may not write come back as "system not found": a lie about existence that
// an operator looking straight at the row reads as a broken platform rather than
// as the missing grant it is. The read set is the caller's own system:read, not
// a convenient wider one, which is the condition that keeps the 403 from
// disclosing a row the caller could not otherwise see.
func requireSystemInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name string) (string, error) {
	id, err := gw.ResolveActionTarget(ctx, "system", name,
		a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
	if err != nil {
		return "", mapSystemErr(err)
	}
	return id, nil
}

// requireComponentInScope is its component-arc twin, used by the alarm routes,
// and returns the component's id for the same reason. action names the
// permission whose scope decides the write (component:update for raising and
// clearing); the read half is always the caller's own component:read.
//
// The read-only caller (the alarm LIST) passes "read" for both, which is the
// same set twice and therefore the plain non-disclosing read it always was: a
// route with nothing to act on has no third branch to pick.
func requireComponentInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name, action string) (string, error) {
	id, err := gw.ResolveActionTarget(ctx, "component", name,
		a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", action))
	if err != nil {
		return "", mapComponentErr(err)
	}
	return id, nil
}

// mapRoleErr translates the role sentinels into HTTP status. The one that
// matters is the shortfall: a component that cannot fill a role is a 422 that
// names both parties in operator vocabulary, what the component actually is
// (or which product it is) and what the role wants, so the operator can act
// on the refusal rather than guess. A bare refusal would say nothing.
func mapRoleErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	// A mask this resource cannot honor is a request fault that names the
	// field, the same shape as a shortfall: the caller stated an intent the
	// resource has no field for, and a silent no-op would leave them
	// believing a write landed.
	var maskErr *updatemask.Error
	if errors.As(err, &maskErr) {
		return huma.Error422UnprocessableEntity(maskErr.Error())
	}
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
	// Before the not-found cases, and a 403 rather than a 404: the staffing
	// routes resolve their system with the read-then-action split (#736), so
	// this sentinel means the caller can READ that system and simply may not
	// staff it. Reporting it absent would be false to a caller looking straight
	// at the row.
	case errors.Is(err, storage.ErrSystemForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("role operation failed")
	}
}
