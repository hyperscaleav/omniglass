package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/avatar"
	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// userBody is the wire shape of a user in the admin directory: its id
// and kind, the kind profile, and its grants. Credentials are deliberately not
// included, so no secret ever leaves the API.
// userGroupRef names a group a user belongs to, so the console can show
// membership and link through to the group.
type userGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type userBody struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Active     bool           `json:"active"`
	ArchivedAt *time.Time     `json:"archived_at,omitempty" doc:"Set when the user is archived (soft-deleted); absent means live"`
	Person     *personBody    `json:"person,omitempty"`
	Machine    *machineBody   `json:"machine,omitempty"`
	Grants     []grantBody    `json:"grants"`
	Groups     []userGroupRef `json:"groups"`
}

func toUserBody(pr *storage.User) userBody {
	b := userBody{ID: pr.ID, Kind: pr.Kind, Active: pr.Active, ArchivedAt: pr.ArchivedAt, Grants: make([]grantBody, 0, len(pr.Grants)), Groups: make([]userGroupRef, 0, len(pr.Groups))}
	if pr.Person != nil {
		b.Person = &personBody{Username: pr.Person.Username, Email: pr.Person.Email, Label: pr.Person.Label, HasAvatar: pr.Person.HasAvatar}
	}
	if pr.Machine != nil {
		b.Machine = &machineBody{Name: pr.Machine.Name}
	}
	for i := range pr.Grants {
		b.Grants = append(b.Grants, toGrantBody(&pr.Grants[i]))
	}
	for _, gr := range pr.Groups {
		b.Groups = append(b.Groups, userGroupRef{ID: gr.ID, Name: gr.Name})
	}
	return b
}

type listUsersInput struct {
	Kind            string `query:"kind" enum:"person,machine" doc:"Optionally filter by user kind"`
	IncludeArchived bool   `query:"include_archived" doc:"Include archived (soft-deleted) users, hidden by default"`
}

type listUsersOutput struct {
	Body struct {
		Users []userBody `json:"users"`
	}
}

type userPathInput struct {
	ID string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
}

type resetPasswordInput struct {
	ID   string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	Body struct {
		Password string `json:"password" minLength:"12" maxLength:"256" doc:"The new password (at least 12 characters, not a common password, not containing the username)"`
	}
}

type setAvatarInput struct {
	ID   string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	Body struct {
		ImageBase64 string `json:"image_base64" doc:"The image (JPEG, PNG, or WebP), base64-encoded; normalized server-side to a 256x256 JPEG"`
	}
}

type avatarPathInput struct {
	ID string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
}

// avatarOutput carries a user's profile picture as base64. The read side is a
// JSON route (not a raw image/jpeg handler) so it stays under the Huma authz
// middleware; the console renders it as a data URL. Shared by the admin and self
// read handlers.
type avatarOutput struct {
	Body struct {
		ImageBase64 string `json:"image_base64" doc:"The profile picture as a base64-encoded 256x256 JPEG"`
	}
}

type userOutput struct {
	Body userBody
}

type createUserInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"200" pattern:"^[a-z0-9][a-z0-9._-]*$" doc:"Unique sign-in name (lowercase letters, digits, and . _ -)"`
		Label    string `json:"label,omitempty" maxLength:"200" doc:"What an operator reads in lists; falls back to the username"`
		Email    string `json:"email,omitempty" maxLength:"320" format:"email" doc:"Contact email for the account"`
		Password string `json:"password,omitempty" minLength:"12" maxLength:"256" doc:"Optional initial password (at least 12 characters, not a common password, not containing the username); the user changes it after signing in"`
	}
}

type updateUserInput struct {
	ID   string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	Body struct {
		Label    *string `json:"label,omitempty" maxLength:"200" doc:"Label; empty clears it"`
		Email    *string `json:"email,omitempty" maxLength:"320" doc:"Email; empty clears it"`
		Username *string `json:"username,omitempty" minLength:"1" maxLength:"200" pattern:"^[a-z0-9][a-z0-9._-]*$" doc:"Sign-in name (lowercase letters, digits, and . _ -); renaming is safe"`
	}
}

type createGrantInput struct {
	ID   string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	Body struct {
		Role      string `json:"role" minLength:"1" doc:"A role id (viewer, operator, admin, owner, or a custom role)"`
		ScopeKind string `json:"scope_kind" enum:"all,location,system,component" doc:"The scope kind; 'all' confers the whole fleet (group-as-scope is unbuilt and not offered)"`
		ScopeID   string `json:"scope_id,omitempty" doc:"The scope root, the location, system or component the scope kind names, by name, dotted address or uuid (stored as its uuid); omit for the all scope"`
		ScopeOp   string `json:"scope_op,omitempty" enum:"subtree,subtree_excl_root,self" doc:"How the scope root matches the tree: subtree (root + descendants, the default), subtree_excl_root (descendants only for update/delete, root kept for read/create), or self (the root row only). Moot for the all scope."`
	}
}

type grantOutput struct {
	Body grantBody
}

type revokeGrantInput struct {
	ID      string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	GrantID string `path:"grantId" doc:"The grant's id (uuid)"`
}

// listUserSessionsOutput is the body of GET /users/{id}/sessions: the
// target's active bearer credentials in the same shape the self-service list uses.
type listUserSessionsOutput struct {
	Body struct {
		Sessions []sessionBody `json:"sessions"`
	}
}

// revokeUserSessionInput addresses one of a target user's credentials.
type revokeUserSessionInput struct {
	ID  string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	SID string `path:"sid" doc:"The credential id to revoke (from the user's session list)"`
}

// revokeAllUserSessionsInput bulk-revokes one purpose of a target's credentials
// (all sessions, or all tokens), chosen by the body's purpose enum.
type revokeAllUserSessionsInput struct {
	ID   string `path:"id" doc:"The user, addressed by its uuid, a person's username, or a machine user's name"`
	Body struct {
		Purpose string `json:"purpose" enum:"session,token" doc:"Which credentials to revoke: all of the user's web-login sessions, or all its CLI/API tokens"`
	}
}

// revokeAllUserSessionsOutput reports how many credentials the bulk revoke ended.
type revokeAllUserSessionsOutput struct {
	Body struct {
		Revoked int `json:"revoked" doc:"How many credentials were revoked"`
	}
}

// registerUserRoutes wires the admin user directory: list, get, and
// create a human. Each is gated by a user capability, which resolves to an
// all-scope grant only (a user is not a scope-tree entity), so the gateway
// refuses a location or system scope with a 403.
func registerUserRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-users",
		Method:      http.MethodGet,
		Path:        "/users",
		Summary:     "List users",
		Description: "Lists all users (people and machine users) with their grants. Gated by user:read:admin.",
	}, "user", "read", "admin"), func(ctx context.Context, in *listUsersInput) (*listUsersOutput, error) {
		prs, err := gw.ListUsers(ctx, a.scopeFor(ctx, "user", "read"), in.IncludeArchived)
		if err != nil {
			return nil, mapUserErr(err)
		}
		out := &listUsersOutput{}
		out.Body.Users = make([]userBody, 0, len(prs))
		for i := range prs {
			if in.Kind != "" && prs[i].Kind != in.Kind {
				continue
			}
			out.Body.Users = append(out.Body.Users, toUserBody(&prs[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-user",
		Method:      http.MethodGet,
		Path:        "/users/{id}",
		Summary:     "Get a user",
		Description: "Fetches one user by id with its profile and grants. Gated by user:read:admin.",
	}, "user", "read", "admin"), func(ctx context.Context, in *userPathInput) (*userOutput, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		pr, err := gw.GetUser(ctx, in.ID, a.scopeFor(ctx, "user", "read"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &userOutput{Body: toUserBody(pr)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-user",
		Extensions:    plaintext(),
		Method:        http.MethodPost,
		Path:          "/users",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a person",
		Description:   "Creates a person with an optional initial password. Gated by user:create (all-scope). The new user holds no grants; assign roles separately.",
	}, "user", "create"), func(ctx context.Context, in *createUserInput) (*userOutput, error) {
		spec := storage.PersonSpec{
			Username: in.Body.Username,
			Email:    in.Body.Email,
			Label:    in.Body.Label,
		}
		if in.Body.Password != "" {
			if err := mapPasswordErr(auth.ValidatePassword(in.Body.Password, in.Body.Username)); err != nil {
				return nil, err
			}
			hash, err := auth.HashPassword(in.Body.Password)
			if err != nil {
				return nil, huma.Error500InternalServerError("create user")
			}
			spec.PasswordHash = hash
		}
		pr, err := gw.CreatePerson(ctx, actorID(ctx), spec, a.scopeFor(ctx, "user", "create"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &userOutput{Body: toUserBody(pr)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-user",
		Method:      http.MethodPatch,
		Path:        "/users/{id}",
		Summary:     "Update a user",
		Description: "Updates a person's label, email, and username. Gated by user:update (all-scope). A username is not an entity name and is patchable here rather than through a :rename method: it is the sign-in identifier, on its own rule, and nothing keys on it.",
	}, "user", "update"), func(ctx context.Context, in *updateUserInput) (*userOutput, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		pr, err := gw.UpdatePerson(ctx, actorID(ctx), in.ID, storage.AdminPersonPatch{
			Label:    in.Body.Label,
			Email:    in.Body.Email,
			Username: in.Body.Username,
		}, a.scopeFor(ctx, "user", "update"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &userOutput{Body: toUserBody(pr)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-grant",
		Method:        http.MethodPost,
		Path:          "/users/{id}/grants",
		DefaultStatus: http.StatusCreated,
		Summary:       "Grant a role to a user",
		Description:   "Assigns a role at a scope to a user. Gated by user_grant:create (all-scope). Refused (403) when the granted role's capabilities exceed the granter's own (no promoting anyone, including yourself, to a higher tier such as owner). A duplicate is 409, an unknown role or bad scope 422.",
	}, "user_grant", "create"), func(ctx context.Context, in *createGrantInput) (*grantOutput, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		ok, err := a.grantCoverOK(ctx, in.Body.Role)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, huma.Error403Forbidden("cannot grant a role whose capabilities exceed yours")
		}
		g, err := gw.CreateGrant(ctx, actorID(ctx), in.ID, storage.GrantSpec{
			Role: in.Body.Role, ScopeKind: in.Body.ScopeKind, ScopeID: in.Body.ScopeID, ScopeOp: in.Body.ScopeOp,
		}, a.scopeFor(ctx, "user_grant", "create"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		return &grantOutput{Body: toGrantBody(g)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "revoke-grant",
		Method:        http.MethodDelete,
		Path:          "/users/{id}/grants/{grantId}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Revoke a grant",
		Description:   "Removes one grant from a user. Gated by user_grant:delete (all-scope). The last owner grant cannot be revoked.",
	}, "user_grant", "delete"), func(ctx context.Context, in *revokeGrantInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		if err := gw.RevokeGrant(ctx, actorID(ctx), in.ID, in.GrantID, a.scopeFor(ctx, "user_grant", "delete")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "disable-user",
		Method:        http.MethodPost,
		Path:          "/users/{id}:disable",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Disable a user",
		Description:   "Soft-disables a user so it can no longer authenticate; its audit trail is kept. Gated by user:update (all-scope). The last active owner cannot be disabled.",
	}, "user", "update"), func(ctx context.Context, in *userPathInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		if err := gw.SetUserActive(ctx, actorID(ctx), in.ID, false, a.scopeFor(ctx, "user", "update")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "enable-user",
		Method:        http.MethodPost,
		Path:          "/users/{id}:enable",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Enable a user",
		Description:   "Re-enables a disabled user, restoring its ability to authenticate. Gated by user:update (all-scope).",
	}, "user", "update"), func(ctx context.Context, in *userPathInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		if err := gw.SetUserActive(ctx, actorID(ctx), in.ID, true, a.scopeFor(ctx, "user", "update")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "archive-user",
		Method:        http.MethodPost,
		Path:          "/users/{id}:archive",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Archive a user",
		Description:   "Soft-deletes a user: it is hidden from the directory, can no longer authenticate, and its rows stay intact, reversibly (restore) until purged. Gated by user:archive (all-scope). The last active owner cannot be archived.",
	}, "user", "archive"), func(ctx context.Context, in *userPathInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		if err := gw.ArchiveUser(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "user", "archive")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "restore-user",
		Method:        http.MethodPost,
		Path:          "/users/{id}:restore",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Restore a user",
		Description:   "Reverses an archive: the account is restored to active and can authenticate again. Gated by user:archive (all-scope).",
	}, "user", "archive"), func(ctx context.Context, in *userPathInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		if err := gw.RestoreUser(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "user", "archive")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "purge-user",
		Method:        http.MethodPost,
		Path:          "/users/{id}:purge",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Purge a user",
		Description:   "Hard-deletes an archived user and its owned rows (profile, credentials, grants, memberships); the audit trail is preserved. Irreversible. A person a notification target notifies cannot be purged (a 409; the refusal names the target to a caller who reads targets). Gated by user:purge:admin (admin-sensitive, all-scope), and the user must be archived first.",
	}, "user", "purge", "admin"), func(ctx context.Context, in *userPathInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		if err := gw.PurgeUser(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "user", "purge")); err != nil {
			return nil, mapBoundErr(ctx, a, err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "reset-user-password",
		Extensions:    plaintext(),
		Method:        http.MethodPost,
		Path:          "/users/{id}:resetPassword",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Reset a user's password",
		Description:   "Sets a new password for another person (an administrator action; the target's current password is not required). Gated by user:reset-password (all-scope). The new password must meet the password policy; a violation is a 422. Refused on yourself (change your own password from your profile, which verifies your current one), on an owner (owners cannot be reset by anyone), or when it would exceed the caller's own capabilities (the takeover guard, shared with impersonation). The action is audited with the administrator as the actor.",
	}, "user", "reset-password"), func(ctx context.Context, in *resetPasswordInput) (*struct{}, error) {
		// Resolve the target first so the self-check below catches addressing yourself
		// by username as well as by uuid.
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		// Self is refused: you change your own password from your profile (which
		// verifies your current one). The admin reset skips that confirmation, so it is
		// for other accounts only.
		if actorID(ctx) == in.ID {
			return nil, huma.Error422UnprocessableEntity("reset your own password from your profile, which verifies your current password")
		}
		reset := a.scopeFor(ctx, "user", "reset-password")
		target, err := gw.GetUser(ctx, in.ID, reset)
		if err != nil {
			return nil, mapUserErr(err)
		}
		// Takeover guard (shared with impersonation): a password reset lets the admin
		// set a known secret and sign in as the target, so an owner cannot be reset by
		// anyone, and the caller's capabilities must cover the target's.
		switch err := a.checkTakeoverGuard(ctx, target); {
		case errors.Is(err, errOwnerTarget):
			return nil, huma.Error403Forbidden("an owner's password cannot be reset")
		case errors.Is(err, errCapabilityEscalation):
			return nil, huma.Error403Forbidden("cannot reset the password of a user whose capabilities exceed yours")
		case err != nil:
			return nil, huma.Error500InternalServerError("reset password")
		}
		// Scope guard (like act-as): a reset yields the target's authority resolved from
		// the target, so the caller's ALL-SCOPE grants alone must cover the target; a
		// capability held only at a narrow scope must not become fleet-wide via a reset.
		actor, _ := userFrom(ctx)
		if ok, err := a.allScopeCovers(ctx, actor, target); err != nil {
			return nil, huma.Error500InternalServerError("reset password")
		} else if !ok {
			return nil, huma.Error403Forbidden("resetting this user requires all-scope authority over its capabilities")
		}
		username := ""
		if target.Person != nil {
			username = target.Person.Username
		}
		if err := mapPasswordErr(auth.ValidatePassword(in.Body.Password, username)); err != nil {
			return nil, err
		}
		hash, err := auth.HashPassword(in.Body.Password)
		if err != nil {
			return nil, huma.Error500InternalServerError("reset password")
		}
		if err := gw.SetUserPassword(ctx, actorID(ctx), in.ID, hash, reset); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "set-user-avatar",
		Method:        http.MethodPost,
		Path:          "/users/{id}:setAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Set a user's profile picture",
		Description:   "Sets another person's profile picture (an administrator action). Gated by user:set-avatar (all-scope). The image (JPEG, PNG, or WebP, base64-encoded) is normalized server-side to a 256x256 JPEG; a bad or oversize image is a 422. Audited with the administrator as the actor.",
	}, "user", "set-avatar"), func(ctx context.Context, in *setAvatarInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		b64, aerr := normalizeAvatar(in.Body.ImageBase64)
		if aerr != nil {
			return nil, aerr
		}
		if err := gw.SetUserAvatar(ctx, actorID(ctx), id, b64, a.scopeFor(ctx, "user", "set-avatar")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "remove-user-avatar",
		Method:        http.MethodPost,
		Path:          "/users/{id}:removeAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove a user's profile picture",
		Description:   "Clears another person's profile picture. Gated by user:set-avatar (all-scope). Removing an absent picture is a no-op. Audited with the administrator as the actor.",
	}, "user", "set-avatar"), func(ctx context.Context, in *avatarPathInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		if err := gw.ClearUserAvatar(ctx, actorID(ctx), id, a.scopeFor(ctx, "user", "set-avatar")); err != nil {
			return nil, mapUserErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-user-avatar",
		Method:      http.MethodGet,
		Path:        "/users/{id}/avatar",
		Summary:     "Get a user's profile picture",
		Description: "Returns the user's profile picture as a base64-encoded JPEG. Gated by user:read:admin. A user without a picture is a 404.",
	}, "user", "read", "admin"), func(ctx context.Context, in *avatarPathInput) (*avatarOutput, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		b64, ok, err := gw.GetUserAvatar(ctx, id, a.scopeFor(ctx, "user", "read"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no profile picture")
		}
		out := &avatarOutput{}
		out.Body.ImageBase64 = b64
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-user-sessions",
		Method:      http.MethodGet,
		Path:        "/users/{id}/sessions",
		Summary:     "List a user's sessions",
		Description: "Lists another user's active bearer credentials (login sessions and API tokens) with their non-secret metadata, newest first, so an administrator can see where an account is signed in and revoke a session that should not be. Gated by user:revoke-session (all-scope). The token secret is never returned, and current is always false (there is no \"this request's own session\" when viewing another user).",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, "user", "revoke-session"), func(ctx context.Context, in *userPathInput) (*listUserSessionsOutput, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		// Verify the target exists and is readable at all-scope (a user is not a
		// scope-tree entity): an unknown id is a 404, a non-all scope a 403.
		if _, err := gw.GetUser(ctx, in.ID, a.scopeFor(ctx, "user", "revoke-session")); err != nil {
			return nil, mapUserErr(err)
		}
		// A nil currentHash means no row is ever flagged current: there is no "this
		// request's own session" when listing someone else's credentials.
		creds, err := gw.ListBearerCredentials(ctx, in.ID, nil)
		if err != nil {
			return nil, huma.Error500InternalServerError("list sessions")
		}
		out := &listUserSessionsOutput{}
		out.Body.Sessions = toSessionBodies(creds)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "revoke-user-session",
		Method:        http.MethodPost,
		Path:          "/users/{id}/sessions/{sid}:revoke",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Revoke a user's session",
		Description:   "Revokes one of another user's sessions or tokens by id (an administrator action; the target is immediately signed out of that credential). Gated by user:revoke-session (all-scope). Bounded to the target, so a credential id that is not theirs is a non-disclosing 404, never a cross-user revoke. Refused (403) on an owner (an owner's sessions cannot be revoked by anyone, the takeover guard shared with impersonation and password reset) or when it would exceed the caller's own capabilities. Audited with the administrator as the actor.",
		Errors:        []int{http.StatusForbidden, http.StatusNotFound},
	}, "user", "revoke-session"), func(ctx context.Context, in *revokeUserSessionInput) (*struct{}, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		// Fetch the target first (unknown id 404, non-all scope 403), then apply the
		// takeover guard: an owner's sessions are un-revocable by anyone (including
		// another owner), and the caller's capabilities must cover the target's, so a
		// lesser admin cannot sign an owner out. Shared with the password reset.
		target, err := gw.GetUser(ctx, in.ID, a.scopeFor(ctx, "user", "revoke-session"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		switch err := a.checkTakeoverGuard(ctx, target); {
		case errors.Is(err, errOwnerTarget):
			return nil, huma.Error403Forbidden("an owner's sessions cannot be revoked")
		case errors.Is(err, errCapabilityEscalation):
			return nil, huma.Error403Forbidden("cannot revoke the sessions of a user whose capabilities exceed yours")
		case err != nil:
			return nil, huma.Error500InternalServerError("revoke session")
		}
		// The delete is bounded to the target user in SQL, so a sid that is not
		// the target's matches nothing and is a non-disclosing 404 (never a 500, never
		// a cross-user revoke).
		revoked, err := gw.RevokeBearerByID(ctx, actorID(ctx), in.ID, in.SID)
		if err != nil {
			return nil, huma.Error500InternalServerError("revoke session")
		}
		if !revoked {
			return nil, huma.Error404NotFound("no such session")
		}
		// Audit the administrator action: the acting admin is the actor, the target
		// user is recorded, and the real actor rides context when impersonating.
		if err := gw.WriteAuthEventFor(ctx, actorID(ctx), "revoke_session", "user", in.ID); err != nil {
			return nil, huma.Error500InternalServerError("revoke session")
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "revoke-all-user-sessions",
		Method:      http.MethodPost,
		Path:        "/users/{id}/sessions:revokeAll",
		Summary:     "Revoke all of a user's sessions or tokens",
		Description: "Revokes every one of another user's web-login sessions, or every one of its CLI/API tokens (chosen by purpose), in a single administrator action, returning how many were ended. Gated by user:revoke-session (all-scope). Bounded to the target and never crosses purpose (revoking sessions leaves tokens, and vice versa). Refused (403) on an owner (the takeover guard shared with impersonation and the password reset) or when it would exceed the caller's own capabilities. Audited with the administrator as the actor.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, "user", "revoke-session"), func(ctx context.Context, in *revokeAllUserSessionsInput) (*revokeAllUserSessionsOutput, error) {
		id, rerr := a.resolveUserRef(ctx, in.ID)
		if rerr != nil {
			return nil, rerr
		}
		in.ID = id
		// Same guards as the single revoke: fetch the target (unknown id 404, non-all
		// scope 403), then the takeover guard (an owner's credentials are un-revocable by
		// a lesser admin, and the caller's capabilities must cover the target's).
		target, err := gw.GetUser(ctx, in.ID, a.scopeFor(ctx, "user", "revoke-session"))
		if err != nil {
			return nil, mapUserErr(err)
		}
		switch err := a.checkTakeoverGuard(ctx, target); {
		case errors.Is(err, errOwnerTarget):
			return nil, huma.Error403Forbidden("an owner's sessions cannot be revoked")
		case errors.Is(err, errCapabilityEscalation):
			return nil, huma.Error403Forbidden("cannot revoke the sessions of a user whose capabilities exceed yours")
		case err != nil:
			return nil, huma.Error500InternalServerError("revoke sessions")
		}
		n, err := gw.RevokeBearersByPurpose(ctx, actorID(ctx), in.ID, in.Body.Purpose)
		if err != nil {
			return nil, huma.Error500InternalServerError("revoke sessions")
		}
		// Audit only a revoke that actually ended something (a zero-count bulk revoke is
		// a no-op), attributed to the acting admin with the target user.
		if n > 0 {
			if err := gw.WriteAuthEventFor(ctx, actorID(ctx), "revoke_session", "user", in.ID); err != nil {
				return nil, huma.Error500InternalServerError("revoke sessions")
			}
		}
		out := &revokeAllUserSessionsOutput{}
		out.Body.Revoked = n
		return out, nil
	})
}

// normalizeAvatar decodes the base64 upload, normalizes the image, and returns it
// re-encoded as base64 for storage. Bad base64 or a bad/oversize image is a 422;
// anything else is a 500. Shared by the admin and self avatar write handlers.
func normalizeAvatar(imageB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(imageB64)
	if err != nil {
		return "", huma.Error422UnprocessableEntity("image_base64 is not valid base64")
	}
	jpeg, err := avatar.Normalize(raw)
	if err != nil {
		if errors.Is(err, avatar.ErrTooLarge) || errors.Is(err, avatar.ErrUnsupported) {
			return "", huma.Error422UnprocessableEntity(err.Error())
		}
		return "", huma.Error500InternalServerError("normalize avatar")
	}
	return base64.StdEncoding.EncodeToString(jpeg), nil
}

// mapUserErr translates the gateway's user sentinels into HTTP status:
// an unknown id 404, a non-all scope 403, a duplicate username 409.
// resolveUserRef maps a {id} path value (a uuid or a person's username) to the
// user id, so every user route accepts either. A uuid passes through; an
// unknown username is a 404, the same as an unknown id. Handlers call it at the top
// and use the returned id (including before any self-check, so addressing yourself by
// username is caught).
//
// A NAME resolves only for a caller who may list the whole user directory
// (user:read:admin on an all-scope grant, the list route's own gate and
// scope), since that caller learns nothing from a name it could not already
// read. Every user route's gateway refuses a non-all caller anyway; doing
// it here, before the lookup, is what keeps that caller from telling a name
// that exists from one that does not (#1133's security review): it gets the
// same all-scope 403 for every name. A uuid passes straight through, and the
// gateway decides it exactly as before.
func (a *authenticator) resolveUserRef(ctx context.Context, ref string) (string, error) {
	if !storage.IsUUID(ref) && !a.canListUsers(ctx) {
		return "", mapUserErr(storage.ErrUserForbidden)
	}
	id, err := a.gw.ResolveUserRef(ctx, ref)
	if refErr, ok := mapRefErr(err); ok {
		return "", refErr
	}
	switch {
	case errors.Is(err, storage.ErrUserNotFound):
		return "", huma.Error404NotFound("user not found")
	case err != nil:
		return "", huma.Error500InternalServerError("resolve user")
	}
	return id, nil
}

// canListUsers reports whether the caller may list every user: the
// user:read:admin permission, held on an all-scope grant.
func (a *authenticator) canListUsers(ctx context.Context) bool {
	return a.canReadDirectory(ctx, "user")
}

// canReadDirectory reports whether the caller reads an IAM directory
// (user, user_group) the way its own list route requires: the
// admin-tier read permission, held on an all-scope grant. The permission
// alone is scope-blind (the flattened set ignores where a grant is scoped),
// so a location-scoped admin holds it without reading the directory, and any
// check that stands in for the directory's read takes both halves (#1341).
func (a *authenticator) canReadDirectory(ctx context.Context, resource string) bool {
	return a.allows(ctx, resource, "read", "admin") && a.scopeFor(ctx, resource, "read").All
}

// mapBoundErr maps a delete refused because a notification target binds the
// group or person (#1341): the refusal names the target to a caller who reads
// targets (notification_target:read, all-scope), the generic refusal to
// anyone else, since a target name is not theirs to learn.
func mapBoundErr(ctx context.Context, a *authenticator, err error) error {
	var bound *storage.TargetBindingError
	if errors.As(err, &bound) && a.allows(ctx, "notification_target", "read") && a.scopeFor(ctx, "notification_target", "read").All {
		return huma.Error409Conflict("the notification target " + bound.Target + " notifies this " + bound.What + "; delete that target first")
	}
	return mapUserErr(err)
}

func mapUserErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrUserNotFound):
		return huma.Error404NotFound("user not found")
	case errors.Is(err, storage.ErrUserForbidden):
		return huma.Error403Forbidden("user management requires an all-scope grant")
	case errors.Is(err, storage.ErrUsernameTaken):
		return huma.Error409Conflict("username already exists")
	case errors.Is(err, storage.ErrUserNotPerson):
		return huma.Error422UnprocessableEntity("only people have these fields")
	case errors.Is(err, storage.ErrLastOwner):
		return huma.Error409Conflict("cannot revoke the last owner grant")
	case errors.Is(err, storage.ErrNotArchived):
		return huma.Error409Conflict("a user must be archived before it can be purged")
	case errors.Is(err, storage.ErrBoundToTarget):
		return huma.Error409Conflict("a notification target notifies it; delete that target first")
	case errors.Is(err, storage.ErrGrantNotFound):
		return huma.Error404NotFound("grant not found")
	case errors.Is(err, storage.ErrGrantExists):
		return huma.Error409Conflict("that grant already exists")
	case errors.Is(err, storage.ErrUnknownRole):
		return huma.Error422UnprocessableEntity("unknown role")
	case errors.Is(err, storage.ErrBadScope):
		return huma.Error422UnprocessableEntity("a scoped grant needs a scope_id naming an existing location, system or component of its scope_kind")
	case errors.Is(err, storage.ErrGroupNotFound):
		return huma.Error404NotFound("group not found")
	case errors.Is(err, storage.ErrGroupExists):
		return huma.Error409Conflict("that group name already exists")
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("group name may not be a uuid")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("group name must be lowercase letters, digits, and hyphens, starting with a letter or digit")
	default:
		return huma.Error500InternalServerError("user operation failed")
	}
}

// grantCoverOK reports whether the caller may grant the given role: the caller's
// all-scope grants must cover the role's capabilities, so no one promotes anyone
// (including itself) to a tier above its own, e.g. admin granting owner. Only the
// caller's all-scope grants count, mirroring the impersonation guard. Shared by
// the direct-grant and group-grant handlers. A non-nil error is already an HTTP
// error.
func (a *authenticator) grantCoverOK(ctx context.Context, role string) (bool, error) {
	actor, ok := userFrom(ctx)
	if !ok {
		return false, huma.Error401Unauthorized("unauthenticated")
	}
	idx, err := a.roleIndex(ctx)
	if err != nil {
		return false, huma.Error500InternalServerError("grant failed")
	}
	return allScopePerms(ctx, idx, actor).Covers(idx.Flatten([]string{role})), nil
}

// allScopePerms flattens the caller's ALL-SCOPE grants into the permission set
// every escalation guard measures against. A request bounded by an agent token
// or the agent ceiling (#1179) is measured by its bounded set, so an agent can
// never hand a user it controls a capability it is itself denied (a
// credential reveal above all) and so escape its ceiling.
func allScopePerms(ctx context.Context, idx rbac.RoleIndex, actor *storage.User) rbac.Set {
	ids := make([]string, 0, len(actor.Grants))
	for _, gr := range actor.Grants {
		if gr.ScopeKind == "all" {
			ids = append(ids, gr.Role)
		}
	}
	perms := idx.Flatten(ids)
	if constrained(ctx) {
		perms = constrainPerms(perms, idx, actor.Agent, true)
	}
	return perms
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
		b.Group = *g.GroupName
	}
	if g.GroupLabel != nil {
		b.GroupLabel = *g.GroupLabel
	}
	return b
}
