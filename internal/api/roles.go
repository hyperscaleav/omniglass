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
// does not provide every capability the role requires.
//
// The refusal is the point of the model, so it is never bare: a shortfall is a
// 422 that NAMES the missing capabilities, because "no" that does not say what
// is missing leaves the operator nothing to do.
//
// Gating follows the owner: the standard arc rides standard:read/update/delete,
// the system arc rides system:read/update, and the component capability facts
// ride component:read/update. Every system and component arc route resolves its
// owner within the caller's scope first, so an out-of-scope target is a
// non-disclosing 404 rather than a forbidden or a silent write.

type systemRoleBody struct {
	Name         string   `json:"name" doc:"The role's name within its owner (the address)"`
	DisplayName  string   `json:"display_name" doc:"The role's human label"`
	Quorum       int      `json:"quorum" doc:"How many components must fill the role"`
	Capabilities []string `json:"capabilities" doc:"The capabilities a component must ALL provide to fill it"`
	Impact       string   `json:"impact" doc:"What an impaired role means for its system: outage, degraded, or none"`
}

func toSystemRoleBody(r *storage.SystemRole) systemRoleBody {
	caps := r.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return systemRoleBody{
		Name:         r.Name,
		DisplayName:  r.DisplayName,
		Quorum:       r.Quorum,
		Capabilities: caps,
		Impact:       r.Impact,
	}
}

// effectiveRoleBody is one resolved role for a system: the declaration, where it
// came from, and its staffing today. assigned and understaffed are served rather
// than left to the client so every surface reads staffing the same way.
type effectiveRoleBody struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Quorum       int      `json:"quorum"`
	Capabilities []string `json:"capabilities" doc:"The capabilities a component must ALL provide to fill it"`
	Impact       string   `json:"impact" doc:"What an impaired role means for its system: outage, degraded, or none"`
	FromStandard bool     `json:"from_standard" doc:"True when the role is inherited from the system's standard; false when declared on the system"`
	AssignedTo   []string `json:"assigned_to" doc:"The component names filling this role in this system"`
	Assigned     int      `json:"assigned" doc:"How many components fill the role"`
	Understaffed int      `json:"understaffed" doc:"How many more the role wants before quorum; zero when staffed"`
}

func toEffectiveRoleBody(e *storage.EffectiveRole) effectiveRoleBody {
	caps, to := e.Capabilities, e.AssignedTo
	if caps == nil {
		caps = []string{}
	}
	if to == nil {
		to = []string{}
	}
	return effectiveRoleBody{
		Name:         e.Name,
		DisplayName:  e.DisplayName,
		Quorum:       e.Quorum,
		Capabilities: caps,
		Impact:       e.Impact,
		FromStandard: e.FromStandard,
		AssignedTo:   to,
		Assigned:     e.Assigned(),
		Understaffed: e.Understaffed(),
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
	DisplayName  string   `json:"display_name,omitempty" doc:"The role's human label; defaults to the role name"`
	Quorum       int      `json:"quorum,omitempty" minimum:"0" doc:"How many components must fill the role; omit for one"`
	Capabilities []string `json:"capabilities,omitempty" doc:"The capabilities a component must ALL provide; replaces the required set wholesale"`
	Impact       string   `json:"impact,omitempty" enum:"outage,degraded,none" doc:"What an impaired role means for its system; omit for degraded. The same broken component matters differently depending on the slot it was filling: a dead confidence monitor is not a dead main display"`
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

type componentCapabilitiesOutput struct {
	Body struct {
		Component    string   `json:"component"`
		Capabilities []string `json:"capabilities" doc:"The resolved set: the product's, plus the component's additions, minus its suppressions"`
	}
}

type componentCapabilityPathInput struct {
	Name       string `path:"name" doc:"The component's unique name"`
	Capability string `path:"capability" doc:"The capability id"`
}

type setComponentCapabilityInput struct {
	Name       string `path:"name" doc:"The component's unique name"`
	Capability string `path:"capability" doc:"The capability id"`
	Body       struct {
		Present bool `json:"present" doc:"True to add the capability, false to suppress one the product declares"`
	}
}

// registerRoleRoutes wires the three arcs of the role surface.
func registerRoleRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	registerStandardRoleRoutes(api, a, gw)
	registerSystemRoleRoutes(api, a, gw)
	registerComponentCapabilityRoutes(api, a, gw)
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
		Description: "Lists the roles this standard declares (every conforming system inherits them live), ordered by name, each with its quorum and the capabilities a component must provide to fill it. Gated by standard:read.",
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
		Description: "Declares a role every conforming system needs filled, or revises it in place (the role is addressed by name, so the write is idempotent). The capability list replaces the required set wholesale. An unknown standard or capability is a 422. Gated by standard:update.",
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
		Description: "Every role this system needs filled: those its standard declares (from_standard true) plus those declared directly on it, each with the capabilities it requires, the components filling it, and how many more it wants before quorum (understaffed). A one-off system shows only its own. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
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
		Description: "Declares a role directly on this system (how a one-off system gets roles at all, and how a conforming one adds what its standard does not cover), or revises it in place. The capability list replaces the required set wholesale. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
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
		Description:   "Puts this component in the role for this system. Refused with a 422 naming the missing capabilities when the component does not provide everything the role requires (its product's capabilities, plus what it adds, minus what it suppresses). Idempotent. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
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
}

// registerComponentCapabilityRoutes wires what a component can do: the resolved
// read the assignment guard checks against, and the two writes that add or
// suppress one capability over the product's set.
func registerComponentCapabilityRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-capabilities",
		Method:      http.MethodGet,
		Path:        "/components/{name}/capabilities",
		Summary:     "List a component's effective capabilities",
		Description: "What this component actually provides: the capabilities its product declares, plus the ones the component adds, minus the ones it suppresses. This is the set the role-assignment guard checks, so a productless component that declares its own can still be staffed. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*componentCapabilitiesOutput, error) {
		if err := requireComponentInScope(ctx, a, gw, in.Name, "read"); err != nil {
			return nil, err
		}
		caps, err := gw.ComponentCapabilities(ctx, in.Name)
		if err != nil {
			return nil, mapRoleErr(err)
		}
		if caps == nil {
			caps = []string{}
		}
		out := &componentCapabilitiesOutput{}
		out.Body.Component = in.Name
		out.Body.Capabilities = caps
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "set-component-capability",
		Method:        http.MethodPut,
		Path:          "/components/{name}/capabilities/{capability}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Declare a capability on a component",
		Description:   "Records this component's own fact about a capability: present true adds one its product does not claim, present false suppresses one it does. Idempotent. An unknown capability is a 422; an unknown or out-of-scope component is a non-disclosing 404 (the component is resolved in scope first). Gated by component:update.",
	}, "component", "update"), func(ctx context.Context, in *setComponentCapabilityInput) (*struct{}, error) {
		if err := requireComponentInScope(ctx, a, gw, in.Name, "update"); err != nil {
			return nil, err
		}
		if err := gw.SetComponentCapability(ctx, actorID(ctx), in.Name, in.Capability, in.Body.Present); err != nil {
			return nil, mapRoleErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "clear-component-capability",
		Method:        http.MethodDelete,
		Path:          "/components/{name}/capabilities/{capability}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Clear a capability declaration on a component",
		Description:   "Removes the component's own fact about the capability, so it falls back to whatever its product declares. Clearing a fact the component never declared is a 404. Gated by component:update; an out-of-scope component is a non-disclosing 404.",
	}, "component", "update"), func(ctx context.Context, in *componentCapabilityPathInput) (*struct{}, error) {
		if err := requireComponentInScope(ctx, a, gw, in.Name, "update"); err != nil {
			return nil, err
		}
		if err := gw.ClearComponentCapability(ctx, actorID(ctx), in.Name, in.Capability); err != nil {
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
		Name:         name,
		DisplayName:  display,
		Quorum:       body.Quorum,
		Capabilities: body.Capabilities,
		Impact:       body.Impact,
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

// requireComponentInScope is its component-arc twin, for the capability facts.
func requireComponentInScope(ctx context.Context, a *authenticator, gw storage.Gateway, name, action string) error {
	if _, err := gw.GetComponent(ctx, name, a.scopeFor(ctx, "component", action)); err != nil {
		return mapComponentErr(err)
	}
	return nil
}

// mapRoleErr translates the role sentinels into HTTP status. The one that
// matters is the shortfall: a component that cannot fill a role is a 422 that
// names every missing capability, so the operator can either fix the component's
// declarations or pick a different one. A bare refusal would say nothing.
func mapRoleErr(err error) error {
	var short *storage.CapabilityShortfall
	if errors.As(err, &short) {
		// Sorted so the same gap always reads the same way: the required set has
		// no inherent order, and an operator comparing two refusals should not
		// have to notice that only the wording moved.
		missing := append([]string(nil), short.Missing...)
		sort.Strings(missing)
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"component %q cannot fill role %q: missing %s",
			short.Component, short.Role, strings.Join(missing, ", ")))
	}
	switch {
	case errors.Is(err, storage.ErrRoleNotFound):
		return huma.Error404NotFound("role not found")
	case errors.Is(err, storage.ErrAssignmentMissing):
		return huma.Error404NotFound("component is not filling this role")
	case errors.Is(err, storage.ErrComponentCapabilityNotFound):
		return huma.Error404NotFound("capability not declared on this component")
	case errors.Is(err, storage.ErrRoleExists):
		return huma.Error409Conflict("a role with this name is already declared here")
	case errors.Is(err, storage.ErrRoleRefNotFound):
		return huma.Error422UnprocessableEntity("unknown owner or capability")
	case errors.Is(err, storage.ErrRoleImpact):
		return huma.Error422UnprocessableEntity("impact must be outage, degraded, or none")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	default:
		return huma.Error500InternalServerError("role operation failed")
	}
}
