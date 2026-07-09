package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The principal-group admin surface: a group holds role x scope grants that its
// members inherit, so an admin assigns access to a team once instead of per user.
// Group management is all-scope admin work, gated by principal_group; granting to
// a group reuses the principal_grant capability and the same escalation
// cover-check as a direct grant. Inheritance itself lives in the storage grant
// loader (a member's group grants are unioned into its grants), so nothing here
// resolves scope: the gateway does that from the loaded grants.

type groupBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"member_count" doc:"How many principals belong to the group (populated on list and get; 0 from create/update)."`
	GrantCount  int    `json:"grant_count" doc:"How many grants the group confers on its members."`
}

func toGroupBody(g *storage.Group) groupBody {
	return groupBody{ID: g.ID, Name: g.Name, DisplayName: g.DisplayName, Description: g.Description, MemberCount: g.MemberCount, GrantCount: g.GrantCount}
}

type memberBody struct {
	PrincipalID string `json:"principal_id"`
	Kind        string `json:"kind"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type groupPathInput struct {
	ID string `path:"id" doc:"The group's id (uuid)"`
}
type groupOutput struct {
	Body groupBody
}
type listGroupsOutput struct {
	Body struct {
		Groups []groupBody `json:"groups"`
	}
}
type createGroupInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"200" doc:"Unique group name"`
		DisplayName string `json:"display_name,omitempty" maxLength:"200"`
		Description string `json:"description,omitempty" maxLength:"1000"`
	}
}
type updateGroupInput struct {
	ID   string `path:"id" doc:"The group's id (uuid)"`
	Body struct {
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"200" doc:"Group name; renaming is safe"`
		DisplayName *string `json:"display_name,omitempty" maxLength:"200" doc:"Display name; empty clears it"`
		Description *string `json:"description,omitempty" maxLength:"1000" doc:"Description; empty clears it"`
	}
}
type addMemberInput struct {
	ID   string `path:"id" doc:"The group's id (uuid)"`
	Body struct {
		PrincipalID string `json:"principal_id" minLength:"1" doc:"The principal to add to the group"`
	}
}
type memberPathInput struct {
	ID          string `path:"id" doc:"The group's id (uuid)"`
	PrincipalID string `path:"principalId" doc:"The member principal's id (uuid)"`
}
type listMembersOutput struct {
	Body struct {
		Members []memberBody `json:"members"`
	}
}
type createGroupGrantInput struct {
	ID   string `path:"id" doc:"The group's id (uuid)"`
	Body struct {
		Role      string `json:"role" minLength:"1" doc:"A role id (viewer, operator, admin, owner, or a custom role)"`
		ScopeKind string `json:"scope_kind" enum:"all,location,system,component,group" doc:"The scope kind; 'all' confers the whole estate"`
		ScopeID   string `json:"scope_id,omitempty" doc:"The scope root id; omit for the all scope"`
		ScopeOp   string `json:"scope_op,omitempty" enum:"subtree,subtree_excl_root,self" doc:"How the scope root matches the tree; moot for the all scope"`
	}
}
type revokeGroupGrantInput struct {
	ID      string `path:"id" doc:"The group's id (uuid)"`
	GrantID string `path:"grantId" doc:"The grant's id (uuid)"`
}
type listGroupGrantsOutput struct {
	Body struct {
		Grants []grantBody `json:"grants"`
	}
}

// registerPrincipalGroupRoutes wires the group admin surface: group CRUD, member
// management, and grant assignment. All are all-scope admin operations.
func registerPrincipalGroupRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-groups",
		Method:      http.MethodGet,
		Path:        "/principal-groups",
		Summary:     "List principal groups",
		Description: "Every principal group. Gated by principal_group:read (all-scope).",
		Middlewares: huma.Middlewares{a.authn, a.require("principal_group", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listGroupsOutput, error) {
		groups, err := gw.ListGroups(ctx, a.scopeFor(ctx, "principal_group", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		out := &listGroupsOutput{}
		out.Body.Groups = make([]groupBody, 0, len(groups))
		for i := range groups {
			out.Body.Groups = append(out.Body.Groups, toGroupBody(&groups[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-group",
		Method:      http.MethodGet,
		Path:        "/principal-groups/{id}",
		Summary:     "Get a principal group",
		Description: "One principal group by id. Gated by principal_group:read (all-scope).",
		Middlewares: huma.Middlewares{a.authn, a.require("principal_group", "read")},
	}, func(ctx context.Context, in *groupPathInput) (*groupOutput, error) {
		g, err := gw.GetGroup(ctx, in.ID, a.scopeFor(ctx, "principal_group", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-group",
		Method:        http.MethodPost,
		Path:          "/principal-groups",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a principal group",
		Description:   "Creates a principal group. Gated by principal_group:create (all-scope). A duplicate name is 409.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_group", "create")},
	}, func(ctx context.Context, in *createGroupInput) (*groupOutput, error) {
		g, err := gw.CreateGroup(ctx, actorID(ctx), storage.GroupSpec{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName, Description: in.Body.Description,
		}, a.scopeFor(ctx, "principal_group", "create"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-group",
		Method:      http.MethodPatch,
		Path:        "/principal-groups/{id}",
		Summary:     "Update a principal group",
		Description: "Updates a group's name and presentational fields. Gated by principal_group:update (all-scope). A duplicate name is 409.",
		Middlewares: huma.Middlewares{a.authn, a.require("principal_group", "update")},
	}, func(ctx context.Context, in *updateGroupInput) (*groupOutput, error) {
		g, err := gw.UpdateGroup(ctx, actorID(ctx), in.ID, storage.GroupPatch{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName, Description: in.Body.Description,
		}, a.scopeFor(ctx, "principal_group", "update"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-group",
		Method:        http.MethodDelete,
		Path:          "/principal-groups/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a principal group",
		Description:   "Removes a group and, by cascade, its memberships and grants. Gated by principal_group:delete (all-scope).",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_group", "delete")},
	}, func(ctx context.Context, in *groupPathInput) (*struct{}, error) {
		if err := gw.DeleteGroup(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "principal_group", "delete")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-group-members",
		Method:      http.MethodGet,
		Path:        "/principal-groups/{id}/members",
		Summary:     "List a group's members",
		Description: "The principals in a group. Gated by principal_group:read (all-scope).",
		Middlewares: huma.Middlewares{a.authn, a.require("principal_group", "read")},
	}, func(ctx context.Context, in *groupPathInput) (*listMembersOutput, error) {
		members, err := gw.ListGroupMembers(ctx, in.ID, a.scopeFor(ctx, "principal_group", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		out := &listMembersOutput{}
		out.Body.Members = make([]memberBody, 0, len(members))
		for _, m := range members {
			out.Body.Members = append(out.Body.Members, memberBody{PrincipalID: m.PrincipalID, Kind: m.Kind, Username: m.Username, DisplayName: m.DisplayName})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "add-group-member",
		Method:        http.MethodPost,
		Path:          "/principal-groups/{id}/members",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Add a member to a group",
		Description:   "Adds a principal to a group; its members inherit the group's grants. Gated by principal_group:update (all-scope). Idempotent.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_group", "update")},
	}, func(ctx context.Context, in *addMemberInput) (*struct{}, error) {
		if err := gw.AddGroupMember(ctx, actorID(ctx), in.ID, in.Body.PrincipalID, a.scopeFor(ctx, "principal_group", "update")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "remove-group-member",
		Method:        http.MethodDelete,
		Path:          "/principal-groups/{id}/members/{principalId}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove a member from a group",
		Description:   "Removes a principal from a group; it stops inheriting the group's grants. Gated by principal_group:update (all-scope).",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_group", "update")},
	}, func(ctx context.Context, in *memberPathInput) (*struct{}, error) {
		if err := gw.RemoveGroupMember(ctx, actorID(ctx), in.ID, in.PrincipalID, a.scopeFor(ctx, "principal_group", "update")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-group-grants",
		Method:      http.MethodGet,
		Path:        "/principal-groups/{id}/grants",
		Summary:     "List a group's grants",
		Description: "The role x scope grants a group confers on its members. Gated by principal_group:read (all-scope).",
		Middlewares: huma.Middlewares{a.authn, a.require("principal_group", "read")},
	}, func(ctx context.Context, in *groupPathInput) (*listGroupGrantsOutput, error) {
		grants, err := gw.ListGroupGrants(ctx, in.ID, a.scopeFor(ctx, "principal_group", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		out := &listGroupGrantsOutput{}
		out.Body.Grants = make([]grantBody, 0, len(grants))
		for i := range grants {
			out.Body.Grants = append(out.Body.Grants, toGrantBody(&grants[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-group-grant",
		Method:        http.MethodPost,
		Path:          "/principal-groups/{id}/grants",
		DefaultStatus: http.StatusCreated,
		Summary:       "Grant a role to a group",
		Description:   "Assigns a role at a scope to a group; its members inherit it. Gated by principal_grant:create (all-scope). Refused (403) when the granted role's capabilities exceed the granter's own, exactly as for a direct grant. A duplicate is 409.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_grant", "create")},
	}, func(ctx context.Context, in *createGroupGrantInput) (*grantOutput, error) {
		ok, err := a.grantCoverOK(ctx, in.Body.Role)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, huma.Error403Forbidden("cannot grant a role whose capabilities exceed yours")
		}
		g, err := gw.CreateGroupGrant(ctx, actorID(ctx), in.ID, storage.GrantSpec{
			Role: in.Body.Role, ScopeKind: in.Body.ScopeKind, ScopeID: in.Body.ScopeID, ScopeOp: in.Body.ScopeOp,
		}, a.scopeFor(ctx, "principal_grant", "create"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &grantOutput{Body: toGrantBody(g)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-group-grant",
		Method:        http.MethodDelete,
		Path:          "/principal-groups/{id}/grants/{grantId}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Revoke a group grant",
		Description:   "Removes one grant from a group. Gated by principal_grant:delete (all-scope).",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_grant", "delete")},
	}, func(ctx context.Context, in *revokeGroupGrantInput) (*struct{}, error) {
		if err := gw.RevokeGroupGrant(ctx, actorID(ctx), in.ID, in.GrantID, a.scopeFor(ctx, "principal_grant", "delete")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})
}
