package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

const defaultImpersonationMinutes = 30

type impersonateInput struct {
	ID   string `path:"id" doc:"The principal to impersonate (uuid)"`
	Body struct {
		Mode            string `json:"mode" enum:"view_as,act_as" doc:"view_as is read-only; act_as is full, with mutations attributed to both the real actor and the impersonated principal"`
		DurationMinutes int    `json:"duration_minutes,omitempty" minimum:"1" maximum:"1440" doc:"Session lifetime in minutes (default 30, max 1440)"`
	}
}

type impersonateOutput struct {
	Body struct {
		Token     string `json:"token" doc:"The bearer token to send while impersonating; shown once"`
		Mode      string `json:"mode"`
		TargetID  string `json:"target_id"`
		ExpiresAt string `json:"expires_at"`
	}
}

// registerImpersonationRoutes wires :impersonate (mint a view-as / act-as session)
// and :stopImpersonation (end it). The capability gate (principal:impersonate,
// all-scope) is the middleware; the escalation guard (the caller must cover the
// target's capabilities) is enforced in the handler.
func registerImpersonationRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID:   "impersonate-principal",
		Method:        http.MethodPost,
		Path:          "/principals/{id}:impersonate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Impersonate a principal (view-as or act-as)",
		Description:   "Mints a bounded, revocable token to view as (read-only) or act as (full) the target. Gated by principal:impersonate (all-scope). Refused on self, when it would grant a capability the caller lacks (the escalation guard), or from within an existing impersonation.",
		Errors:        []int{http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity},
		Middlewares:   huma.Middlewares{a.authn, a.require("principal", "impersonate")},
	}, func(ctx context.Context, in *impersonateInput) (*impersonateOutput, error) {
		actor, ok := principalFrom(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("unauthenticated")
		}
		// No nesting: an impersonation cannot be started from within one.
		if impersonationMode(ctx) != "" {
			return nil, huma.Error403Forbidden("cannot start impersonation while impersonating")
		}
		if actor.ID == in.ID {
			return nil, huma.Error422UnprocessableEntity("cannot impersonate yourself")
		}
		// The target must be readable, which for a principal is all-scope only.
		target, err := gw.GetPrincipal(ctx, in.ID, a.scopeFor(ctx, "principal", "read"))
		if err != nil {
			return nil, mapPrincipalErr(err)
		}
		// Escalation guard: the caller's capabilities must cover the target's, so
		// impersonation can never confer a capability the caller lacks (a lesser
		// admin cannot impersonate an owner). Audit records the real actor regardless.
		actorPerms, _ := permsFrom(ctx)
		idx, err := a.roleIndex(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("impersonation failed")
		}
		targetRoleIDs := make([]string, 0, len(target.Grants))
		for _, g := range target.Grants {
			targetRoleIDs = append(targetRoleIDs, g.Role)
		}
		targetPerms := idx.Flatten(targetRoleIDs)
		if !actorPerms.Covers(targetPerms) {
			return nil, huma.Error403Forbidden("cannot impersonate a principal whose capabilities exceed yours")
		}
		// Scope guard for act-as: a mutation while acting-as resolves its scope from
		// the TARGET, so a caller who is not all-scope for a write capability the
		// target holds would gain write access to a scope it lacks (a scope
		// escalation). Refuse act-as unless the caller is all-scope for every
		// tree-write capability the target has; view-as stays cross-scope (read
		// only, so it grants no write authority). Non-tree resources (principal,
		// role, credential) have no scope kind, so they are already all-or-nothing
		// and covered by the capability guard above.
		if in.Body.Mode == "act_as" {
			for _, resource := range []string{"location", "system", "component"} {
				for _, action := range []string{"create", "update", "delete"} {
					if targetPerms.Allows(resource, action) && !a.scopeFor(ctx, resource, action).All {
						return nil, huma.Error403Forbidden("act-as requires all-scope authority over the target's capabilities; use view-as instead")
					}
				}
			}
		}
		mins := in.Body.DurationMinutes
		if mins == 0 {
			mins = defaultImpersonationMinutes
		}
		token, sess, err := gw.BeginImpersonation(ctx, actor.ID, target.ID, in.Body.Mode, time.Duration(mins)*time.Minute)
		if err != nil {
			if errors.Is(err, storage.ErrCannotImpersonateSelf) {
				return nil, huma.Error422UnprocessableEntity("cannot impersonate yourself")
			}
			return nil, huma.Error500InternalServerError("impersonation failed")
		}
		out := &impersonateOutput{}
		out.Body.Token = token
		out.Body.Mode = sess.Mode
		out.Body.TargetID = sess.TargetID
		out.Body.ExpiresAt = sess.ExpiresAt.Format(time.RFC3339)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "stop-impersonation",
		Method:        http.MethodPost,
		Path:          "/auth/me:stopImpersonation",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Stop the current impersonation session",
		Description:   "Revokes the impersonation session presented by the request token, ending the view-as / act-as. Requires an impersonation token.",
		Errors:        []int{http.StatusForbidden},
		Middlewares:   huma.Middlewares{a.authn},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		sid, _ := ctx.Value(impSessionCtxKey).(string)
		if sid == "" {
			return nil, huma.Error403Forbidden("not an impersonation session")
		}
		if err := gw.EndImpersonation(ctx, sid); err != nil {
			return nil, huma.Error403Forbidden("no active impersonation session")
		}
		return &struct{}{}, nil
	})
}
