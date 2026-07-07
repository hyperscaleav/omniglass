package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// principalBody is the wire shape of a principal in the admin directory: its id
// and kind, the kind profile, and its grants. Credentials are deliberately not
// included, so no secret ever leaves the API.
type principalBody struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Active  bool        `json:"active"`
	Human   *humanBody  `json:"human,omitempty"`
	Service *svcBody    `json:"service,omitempty"`
	Grants  []grantBody `json:"grants"`
}

func toPrincipalBody(pr *storage.Principal) principalBody {
	b := principalBody{ID: pr.ID, Kind: pr.Kind, Active: pr.Active, Grants: make([]grantBody, 0, len(pr.Grants))}
	if pr.Human != nil {
		b.Human = &humanBody{Username: pr.Human.Username, Email: pr.Human.Email, DisplayName: pr.Human.DisplayName}
	}
	if pr.Service != nil {
		b.Service = &svcBody{Label: pr.Service.Label}
	}
	for _, g := range pr.Grants {
		gb := grantBody{ID: g.ID, Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
		if g.ScopeID != nil {
			gb.ScopeID = *g.ScopeID
		}
		b.Grants = append(b.Grants, gb)
	}
	return b
}

type listPrincipalsInput struct {
	Kind string `query:"kind" enum:"human,service" doc:"Optionally filter by principal kind"`
}

type listPrincipalsOutput struct {
	Body struct {
		Principals []principalBody `json:"principals"`
	}
}

type principalPathInput struct {
	ID string `path:"id" doc:"The principal's id (uuid)"`
}

type principalOutput struct {
	Body principalBody
}

type createPrincipalInput struct {
	Body struct {
		Username    string `json:"username" minLength:"1" maxLength:"200" doc:"Unique sign-in name"`
		DisplayName string `json:"display_name,omitempty" maxLength:"200"`
		Email       string `json:"email,omitempty" maxLength:"320"`
		Password    string `json:"password,omitempty" minLength:"8" maxLength:"256" doc:"Optional initial password; the user changes it after signing in"`
	}
}

type updatePrincipalInput struct {
	ID   string `path:"id" doc:"The principal's id (uuid)"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" maxLength:"200" doc:"Display name; empty clears it"`
		Email       *string `json:"email,omitempty" maxLength:"320" doc:"Email; empty clears it"`
		Username    *string `json:"username,omitempty" minLength:"1" maxLength:"200" doc:"Sign-in name; renaming is safe"`
	}
}

type createGrantInput struct {
	ID   string `path:"id" doc:"The principal's id (uuid)"`
	Body struct {
		Role      string `json:"role" minLength:"1" doc:"A role id (viewer, operator, admin, owner, or a custom role)"`
		ScopeKind string `json:"scope_kind" enum:"all,location,system,component,group" doc:"The scope kind; 'all' confers the whole estate"`
		ScopeID   string `json:"scope_id,omitempty" doc:"The scope root id; omit for the all scope"`
		ScopeOp   string `json:"scope_op,omitempty" enum:"subtree,subtree_excl_root,self" doc:"How the scope root matches the tree: subtree (root + descendants, the default), subtree_excl_root (descendants only for update/delete, root kept for read/create), or self (the root row only). Moot for the all scope."`
	}
}

type grantOutput struct {
	Body grantBody
}

type revokeGrantInput struct {
	ID      string `path:"id" doc:"The principal's id (uuid)"`
	GrantID string `path:"grantId" doc:"The grant's id (uuid)"`
}

// registerPrincipalRoutes wires the admin principal directory: list, get, and
// create a human. Each is gated by a principal capability, which resolves to an
// all-scope grant only (a principal is not a scope-tree entity), so the gateway
// refuses a location or system scope with a 403.
func registerPrincipalRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-principals",
		Method:      http.MethodGet,
		Path:        "/principals",
		Summary:     "List principals",
		Description: "Lists all principals (humans and service accounts) with their grants. Gated by principal:read, which confers access only at all-scope.",
		Middlewares: huma.Middlewares{a.authn, a.require("principal", "read")},
	}, func(ctx context.Context, in *listPrincipalsInput) (*listPrincipalsOutput, error) {
		prs, err := gw.ListPrincipals(ctx, a.scopeFor(ctx, "principal", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		out := &listPrincipalsOutput{}
		out.Body.Principals = make([]principalBody, 0, len(prs))
		for i := range prs {
			if in.Kind != "" && prs[i].Kind != in.Kind {
				continue
			}
			out.Body.Principals = append(out.Body.Principals, toPrincipalBody(&prs[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-principal",
		Method:      http.MethodGet,
		Path:        "/principals/{id}",
		Summary:     "Get a principal",
		Description: "Fetches one principal by id with its profile and grants. Gated by principal:read (all-scope).",
		Middlewares: huma.Middlewares{a.authn, a.require("principal", "read")},
	}, func(ctx context.Context, in *principalPathInput) (*principalOutput, error) {
		pr, err := gw.GetPrincipal(ctx, in.ID, a.scopeFor(ctx, "principal", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &principalOutput{Body: toPrincipalBody(pr)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-principal",
		Method:        http.MethodPost,
		Path:          "/principals",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a human principal",
		Description:   "Creates a human principal with an optional initial password. Gated by principal:create (all-scope). The new principal holds no grants; assign roles separately.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "create")},
	}, func(ctx context.Context, in *createPrincipalInput) (*principalOutput, error) {
		spec := storage.HumanSpec{
			Username:    in.Body.Username,
			Email:       in.Body.Email,
			DisplayName: in.Body.DisplayName,
		}
		if in.Body.Password != "" {
			hash, err := auth.HashPassword(in.Body.Password)
			if err != nil {
				return nil, huma.Error500InternalServerError("create principal")
			}
			spec.PasswordHash = hash
		}
		pr, err := gw.CreateHumanPrincipal(ctx, actorID(ctx), spec, a.scopeFor(ctx, "principal", "create"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &principalOutput{Body: toPrincipalBody(pr)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-principal",
		Method:      http.MethodPatch,
		Path:        "/principals/{id}",
		Summary:     "Update a principal",
		Description: "Updates a human principal's display name, email, and username. Gated by principal:update (all-scope). Renaming is safe: nothing keys on the username.",
		Middlewares: huma.Middlewares{a.authn, a.require("principal", "update")},
	}, func(ctx context.Context, in *updatePrincipalInput) (*principalOutput, error) {
		pr, err := gw.UpdatePrincipalHuman(ctx, actorID(ctx), in.ID, storage.AdminHumanPatch{
			DisplayName: in.Body.DisplayName,
			Email:       in.Body.Email,
			Username:    in.Body.Username,
		}, a.scopeFor(ctx, "principal", "update"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &principalOutput{Body: toPrincipalBody(pr)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-grant",
		Method:        http.MethodPost,
		Path:          "/principals/{id}/grants",
		DefaultStatus: http.StatusCreated,
		Summary:       "Grant a role to a principal",
		Description:   "Assigns a role at a scope to a principal. Gated by principal_grant:create (all-scope). Refused (403) when the granted role's capabilities exceed the granter's own (no promoting anyone, including yourself, to a higher tier such as owner). A duplicate is 409, an unknown role or bad scope 422.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_grant", "create")},
	}, func(ctx context.Context, in *createGrantInput) (*grantOutput, error) {
		// Escalation guard: a grant cannot confer a capability the granter lacks at
		// all-scope, so no caller can promote anyone (including itself) to a tier above
		// its own, e.g. admin granting owner (*:*). Mirrors the impersonation guard: only
		// the caller's all-scope grants count, so a capability held through a narrower
		// grant cannot be conferred estate-wide.
		actor, ok := principalFrom(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("unauthenticated")
		}
		idx, err := a.roleIndex(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("grant failed")
		}
		allScopeRoleIDs := make([]string, 0, len(actor.Grants))
		for _, gr := range actor.Grants {
			if gr.ScopeKind == "all" {
				allScopeRoleIDs = append(allScopeRoleIDs, gr.Role)
			}
		}
		if !idx.Flatten(allScopeRoleIDs).Covers(idx.Flatten([]string{in.Body.Role})) {
			return nil, huma.Error403Forbidden("cannot grant a role whose capabilities exceed yours")
		}
		g, err := gw.CreateGrant(ctx, actorID(ctx), in.ID, storage.GrantSpec{
			Role: in.Body.Role, ScopeKind: in.Body.ScopeKind, ScopeID: in.Body.ScopeID, ScopeOp: in.Body.ScopeOp,
		}, a.scopeFor(ctx, "principal_grant", "create"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		out := &grantOutput{Body: grantBody{ID: g.ID, Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}}
		if g.ScopeID != nil {
			out.Body.ScopeID = *g.ScopeID
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-grant",
		Method:        http.MethodDelete,
		Path:          "/principals/{id}/grants/{grantId}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Revoke a grant",
		Description:   "Removes one grant from a principal. Gated by principal_grant:delete (all-scope). The last owner grant cannot be revoked.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal_grant", "delete")},
	}, func(ctx context.Context, in *revokeGrantInput) (*struct{}, error) {
		if err := gw.RevokeGrant(ctx, actorID(ctx), in.ID, in.GrantID, a.scopeFor(ctx, "principal_grant", "delete")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "disable-principal",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:disable",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Disable a principal",
		Description:   "Soft-disables a principal so it can no longer authenticate; its audit trail is kept. Gated by principal:update (all-scope). The last active owner cannot be disabled.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "update")},
	}, func(ctx context.Context, in *principalPathInput) (*struct{}, error) {
		if err := gw.SetPrincipalActive(ctx, actorID(ctx), in.ID, false, a.scopeFor(ctx, "principal", "update")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "enable-principal",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:enable",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Enable a principal",
		Description:   "Re-enables a disabled principal, restoring its ability to authenticate. Gated by principal:update (all-scope).",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "update")},
	}, func(ctx context.Context, in *principalPathInput) (*struct{}, error) {
		if err := gw.SetPrincipalActive(ctx, actorID(ctx), in.ID, true, a.scopeFor(ctx, "principal", "update")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})
}

// mapPrincipalErr translates the gateway's principal sentinels into HTTP status:
// an unknown id 404, a non-all scope 403, a duplicate username 409.
func mapPrincipalErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrPrincipalNotFound):
		return huma.Error404NotFound("principal not found")
	case errors.Is(err, storage.ErrPrincipalForbidden):
		return huma.Error403Forbidden("principal management requires an all-scope grant")
	case errors.Is(err, storage.ErrUsernameTaken):
		return huma.Error409Conflict("username already exists")
	case errors.Is(err, storage.ErrPrincipalNotHuman):
		return huma.Error422UnprocessableEntity("only human principals have these fields")
	case errors.Is(err, storage.ErrLastOwner):
		return huma.Error409Conflict("cannot revoke the last owner grant")
	case errors.Is(err, storage.ErrGrantNotFound):
		return huma.Error404NotFound("grant not found")
	case errors.Is(err, storage.ErrGrantExists):
		return huma.Error409Conflict("that grant already exists")
	case errors.Is(err, storage.ErrUnknownRole):
		return huma.Error422UnprocessableEntity("unknown role")
	case errors.Is(err, storage.ErrBadScope):
		return huma.Error422UnprocessableEntity("a scoped grant needs a scope_id")
	default:
		return huma.Error500InternalServerError("principal operation failed")
	}
}
