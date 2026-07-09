package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// principalBody is the wire shape of a principal in the admin directory: its id
// and kind, the kind profile, and its grants. Credentials are deliberately not
// included, so no secret ever leaves the API.
// principalGroupRef names a group a principal belongs to, so the console can show
// membership and link through to the group.
type principalGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type principalBody struct {
	ID            string              `json:"id"`
	Kind          string              `json:"kind"`
	Active        bool                `json:"active"`
	ArchivedAt    *time.Time          `json:"archived_at,omitempty" doc:"Set when the principal is archived (soft-deleted); absent means live"`
	Human         *humanBody          `json:"human,omitempty"`
	Service       *svcBody            `json:"service,omitempty"`
	Grants        []grantBody         `json:"grants"`
	Groups        []principalGroupRef `json:"groups"`
}

func toPrincipalBody(pr *storage.Principal) principalBody {
	b := principalBody{ID: pr.ID, Kind: pr.Kind, Active: pr.Active, ArchivedAt: pr.ArchivedAt, Grants: make([]grantBody, 0, len(pr.Grants)), Groups: make([]principalGroupRef, 0, len(pr.Groups))}
	if pr.Human != nil {
		b.Human = &humanBody{Username: pr.Human.Username, Email: pr.Human.Email, DisplayName: pr.Human.DisplayName}
	}
	if pr.Service != nil {
		b.Service = &svcBody{Label: pr.Service.Label}
	}
	for i := range pr.Grants {
		b.Grants = append(b.Grants, toGrantBody(&pr.Grants[i]))
	}
	for _, gr := range pr.Groups {
		b.Groups = append(b.Groups, principalGroupRef{ID: gr.ID, Name: gr.Name})
	}
	return b
}

type listPrincipalsInput struct {
	Kind            string `query:"kind" enum:"human,service" doc:"Optionally filter by principal kind"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived (soft-deleted) principals, hidden by default"`
}

type listPrincipalsOutput struct {
	Body struct {
		Principals []principalBody `json:"principals"`
	}
}

type principalPathInput struct {
	ID string `path:"id" doc:"The principal's id (uuid)"`
}

type resetPasswordInput struct {
	ID   string `path:"id" doc:"The principal's id (uuid)"`
	Body struct {
		Password string `json:"password" minLength:"12" maxLength:"256" doc:"The new password (at least 12 characters, not a common password, not containing the username)"`
	}
}

type principalOutput struct {
	Body principalBody
}

type createPrincipalInput struct {
	Body struct {
		Username    string `json:"username" minLength:"1" maxLength:"200" pattern:"^[a-z0-9][a-z0-9._-]*$" doc:"Unique sign-in name (lowercase letters, digits, and . _ -)"`
		DisplayName string `json:"display_name,omitempty" maxLength:"200"`
		Email       string `json:"email,omitempty" maxLength:"320" format:"email"`
		Password    string `json:"password,omitempty" minLength:"12" maxLength:"256" doc:"Optional initial password (at least 12 characters, not a common password, not containing the username); the user changes it after signing in"`
	}
}

type updatePrincipalInput struct {
	ID   string `path:"id" doc:"The principal's id (uuid)"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" maxLength:"200" doc:"Display name; empty clears it"`
		Email       *string `json:"email,omitempty" maxLength:"320" doc:"Email; empty clears it"`
		Username    *string `json:"username,omitempty" minLength:"1" maxLength:"200" pattern:"^[a-z0-9][a-z0-9._-]*$" doc:"Sign-in name (lowercase letters, digits, and . _ -); renaming is safe"`
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
		prs, err := gw.ListPrincipals(ctx, a.scopeFor(ctx, "principal", "read"), in.IncludeArchived)
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
			if err := mapPasswordErr(auth.ValidatePassword(in.Body.Password, in.Body.Username)); err != nil {
				return nil, err
			}
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
		ok, err := a.grantCoverOK(ctx, in.Body.Role)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, huma.Error403Forbidden("cannot grant a role whose capabilities exceed yours")
		}
		g, err := gw.CreateGrant(ctx, actorID(ctx), in.ID, storage.GrantSpec{
			Role: in.Body.Role, ScopeKind: in.Body.ScopeKind, ScopeID: in.Body.ScopeID, ScopeOp: in.Body.ScopeOp,
		}, a.scopeFor(ctx, "principal_grant", "create"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		return &grantOutput{Body: toGrantBody(g)}, nil
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

	huma.Register(api, huma.Operation{
		OperationID:   "archive-principal",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:archive",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Archive a principal",
		Description:   "Soft-deletes a principal: it is hidden from the directory, can no longer authenticate, and its rows stay intact, reversibly (restore) until purged. Gated by principal:archive (all-scope). The last active owner cannot be archived.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "archive")},
	}, func(ctx context.Context, in *principalPathInput) (*struct{}, error) {
		if err := gw.ArchivePrincipal(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "principal", "archive")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "restore-principal",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:restore",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Restore a principal",
		Description:   "Reverses an archive: the account is restored to active and can authenticate again. Gated by principal:archive (all-scope).",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "archive")},
	}, func(ctx context.Context, in *principalPathInput) (*struct{}, error) {
		if err := gw.RestorePrincipal(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "principal", "archive")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "purge-principal",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:purge",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Purge a principal",
		Description:   "Hard-deletes an archived principal and its owned rows (profile, credentials, grants, memberships); the audit trail is preserved. Irreversible. Gated by principal:purge (admin-sensitive, all-scope), and the principal must be archived first.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "purge", "admin")},
	}, func(ctx context.Context, in *principalPathInput) (*struct{}, error) {
		if err := gw.PurgePrincipal(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "principal", "purge")); err != nil {
			return nil, mapPrincipalErr(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "reset-principal-password",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:resetPassword",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Reset a principal's password",
		Description:   "Sets a new password for another human principal (an administrator action; the target's current password is not required). Gated by principal:reset-password (all-scope). The new password must meet the password policy; a violation is a 422. Refused on yourself (change your own password from your profile, which verifies your current one), on an owner (owners cannot be reset by anyone), or when it would exceed the caller's own capabilities (the takeover guard, shared with impersonation). The action is audited with the administrator as the actor.",
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "reset-password")},
	}, func(ctx context.Context, in *resetPasswordInput) (*struct{}, error) {
		// Self is refused: you change your own password from your profile (which
		// verifies your current one). The admin reset skips that confirmation, so it is
		// for other accounts only.
		if actorID(ctx) == in.ID {
			return nil, huma.Error422UnprocessableEntity("reset your own password from your profile, which verifies your current password")
		}
		reset := a.scopeFor(ctx, "principal", "reset-password")
		target, err := gw.GetPrincipal(ctx, in.ID, reset)
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		// Takeover guard (shared with impersonation): a password reset lets the admin
		// set a known secret and sign in as the target, so an owner cannot be reset by
		// anyone, and the caller's capabilities must cover the target's.
		switch err := a.checkTakeoverGuard(ctx, target); {
		case errors.Is(err, errOwnerTarget):
			return nil, huma.Error403Forbidden("an owner's password cannot be reset")
		case errors.Is(err, errCapabilityEscalation):
			return nil, huma.Error403Forbidden("cannot reset the password of a principal whose capabilities exceed yours")
		case err != nil:
			return nil, huma.Error500InternalServerError("reset password")
		}
		// Scope guard (like act-as): a reset yields the target's authority resolved from
		// the target, so the caller's ALL-SCOPE grants alone must cover the target; a
		// capability held only at a narrow scope must not become estate-wide via a reset.
		actor, _ := principalFrom(ctx)
		if ok, err := a.allScopeCovers(ctx, actor, target); err != nil {
			return nil, huma.Error500InternalServerError("reset password")
		} else if !ok {
			return nil, huma.Error403Forbidden("resetting this principal requires all-scope authority over its capabilities")
		}
		username := ""
		if target.Human != nil {
			username = target.Human.Username
		}
		if err := mapPasswordErr(auth.ValidatePassword(in.Body.Password, username)); err != nil {
			return nil, err
		}
		hash, err := auth.HashPassword(in.Body.Password)
		if err != nil {
			return nil, huma.Error500InternalServerError("reset password")
		}
		if err := gw.SetPrincipalPassword(ctx, actorID(ctx), in.ID, hash, reset); err != nil {
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
	case errors.Is(err, storage.ErrNotArchived):
		return huma.Error409Conflict("a principal must be archived before it can be purged")
	case errors.Is(err, storage.ErrGrantNotFound):
		return huma.Error404NotFound("grant not found")
	case errors.Is(err, storage.ErrGrantExists):
		return huma.Error409Conflict("that grant already exists")
	case errors.Is(err, storage.ErrUnknownRole):
		return huma.Error422UnprocessableEntity("unknown role")
	case errors.Is(err, storage.ErrBadScope):
		return huma.Error422UnprocessableEntity("a scoped grant needs a scope_id")
	case errors.Is(err, storage.ErrGroupNotFound):
		return huma.Error404NotFound("group not found")
	case errors.Is(err, storage.ErrGroupExists):
		return huma.Error409Conflict("that group name already exists")
	default:
		return huma.Error500InternalServerError("principal operation failed")
	}
}

// grantCoverOK reports whether the caller may grant the given role: the caller's
// all-scope grants must cover the role's capabilities, so no one promotes anyone
// (including itself) to a tier above its own, e.g. admin granting owner. Only the
// caller's all-scope grants count, mirroring the impersonation guard. Shared by
// the direct-grant and group-grant handlers. A non-nil error is already an HTTP
// error.
func (a *authenticator) grantCoverOK(ctx context.Context, role string) (bool, error) {
	actor, ok := principalFrom(ctx)
	if !ok {
		return false, huma.Error401Unauthorized("unauthenticated")
	}
	idx, err := a.roleIndex(ctx)
	if err != nil {
		return false, huma.Error500InternalServerError("grant failed")
	}
	allScopeRoleIDs := make([]string, 0, len(actor.Grants))
	for _, gr := range actor.Grants {
		if gr.ScopeKind == "all" {
			allScopeRoleIDs = append(allScopeRoleIDs, gr.Role)
		}
	}
	return idx.Flatten(allScopeRoleIDs).Covers(idx.Flatten([]string{role})), nil
}

// toGrantBody renders a stored grant as the API body (the scope id is inlined only
// when set). Shared by the direct-grant and group-grant surfaces.
func toGrantBody(g *storage.Grant) grantBody {
	b := grantBody{ID: g.ID, Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
	if g.ScopeID != nil {
		b.ScopeID = *g.ScopeID
	}
	if g.GroupID != nil {
		b.GroupID = *g.GroupID
	}
	if g.GroupName != nil {
		b.GroupName = *g.GroupName
	}
	return b
}
