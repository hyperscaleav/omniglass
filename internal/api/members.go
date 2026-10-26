package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// systemMemberBody is one binding of a component to a system. Primary marks the
// membership that answers a question asked without a system in hand; it is a
// default for context-free callers, not a resolution rule.
type systemMemberBody struct {
	System      string `json:"system" doc:"Name of the system, or a dotted address (e.g. boi.17c.$sys.av)"`
	SystemID    string `json:"system_id" doc:"The system's uuid, the address to navigate by: a bare name is unique only within its placement, so a shared component's memberships can name two systems that share a name"`
	Component   string `json:"component" doc:"Name of the component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	ComponentID string `json:"component_id" doc:"The component's uuid"`
	Primary     bool   `json:"primary" doc:"Whether this membership is the component's default when no system is given"`
	SystemCount int    `json:"system_count" doc:"How many systems this component belongs to in total; more than one means it is shared"`
	// The component's verdict, not the system's: a system's view polls its
	// members' health here (#940), one request for the system.
	ComponentVerdict string `json:"component_verdict" doc:"The component's own recorded verdict (healthy, degraded, or outage), the one its active alarms make; never the system's"`
}

func toSystemMemberBody(m storage.Member) systemMemberBody {
	return systemMemberBody{System: m.SystemID, SystemID: m.SystemUUID, Component: m.ComponentID, ComponentID: m.ComponentUUID, Primary: m.IsPrimary, SystemCount: m.SystemCount, ComponentVerdict: m.ComponentVerdict}
}

func toSystemMemberBodies(ms []storage.Member) []systemMemberBody {
	out := make([]systemMemberBody, 0, len(ms))
	for _, m := range ms {
		out = append(out, toSystemMemberBody(m))
	}
	return out
}

type systemMembersInput struct {
	Name string `path:"name" doc:"Name of the system, or a dotted address (e.g. boi.17c.$sys.av)"`
}

type componentMembershipsInput struct {
	Name string `path:"name" doc:"Name of the component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
}

type systemMemberPathInput struct {
	Name      string `path:"name" doc:"Name of the system, or a dotted address (e.g. boi.17c.$sys.av)"`
	Component string `path:"component" doc:"Name of the component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
}

type listSystemMembersOutput struct {
	Body struct {
		System  string             `json:"system"`
		Members []systemMemberBody `json:"members"`
	}
}

type listComponentMembershipsOutput struct {
	Body struct {
		Component   string             `json:"component"`
		Memberships []systemMemberBody `json:"memberships"`
	}
}

// ErrComponentEndNeedsUpdate is the 403 a membership or role-assignment write
// returns when the caller holds no component:update at all (#510). Binding a
// component into a system, unbinding it, re-defaulting it and staffing it all
// write onto the component (its primary membership, its label, the system band
// of every cascade it reads), so the write costs a write on both ends: the
// system's system:update and the component's component:update. It names the
// permission, as ErrSystemBindNeedsUpdate does for the create that writes the
// same row from the other end, because a refusal an operator cannot act on sends
// them to an administrator with no ask.
const ErrComponentEndNeedsUpdate = "a membership or a role assignment writes onto its component as well as its system (the component's primary membership, its label, the system band of its cascade), so it requires component:update as well as system:update. Ask for component:update over the component."

// ErrComponentEndOutOfScope is the 403 for the scope half of the same gate: the
// caller holds component:update, but not over this component. It is reached
// only for a component the caller may read, or one already bound into a system
// it reads (whose member list names it), so naming the scope discloses nothing
// a GET would not.
const ErrComponentEndOutOfScope = "this component is outside the component:update scope your grants resolve to; a membership or a role assignment writes onto its component as well as its system, so both must be in scope. Ask for a component:update grant covering it."

// requireComponentEndPermission is the permission half of the component end
// (#510): every route that names a component in a membership or an assignment
// enforces component:update beside its system:update gate, and publishes it as
// the route's conditional permission (the condition being the component the
// path always names). The scope half is the gateway's.
func requireComponentEndPermission(ctx context.Context, a *authenticator) error {
	if !a.allows(ctx, "component", "update") {
		return huma.Error403Forbidden(ErrComponentEndNeedsUpdate)
	}
	return nil
}

// registerMemberRoutes wires membership: which components are in a system, which
// systems a component is in, and the writes that bind and unbind. The two reads
// are the same relation from either end, and the component-side one is the answer
// a single pointer could never give, since a shared device is in several systems
// at once.
func registerMemberRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-members",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/members",
		Summary:     "List the components in a system",
		Description: "The components bound into this system, ordered by name. Membership is what a role attaches to: every component staffing a role here is a member, and a member may also carry no role at all (a power conditioner is in the room without filling a declared slot). Each member carries its own verdict, so a system's view can poll its members' health here rather than reading the whole fleet. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemMembersInput) (*listSystemMembersOutput, error) {
		ms, err := gw.ListMembers(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapMemberErr(err)
		}
		out := &listSystemMembersOutput{}
		out.Body.System = in.Name
		out.Body.Members = toSystemMemberBodies(ms)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-memberships",
		Method:      http.MethodGet,
		Path:        "/components/{name}/memberships",
		Summary:     "List the systems a component is in",
		Description: "The systems this component is bound into, ordered by name. A component may belong to several: a rack DSP serving three rooms is a member of all three, and each of them depends on it. Exactly one membership may be marked primary, the default for a question asked without a system in hand. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentMembershipsInput) (*listComponentMembershipsOutput, error) {
		ms, err := gw.ComponentMemberships(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapMemberErr(err)
		}
		out := &listComponentMembershipsOutput{}
		out.Body.Component = in.Name
		out.Body.Memberships = toSystemMemberBodies(ms)
		return out, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "add-system-member",
		Method:        http.MethodPut,
		Path:          "/systems/{name}/members/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Put a component in a system",
		Description:   "Binds this component into the system. Idempotent. A component's first membership becomes its primary with nobody asking, so a component in exactly one system never has to think about the concept; a later membership does not take that default away. Gated by system:update, and also needs component:update, because the binding writes onto the component (its primary, its label, the system band of its cascade): each end resolves in its own tier's read scope and must be inside its update scope, so an end outside the read scope is a non-disclosing 404 and one readable but not writable a 403.",
	}, "system", "update"), "component", "update"), func(ctx context.Context, in *systemMemberPathInput) (*struct{}, error) {
		if err := requireComponentEndPermission(ctx, a); err != nil {
			return nil, err
		}
		if err := gw.AddMember(ctx, actorID(ctx), in.Name, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"),
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapMemberErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "remove-system-member",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/members/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Take a component out of a system",
		Description:   "Unbinds this component from the system. Refused with a 409 while it still fills a role here, since removing it would leave the system staffed by a non-member: unassign the role first. A component that was not a member is a 404. Gated by system:update, and also needs component:update, because unbinding moves the component's primary and restamps its label; read and update scopes drive the system's 404 versus 403 split, and a member outside the component:update scope is a 403 (the system's member list already names it).",
	}, "system", "update"), "component", "update"), func(ctx context.Context, in *systemMemberPathInput) (*struct{}, error) {
		if err := requireComponentEndPermission(ctx, a); err != nil {
			return nil, err
		}
		if err := gw.RemoveMember(ctx, actorID(ctx), in.Name, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"),
			a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapMemberErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "set-primary-member",
		Method:        http.MethodPost,
		Path:          "/systems/{name}/members/{component}:setPrimary",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Make this the component's default system",
		Description:   "Moves the component's default to this membership. The default answers questions asked without a system in hand; it does not decide anything that names a system explicitly. A component that was not a member here is a 404. Gated by system:update, and also needs component:update, because the default decides which system's band every cascade on the component reads; read and update scopes drive the system's 404 versus 403 split, and a member outside the component:update scope is a 403 (the system's member list already names it).",
	}, "system", "update"), "component", "update"), func(ctx context.Context, in *systemMemberPathInput) (*struct{}, error) {
		if err := requireComponentEndPermission(ctx, a); err != nil {
			return nil, err
		}
		if err := gw.SetPrimaryMember(ctx, actorID(ctx), in.Name, in.Component,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"),
			a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapMemberErr(err)
		}
		return nil, nil
	})
}

func mapMemberErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrMemberNotFound):
		return huma.Error404NotFound("component is not a member of this system")
	case errors.Is(err, storage.ErrMemberOccupied):
		return huma.Error409Conflict("component still fills a role in this system; unassign the role first")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	// The component end's scope half (#510), before the system's own forbidden:
	// both are 403s, but only this one can say which grant to ask for.
	case errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden(ErrComponentEndOutOfScope)
	case errors.Is(err, storage.ErrSystemForbidden):
		return huma.Error403Forbidden("forbidden")
	default:
		return err
	}
}
