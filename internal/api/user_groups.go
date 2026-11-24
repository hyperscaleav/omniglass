package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The user-group admin surface: a group holds role x scope grants that its
// members inherit, so an admin assigns access to a team once instead of per user.
// Group management is all-scope admin work, gated by user_group; granting to
// a group reuses the user_grant capability and the same escalation
// cover-check as a direct grant. Inheritance itself lives in the storage grant
// loader (a member's group grants are unioned into its grants), so nothing here
// resolves scope: the gateway does that from the loaded grants.

type groupBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"member_count" doc:"How many users belong to the group (populated on list and get; 0 from create/update)."`
	GrantCount  int    `json:"grant_count" doc:"How many grants the group confers on its members."`
}

func toGroupBody(g *storage.Group) groupBody {
	return groupBody{ID: g.ID, Name: g.Name, Label: g.Label, Description: g.Description, MemberCount: g.MemberCount, GrantCount: g.GrantCount}
}

// memberBody is one user on a group's roster. The two facts are kept
// apart: `name` is the identifier, whichever kind the member is (a human's
// username, a machine user's name), and `label` is the friendly
// string, present only where a human set one. It used to be one field carrying
// either, which crossed an identifier with a label and is what #563 ended.
type memberBody struct {
	UserID string `json:"user_id"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty" doc:"The member's identifier: a person's username, or a machine user's name"`
	Label  string `json:"label,omitempty" doc:"The friendly string, where the member is a human who has one"`
	Source string `json:"source" enum:"local,idp" doc:"Where the membership comes from: local (an admin added it) or idp (synced from an identity provider's groups claim at sign-in and re-check, through a mapping)"`
	Issuer string `json:"issuer,omitempty" doc:"The identity provider (its issuer) whose groups claim granted an idp membership"`
}

// groupMappingBody is one identity provider group mapping.
type groupMappingBody struct {
	ID         string    `json:"id"`
	GroupID    string    `json:"group_id" doc:"The group's uuid, the handle"`
	Group      string    `json:"group" doc:"The group's name, for display"`
	Issuer     string    `json:"issuer" doc:"The identity provider's issuer the claim value is read from"`
	ClaimValue string    `json:"claim_value" doc:"A value of the provider's groups claim"`
	CreatedAt  time.Time `json:"created_at"`
}

func toGroupMappingBody(m *storage.GroupIdPMapping) groupMappingBody {
	return groupMappingBody{ID: m.ID, GroupID: m.GroupID, Group: m.Group, Issuer: m.Issuer, ClaimValue: m.ClaimValue, CreatedAt: m.CreatedAt}
}

type listGroupMappingsOutput struct {
	Body struct {
		Mappings []groupMappingBody `json:"mappings"`
	}
}
type groupMappingOutput struct {
	Body groupMappingBody
}
type createGroupMappingInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	Body  struct {
		ClaimValue string `json:"claim_value" minLength:"1" maxLength:"512" doc:"The value of the identity provider's groups claim that puts a person in this group"`
		Issuer     string `json:"issuer,omitempty" maxLength:"2048" doc:"The identity provider's issuer, exactly as its tokens carry it; omit for the configured provider's"`
	}
}
type groupMappingPathInput struct {
	Group   string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	Mapping string `path:"mappingId" doc:"The mapping's id (uuid)"`
}

type groupPathInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
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
		Name        string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Unique group name (lowercase letters, digits, and hyphens)"`
		Label       string `json:"label,omitempty" maxLength:"200" doc:"What an operator reads in lists"`
		Description string `json:"description,omitempty" maxLength:"1000" doc:"Free-form notes on what the group is for"`
	}
}

// updateGroupInput is the PATCH body. It deliberately carries no name: a rename is
// the :rename custom method, gated by user_group:rename.
type updateGroupInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	Body  struct {
		Label       *string `json:"label,omitempty" maxLength:"200" doc:"Label; empty clears it"`
		Description *string `json:"description,omitempty" maxLength:"1000" doc:"Description; empty clears it"`
	}
}

// renameGroupInput is the :rename body. The name rule lives here, in the contract,
// not only in the prose below it.
type renameGroupInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	Body  struct {
		Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The new unique group name (lowercase letters, digits, hyphens)"`
	}
}
type addMemberInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	Body  struct {
		UserID string `json:"user_id" minLength:"1" doc:"The user to add: a person's username, a machine user's name, or its uuid"`
	}
}
type memberPathInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	User  string `path:"user" doc:"The member: a person's username, a machine user's name, or the user's uuid" example:"alice"`
}
type listMembersOutput struct {
	Body struct {
		Members []memberBody `json:"members"`
	}
}
type createGroupGrantInput struct {
	Group string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	Body  struct {
		Role      string `json:"role" minLength:"1" doc:"A role id (viewer, operator, admin, owner, or a custom role)"`
		ScopeKind string `json:"scope_kind" enum:"all,location,system,component" doc:"The scope kind; 'all' confers the whole fleet (group-as-scope is unbuilt and not offered)"`
		ScopeID   string `json:"scope_id,omitempty" doc:"The scope root, the location, system or component the scope kind names, by name, dotted address or uuid (stored as its uuid); omit for the all scope"`
		ScopeOp   string `json:"scope_op,omitempty" enum:"subtree,subtree_excl_root,self" doc:"How the scope root matches the tree; moot for the all scope"`
	}
}
type revokeGroupGrantInput struct {
	Group   string `path:"group" doc:"The group, by name or uuid" example:"night-shift"`
	GrantID string `path:"grantId" doc:"The grant's id (uuid)"`
}
type listGroupGrantsOutput struct {
	Body struct {
		Grants []grantBody `json:"grants"`
	}
}

// registerUserGroupRoutes wires the group admin surface: group CRUD, member
// management, and grant assignment. All are all-scope admin operations.
func registerUserGroupRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-groups",
		Method:      http.MethodGet,
		Path:        "/user-groups",
		Summary:     "List user groups",
		Description: "Every user group. Gated by user_group:read:admin.",
	}, "user_group", "read", "admin"), func(ctx context.Context, _ *struct{}) (*listGroupsOutput, error) {
		groups, err := gw.ListGroups(ctx, a.scopeFor(ctx, "user_group", "read"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		out := &listGroupsOutput{}
		out.Body.Groups = make([]groupBody, 0, len(groups))
		for i := range groups {
			out.Body.Groups = append(out.Body.Groups, toGroupBody(&groups[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-group",
		Method:      http.MethodGet,
		Path:        "/user-groups/{group}",
		Summary:     "Get a user group",
		Description: "One user group, by name or uuid. Gated by user_group:read:admin.",
	}, "user_group", "read", "admin"), func(ctx context.Context, in *groupPathInput) (*groupOutput, error) {
		g, err := gw.GetGroup(ctx, in.Group, a.scopeFor(ctx, "user_group", "read"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-group",
		Method:        http.MethodPost,
		Path:          "/user-groups",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a user group",
		Description:   "Creates a user group. Gated by user_group:create (all-scope). A duplicate name is 409.",
	}, "user_group", "create"), func(ctx context.Context, in *createGroupInput) (*groupOutput, error) {
		g, err := gw.CreateGroup(ctx, actorID(ctx), storage.GroupSpec{
			Name: in.Body.Name, Label: in.Body.Label, Description: in.Body.Description,
		}, a.scopeFor(ctx, "user_group", "create"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-group",
		Method:      http.MethodPatch,
		Path:        "/user-groups/{group}",
		Summary:     "Update a user group",
		Description: "Updates a group's presentational fields. The name is not patchable: renaming is the :rename custom method. Gated by user_group:update (all-scope).",
	}, "user_group", "update"), func(ctx context.Context, in *updateGroupInput) (*groupOutput, error) {
		g, err := gw.UpdateGroup(ctx, actorID(ctx), in.Group, storage.GroupPatch{
			Label: in.Body.Label, Description: in.Body.Description,
		}, a.scopeFor(ctx, "user_group", "update"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "rename-group",
		Method:      http.MethodPost,
		Path:        "/user-groups/{group}:rename",
		Summary:     "Rename a user group",
		Description: "Moves the group's name. A separate act from an update, and a separately grantable one, because it breaks the references stored outside this system; inside it nothing breaks, since membership and grants both key on the group's uuid. A taken name is a 409, an illegal or uuid-shaped one a 422. Gated by user_group:rename (all-scope).",
	}, "user_group", "rename"), func(ctx context.Context, in *renameGroupInput) (*groupOutput, error) {
		g, err := gw.RenameGroup(ctx, actorID(ctx), in.Group, in.Body.Name, a.scopeFor(ctx, "user_group", "rename"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &groupOutput{Body: toGroupBody(g)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-group",
		Method:        http.MethodDelete,
		Path:          "/user-groups/{group}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a user group",
		Description:   "Removes a group and, by cascade, its memberships and grants. A group a notification target notifies cannot be deleted (a 409): the refusal names the target to a caller who reads targets. Gated by user_group:delete (all-scope).",
	}, "user_group", "delete"), func(ctx context.Context, in *groupPathInput) (*struct{}, error) {
		if err := gw.DeleteGroup(ctx, actorID(ctx), in.Group, a.scopeFor(ctx, "user_group", "delete")); err != nil {
			return nil, mapBoundErr(ctx, a, err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-group-members",
		Method:      http.MethodGet,
		Path:        "/user-groups/{group}/members",
		Summary:     "List a group's members",
		Description: "The users in a group, each with its source: local (an admin added it) or idp (synced from an identity provider's groups claim through a mapping, with the issuer). Gated by user_group:read:admin.",
	}, "user_group", "read", "admin"), func(ctx context.Context, in *groupPathInput) (*listMembersOutput, error) {
		members, err := gw.ListGroupMembers(ctx, in.Group, a.scopeFor(ctx, "user_group", "read"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		out := &listMembersOutput{}
		out.Body.Members = make([]memberBody, 0, len(members))
		for _, m := range members {
			out.Body.Members = append(out.Body.Members, memberBody{UserID: m.UserID, Kind: m.Kind, Name: m.Name, Label: m.Label,
				Source: m.Source, Issuer: m.Issuer})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "add-group-member",
		Method:        http.MethodPost,
		Path:          "/user-groups/{group}/members",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Add a member to a group",
		Description:   "Adds a user to a group; its members inherit the group's grants. The membership is local: a member an identity provider's groups brought (idp) becomes local, so it no longer follows the provider. Gated by user_group:update (all-scope). Idempotent. Under an agent token or the agent ceiling, refused (403) when the group's grants exceed the caller's bounded permissions, since membership is a grant by another route.",
	}, "user_group", "update"), func(ctx context.Context, in *addMemberInput) (*struct{}, error) {
		if err := a.groupMemberCoverOK(ctx, gw, in.Group); err != nil {
			return nil, err
		}
		if err := gw.AddGroupMember(ctx, actorID(ctx), in.Group, in.Body.UserID, a.scopeFor(ctx, "user_group", "update")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "remove-group-member",
		Method:        http.MethodDelete,
		Path:          "/user-groups/{group}/members/{user}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove a member from a group",
		Description:   "Removes a user from a group; it stops inheriting the group's grants. An idp membership comes back at the person's next sign-in or re-check while a mapping still puts them in the group. Gated by user_group:update (all-scope).",
	}, "user_group", "update"), func(ctx context.Context, in *memberPathInput) (*struct{}, error) {
		if err := gw.RemoveGroupMember(ctx, actorID(ctx), in.Group, in.User, a.scopeFor(ctx, "user_group", "update")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-group-grants",
		Method:      http.MethodGet,
		Path:        "/user-groups/{group}/grants",
		Summary:     "List a group's grants",
		Description: "The role x scope grants a group confers on its members. Gated by user_group:read:admin.",
	}, "user_group", "read", "admin"), func(ctx context.Context, in *groupPathInput) (*listGroupGrantsOutput, error) {
		grants, err := gw.ListGroupGrants(ctx, in.Group, a.scopeFor(ctx, "user_group", "read"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		out := &listGroupGrantsOutput{}
		out.Body.Grants = make([]grantBody, 0, len(grants))
		for i := range grants {
			out.Body.Grants = append(out.Body.Grants, toGrantBody(&grants[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-group-grant",
		Method:        http.MethodPost,
		Path:          "/user-groups/{group}/grants",
		DefaultStatus: http.StatusCreated,
		Summary:       "Grant a role to a group",
		Description:   "Assigns a role at a scope to a group; its members inherit it. Gated by user_grant:create (all-scope). Refused (403) when the granted role's capabilities exceed the granter's own, exactly as for a direct grant. A duplicate is 409, and so is an owner grant on a group an identity provider group mapping feeds (no provider group reaches an owner).",
	}, "user_grant", "create"), func(ctx context.Context, in *createGroupGrantInput) (*grantOutput, error) {
		ok, err := a.grantCoverOK(ctx, in.Body.Role)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, huma.Error403Forbidden("cannot grant a role whose capabilities exceed yours")
		}
		g, err := gw.CreateGroupGrant(ctx, actorID(ctx), in.Group, storage.GrantSpec{
			Role: in.Body.Role, ScopeKind: in.Body.ScopeKind, ScopeID: in.Body.ScopeID, ScopeOp: in.Body.ScopeOp,
		}, a.scopeFor(ctx, "user_grant", "create"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &grantOutput{Body: toGrantBody(g)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "revoke-group-grant",
		Method:        http.MethodDelete,
		Path:          "/user-groups/{group}/grants/{grantId}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Revoke a group grant",
		Description:   "Removes one grant from a group. Gated by user_grant:delete (all-scope).",
	}, "user_grant", "delete"), func(ctx context.Context, in *revokeGroupGrantInput) (*struct{}, error) {
		if err := gw.RevokeGroupGrant(ctx, actorID(ctx), in.Group, in.GrantID, a.scopeFor(ctx, "user_grant", "delete")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-group-idp-mappings",
		Method:      http.MethodGet,
		Path:        "/user-groups/{group}/idp-mappings",
		Summary:     "List a group's identity provider mappings",
		Description: "The identity provider groups that put a person in this group: each a value of the provider's groups claim, keyed by its issuer. At each single sign-on sign-in, and each re-check refresh whose id_token carries the claim, a person's idp memberships from that issuer are set to exactly the mapped set. Gated by user_group:read:admin.",
	}, "user_group", "read", "admin"), func(ctx context.Context, in *groupPathInput) (*listGroupMappingsOutput, error) {
		ms, err := gw.ListGroupIdPMappings(ctx, in.Group, a.scopeFor(ctx, "user_group", "read"))
		if err != nil {
			return nil, mapGroupMappingErr(err)
		}
		out := &listGroupMappingsOutput{}
		out.Body.Mappings = make([]groupMappingBody, 0, len(ms))
		for i := range ms {
			out.Body.Mappings = append(out.Body.Mappings, toGroupMappingBody(&ms[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-group-idp-mapping",
		Method:        http.MethodPost,
		Path:          "/user-groups/{group}/idp-mappings",
		DefaultStatus: http.StatusCreated,
		Summary:       "Map an identity provider group to a group",
		Description:   "Maps a value of the identity provider's groups claim to this group, so a person whose token carries it becomes an idp member at their next sign-in and inherits the group's grants. The issuer defaults to the configured provider's; with none configured and none given it is a 422. Refused (409) when the group carries an owner grant at any scope, since no provider group may reach an owner, and when the group already maps the value. Gated by user_group:update (all-scope). Under an agent token or the agent ceiling, refused (403) when the group's grants exceed the caller's bounded permissions, as adding a member is.",
	}, "user_group", "update"), func(ctx context.Context, in *createGroupMappingInput) (*groupMappingOutput, error) {
		if err := a.groupMemberCoverOK(ctx, gw, in.Group); err != nil {
			return nil, err
		}
		m, err := gw.CreateGroupIdPMapping(ctx, actorID(ctx), in.Group, in.Body.Issuer, in.Body.ClaimValue, a.scopeFor(ctx, "user_group", "update"))
		if err != nil {
			return nil, mapGroupMappingErr(err)
		}
		return &groupMappingOutput{Body: toGroupMappingBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-group-idp-mapping",
		Method:        http.MethodDelete,
		Path:          "/user-groups/{group}/idp-mappings/{mappingId}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove an identity provider group mapping",
		Description:   "Removes one mapping. When it was the group's last mapping from its issuer, that issuer's idp memberships of the group are removed with it; otherwise they are re-judged at each person's next sign-in or re-check. Local memberships are never touched. Gated by user_group:update (all-scope).",
	}, "user_group", "update"), func(ctx context.Context, in *groupMappingPathInput) (*struct{}, error) {
		if err := gw.DeleteGroupIdPMapping(ctx, actorID(ctx), in.Group, in.Mapping, a.scopeFor(ctx, "user_group", "update")); err != nil {
			return nil, mapGroupMappingErr(err)
		}
		return nil, nil
	})
}

// groupMemberCoverOK refuses a request bounded by an agent token or the agent
// ceiling (#1179) from adding a member to a group whose grants exceed its own
// bounded permissions: membership confers the group's grants, so it is a grant
// by another route. An unconstrained caller is unchanged. A non-nil error is
// already an HTTP error.
func (a *authenticator) groupMemberCoverOK(ctx context.Context, gw storage.Gateway, group string) error {
	if !constrained(ctx) {
		return nil
	}
	actor, ok := userFrom(ctx)
	if !ok {
		return huma.Error401Unauthorized("unauthenticated")
	}
	idx, err := a.roleIndex(ctx)
	if err != nil {
		return huma.Error500InternalServerError("add member failed")
	}
	grants, err := gw.ListGroupGrants(ctx, group, scope.Set{All: true})
	if err != nil {
		return mapUserErr(err)
	}
	roles := make([]string, 0, len(grants))
	for _, g := range grants {
		roles = append(roles, g.Role)
	}
	if !allScopePerms(ctx, idx, actor).Covers(idx.Flatten(roles)) {
		return huma.Error403Forbidden("cannot add a member to a group whose grants exceed yours")
	}
	return nil
}

// mapGroupMappingErr maps the identity provider group mapping errors, then
// the user-admin ones.
func mapGroupMappingErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrGroupMappingNotFound):
		return huma.Error404NotFound("mapping not found")
	case errors.Is(err, storage.ErrGroupMappingExists):
		return huma.Error409Conflict("the group already maps that claim value from that issuer")
	case errors.Is(err, storage.ErrGroupMappingOwner):
		return huma.Error409Conflict(errGroupMappingOwner)
	case errors.Is(err, storage.ErrGroupMappingNoIssuer):
		return huma.Error422UnprocessableEntity("name the issuer: single sign-on is not configured, so there is no provider to default to")
	default:
		return mapUserErr(err)
	}
}

// errGroupMappingOwner is the owner guard's refusal, from either side.
const errGroupMappingOwner = "an identity provider group may not reach an owner: a group mapped from a provider cannot carry an owner grant, and a group with an owner grant cannot be mapped"
