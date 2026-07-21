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
	System    string `json:"system" doc:"Technical name of the system"`
	Component string `json:"component" doc:"Technical name of the component"`
	Primary   bool   `json:"primary" doc:"Whether this membership is the component's default when no system is given"`
}

func toSystemMemberBody(m storage.Member) systemMemberBody {
	return systemMemberBody{System: m.SystemID, Component: m.ComponentID, Primary: m.IsPrimary}
}

func toSystemMemberBodies(ms []storage.Member) []systemMemberBody {
	out := make([]systemMemberBody, 0, len(ms))
	for _, m := range ms {
		out = append(out, toSystemMemberBody(m))
	}
	return out
}

type systemMembersInput struct {
	Name string `path:"name" doc:"Technical name of the system"`
}

type componentMembershipsInput struct {
	Name string `path:"name" doc:"Technical name of the component"`
}

type systemMemberPathInput struct {
	Name      string `path:"name" doc:"Technical name of the system"`
	Component string `path:"component" doc:"Technical name of the component"`
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
		Description: "The components bound into this system, ordered by name. Membership is what a role attaches to: every component staffing a role here is a member, and a member may also carry no role at all (a power conditioner is in the room without filling a declared slot). Gated by system:read; an out-of-scope system is a non-disclosing 404.",
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

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "add-system-member",
		Method:        http.MethodPut,
		Path:          "/systems/{name}/members/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Put a component in a system",
		Description:   "Binds this component into the system. Idempotent. A component's first membership becomes its primary with nobody asking, so a component in exactly one system never has to think about the concept; a later membership does not take that default away. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *systemMemberPathInput) (*struct{}, error) {
		if err := gw.AddMember(ctx, actorID(ctx), in.Name, in.Component,
			a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapMemberErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "remove-system-member",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/members/{component}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Take a component out of a system",
		Description:   "Unbinds this component from the system. Refused with a 409 while it still fills a role here, since removing it would leave the system staffed by a non-member: unassign the role first. A component that was not a member is a 404. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *systemMemberPathInput) (*struct{}, error) {
		if err := gw.RemoveMember(ctx, actorID(ctx), in.Name, in.Component,
			a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapMemberErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "set-primary-member",
		Method:        http.MethodPost,
		Path:          "/systems/{name}/members/{component}:setPrimary",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Make this the component's default system",
		Description:   "Moves the component's default to this membership. The default answers questions asked without a system in hand; it does not decide anything that names a system explicitly. A component that was not a member here is a 404. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *systemMemberPathInput) (*struct{}, error) {
		if err := gw.SetPrimaryMember(ctx, actorID(ctx), in.Name, in.Component,
			a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapMemberErr(err)
		}
		return nil, nil
	})
}

func mapMemberErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrMemberNotFound):
		return huma.Error404NotFound("component is not a member of this system")
	case errors.Is(err, storage.ErrMemberOccupied):
		return huma.Error409Conflict("component still fills a role in this system; unassign the role first")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrSystemForbidden), errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden("forbidden")
	default:
		return err
	}
}
