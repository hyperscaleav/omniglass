package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// ctxKey scopes the request-context values the authn middleware attaches.
type ctxKey int

const (
	principalCtxKey ctxKey = iota
	permsCtxKey
)

func principalFrom(ctx context.Context) (*storage.Principal, bool) {
	pr, ok := ctx.Value(principalCtxKey).(*storage.Principal)
	return pr, ok
}

func permsFrom(ctx context.Context) (rbac.Set, bool) {
	s, ok := ctx.Value(permsCtxKey).(rbac.Set)
	return s, ok
}

// authenticator resolves bearer tokens to principals and lazily caches the role
// index. The index is loaded once; CDC-driven invalidation is a later slice
// (this slice's roles change only at boot).
type authenticator struct {
	gw  storage.Gateway
	api huma.API
	// secureCookies marks the session cookie Secure (https only): off for local
	// http dev, on behind TLS. Set from config.
	secureCookies bool

	once   sync.Once
	index  rbac.RoleIndex
	idxErr error
}

func (a *authenticator) roleIndex(ctx context.Context) (rbac.RoleIndex, error) {
	a.once.Do(func() {
		roles, err := a.gw.ListRoles(ctx)
		if err != nil {
			a.idxErr = err
			return
		}
		rr := make([]rbac.Role, 0, len(roles))
		for _, r := range roles {
			rr = append(rr, rbac.Role{ID: r.ID, Permissions: r.Permissions, Inherits: r.Inherits})
		}
		a.index = rbac.NewRoleIndex(rr)
	})
	return a.index, a.idxErr
}

// authn is Huma operation middleware: resolve the bearer token, attach the
// principal and its flattened permission set to the context, or 401. It is the
// capability fast-reject's prerequisite, not the authorization itself.
func (a *authenticator) authn(ctx huma.Context, next func(huma.Context)) {
	// A human session arrives as the httpOnly cookie; a service or the CLI sends an
	// Authorization bearer. Try each candidate token: a stale or invalid bearer
	// must not shadow a valid session cookie (the cookie is the fallback), so 401
	// only when no candidate resolves.
	var candidates []string
	if t, ok := bearerToken(ctx.Header("Authorization")); ok {
		candidates = append(candidates, t)
	}
	if t, ok := sessionCookieToken(ctx.Header("Cookie")); ok {
		candidates = append(candidates, t)
	}
	if len(candidates) == 0 {
		_ = huma.WriteErr(a.api, ctx, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var pr *storage.Principal
	for _, tok := range candidates {
		p, err := a.gw.AuthenticateBearer(ctx.Context(), auth.HashToken(tok))
		switch {
		case err == nil:
			pr = p
		case errors.Is(err, storage.ErrCredentialNotFound):
			continue
		default:
			_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "authentication failed")
			return
		}
		if pr != nil {
			break
		}
	}
	if pr == nil {
		_ = huma.WriteErr(a.api, ctx, http.StatusUnauthorized, "unauthenticated")
		return
	}
	idx, err := a.roleIndex(ctx.Context())
	if err != nil {
		_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "authentication failed")
		return
	}
	roleIDs := make([]string, 0, len(pr.Grants))
	for _, g := range pr.Grants {
		roleIDs = append(roleIDs, g.Role)
	}
	c := huma.WithValue(ctx, principalCtxKey, pr)
	c = huma.WithValue(c, permsCtxKey, idx.Flatten(roleIDs))
	next(c)
}

// require is Huma operation middleware enforcing a capability. It runs after
// authn; a principal whose flattened permissions do not allow the action is 403.
// Scope (which entities) is the gateway's job and lands when entities exist.
func (a *authenticator) require(resource, action string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		perms, ok := permsFrom(ctx.Context())
		if !ok || !perms.Allows(resource, action) {
			_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, "forbidden")
			return
		}
		next(ctx)
	}
}

// scopeFor resolves visible_set(P, action) for a resource from the request
// principal's grants and the cached role index. It is the API's half of the ABAC
// model: the gateway requires a resolved scope on every query, and this is where
// the handler composes it (cheap: cached grants plus the role map). An
// unauthenticated context or an index failure resolves to the empty scope, which
// admits nothing.
func (a *authenticator) scopeFor(ctx context.Context, resource, action string) scope.Set {
	pr, ok := principalFrom(ctx)
	if !ok {
		return scope.Set{}
	}
	idx, err := a.roleIndex(ctx)
	if err != nil {
		return scope.Set{}
	}
	grants := make([]scope.Grant, 0, len(pr.Grants))
	for _, g := range pr.Grants {
		sg := scope.Grant{Role: g.Role, ScopeKind: g.ScopeKind}
		if g.ScopeID != nil {
			sg.ScopeID = *g.ScopeID
		}
		grants = append(grants, sg)
	}
	return scope.Resolve(grants, idx, resource, action)
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		if tok := strings.TrimSpace(header[len(prefix):]); tok != "" {
			return tok, true
		}
	}
	return "", false
}

// sessionCookieName is the httpOnly cookie carrying a human's session bearer
// token; the SPA never reads it (it sends with credentials: 'include').
const sessionCookieName = "og_session"

// sessionCookieToken extracts the session token from a raw Cookie header.
func sessionCookieToken(cookieHeader string) (string, bool) {
	if cookieHeader == "" {
		return "", false
	}
	r := http.Request{Header: http.Header{"Cookie": {cookieHeader}}}
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func (a *authenticator) sessionCookie(token string) http.Cookie {
	return http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode,
	}
}

func (a *authenticator) clearedCookie() http.Cookie {
	return http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	}
}

// authStatusOutput is the body of GET /api/v1/auth/status: whether the system has
// an owner yet. Public, so the login screen can hide the bootstrap hint.
type authStatusOutput struct {
	Body struct {
		Bootstrapped bool `json:"bootstrapped"`
	}
}

func (a *authenticator) statusHandler(ctx context.Context, _ *struct{}) (*authStatusOutput, error) {
	exists, err := a.gw.AnyHuman(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("status")
	}
	out := &authStatusOutput{}
	out.Body.Bootstrapped = exists
	return out, nil
}

// loginInput is the body of POST /api/v1/auth/login.
type loginInput struct {
	Body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
}

// sessionOutput sets (or clears) the session cookie. No body: the SPA reads the
// principal from /auth/me after a successful login.
type sessionOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

// loginHandler verifies a username and password, mints a session bearer token,
// and returns it as the httpOnly session cookie. A bad credential is a flat 401
// that does not say which of user / password was wrong.
func (a *authenticator) loginHandler(ctx context.Context, in *loginInput) (*sessionOutput, error) {
	pr, err := a.gw.AuthenticatePassword(ctx, in.Body.Username, in.Body.Password)
	switch {
	case errors.Is(err, storage.ErrBadCredentials):
		return nil, huma.Error401Unauthorized("invalid username or password")
	case errors.Is(err, storage.ErrAccountDisabled):
		// The password was correct but the account is disabled. A distinct 403 (not
		// the generic 401) so the sign-in screen can explain it; only reachable with
		// the right password, so it discloses nothing to an attacker without it.
		return nil, huma.Error403Forbidden("account disabled")
	case err != nil:
		return nil, huma.Error500InternalServerError("login failed")
	}
	token, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		return nil, huma.Error500InternalServerError("login failed")
	}
	if _, err := a.gw.IssueBearerCredential(ctx, pr.Human.Username, hash, prefix); err != nil {
		return nil, huma.Error500InternalServerError("login failed")
	}
	return &sessionOutput{SetCookie: a.sessionCookie(token)}, nil
}

// logoutInput reads the session cookie so the token can be revoked.
type logoutInput struct {
	Cookie string `header:"Cookie"`
}

// logoutHandler revokes the session token (if a valid one is presented) and
// clears the cookie. Public: clearing the cookie always succeeds, even for an
// already-invalid session.
func (a *authenticator) logoutHandler(ctx context.Context, in *logoutInput) (*sessionOutput, error) {
	if tok, ok := sessionCookieToken(in.Cookie); ok {
		_ = a.gw.RevokeBearer(ctx, auth.HashToken(tok))
	}
	return &sessionOutput{SetCookie: a.clearedCookie()}, nil
}

// meOutput is the body of GET /api/v1/auth/me: the resolved principal, its
// flattened permissions (a UI hint and the fast-reject set), and its grants.
type meOutput struct {
	Body struct {
		Principal struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"principal"`
		Human       *humanBody  `json:"human,omitempty"`
		Service     *svcBody    `json:"service,omitempty"`
		Permissions []string    `json:"permissions"`
		Grants      []grantBody `json:"grants"`
	}
}

type humanBody struct {
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type svcBody struct {
	Label string `json:"label"`
}

type grantBody struct {
	ID        string `json:"id,omitempty"`
	Role      string `json:"role"`
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id,omitempty"`
}

func meHandler(ctx context.Context, _ *struct{}) (*meOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	perms, _ := permsFrom(ctx)

	out := &meOutput{}
	out.Body.Principal.ID = pr.ID
	out.Body.Principal.Kind = pr.Kind
	if pr.Human != nil {
		out.Body.Human = &humanBody{Username: pr.Human.Username, Email: pr.Human.Email, DisplayName: pr.Human.DisplayName}
	}
	if pr.Service != nil {
		out.Body.Service = &svcBody{Label: pr.Service.Label}
	}
	out.Body.Permissions = perms.Strings()
	out.Body.Grants = make([]grantBody, 0, len(pr.Grants))
	for _, g := range pr.Grants {
		gb := grantBody{ID: g.ID, Role: g.Role, ScopeKind: g.ScopeKind}
		if g.ScopeID != nil {
			gb.ScopeID = *g.ScopeID
		}
		out.Body.Grants = append(out.Body.Grants, gb)
	}
	return out, nil
}

// updateMeInput is the body of PATCH /api/v1/auth/me. Only the display name is
// self-editable; email is set by an administrator (a later slice), so it is not
// on the self-service patch. The field is optional (a pointer): absent leaves it
// unchanged, a provided empty string clears it.
type updateMeInput struct {
	Body struct {
		DisplayName *string `json:"display_name,omitempty" maxLength:"200" doc:"Your display name; empty clears it"`
	}
}

// profileOutput is the updated human profile returned by PATCH /auth/me.
type profileOutput struct {
	Body humanBody
}

// updateMeHandler updates the caller's own profile. Self-scoped: it edits the
// principal resolved from the session, never another. Authentication is the only
// gate (in the ungated allow-list, like GET /auth/me). Email is deliberately not
// self-editable here; only the display name moves.
func (a *authenticator) updateMeHandler(ctx context.Context, in *updateMeInput) (*profileOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok || pr.Human == nil {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	patch := storage.HumanProfilePatch{DisplayName: in.Body.DisplayName}
	if err := a.gw.UpdateHumanProfile(ctx, pr.ID, patch); err != nil {
		return nil, huma.Error500InternalServerError("update profile")
	}
	// Return the merged result: exactly what was written (the session profile plus
	// the applied field), so the client need not re-read.
	h := *pr.Human
	if in.Body.DisplayName != nil {
		h.DisplayName = *in.Body.DisplayName
	}
	return &profileOutput{Body: humanBody{Username: h.Username, Email: h.Email, DisplayName: h.DisplayName}}, nil
}

// changePasswordInput is the body of POST /api/v1/auth/me:changePassword. The new
// password has a minimum length; the current password is verified in the handler.
type changePasswordInput struct {
	Body struct {
		CurrentPassword string `json:"current_password" doc:"Your current password"`
		NewPassword     string `json:"new_password" minLength:"8" maxLength:"256" doc:"The new password (at least 8 characters)"`
	}
}

// changePasswordHandler verifies the caller's current password and sets a new one.
// Self-scoped and authn-only. A wrong current password is a 403 (the request is
// authenticated, but not permitted to rotate without the current secret). Existing
// sessions are intentionally left valid for this slice; revoke-on-change is a later
// hardening.
func (a *authenticator) changePasswordHandler(ctx context.Context, in *changePasswordInput) (*struct{}, error) {
	pr, ok := principalFrom(ctx)
	if !ok || pr.Human == nil {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	if _, err := a.gw.AuthenticatePassword(ctx, pr.Human.Username, in.Body.CurrentPassword); err != nil {
		if errors.Is(err, storage.ErrBadCredentials) {
			return nil, huma.Error403Forbidden("current password is incorrect")
		}
		return nil, huma.Error500InternalServerError("change password")
	}
	encoded, err := auth.HashPassword(in.Body.NewPassword)
	if err != nil {
		return nil, huma.Error500InternalServerError("change password")
	}
	if _, err := a.gw.SetPassword(ctx, pr.Human.Username, encoded); err != nil {
		return nil, huma.Error500InternalServerError("change password")
	}
	return nil, nil
}

// rolesOutput is the body of GET /api/v1/roles.
type rolesOutput struct {
	Body struct {
		Roles []roleBody `json:"roles"`
	}
}

type roleBody struct {
	ID          string   `json:"id"`
	Official    bool     `json:"official"`
	Permissions []string `json:"permissions"`
	Inherits    []string `json:"inherits"`
}

func rolesHandler(gw storage.Gateway) func(context.Context, *struct{}) (*rolesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*rolesOutput, error) {
		roles, err := gw.ListRoles(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list roles")
		}
		out := &rolesOutput{}
		out.Body.Roles = make([]roleBody, 0, len(roles))
		for _, r := range roles {
			out.Body.Roles = append(out.Body.Roles, roleBody{
				ID: r.ID, Official: r.Official, Permissions: r.Permissions, Inherits: r.Inherits,
			})
		}
		return out, nil
	}
}
