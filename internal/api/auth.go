package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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
	// impModeCtxKey carries "view_as" / "act_as" when the request is impersonated,
	// and impSessionCtxKey the session id (so the caller can end it). The real
	// actor rides storage.RealActorContextKey so the audit writer records it.
	impModeCtxKey
	impSessionCtxKey
	// sessionHashCtxKey carries the sha256 of the bearer token that authenticated the
	// request, so the change-password handler can keep the caller's own session when it
	// signs out their other sessions.
	sessionHashCtxKey
)

// sessionHashFrom returns the sha256 of the request's bearer token, if one resolved.
func sessionHashFrom(ctx context.Context) ([]byte, bool) {
	h, ok := ctx.Value(sessionHashCtxKey).([]byte)
	return h, ok && len(h) > 0
}

func principalFrom(ctx context.Context) (*storage.Principal, bool) {
	pr, ok := ctx.Value(principalCtxKey).(*storage.Principal)
	return pr, ok
}

func permsFrom(ctx context.Context) (rbac.Set, bool) {
	s, ok := ctx.Value(permsCtxKey).(rbac.Set)
	return s, ok
}

// impersonationMode returns the impersonation mode ("view_as"/"act_as") for the
// request, empty when the caller is not impersonating.
func impersonationMode(ctx context.Context) string {
	m, _ := ctx.Value(impModeCtxKey).(string)
	return m
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
	var realActorID, impMode, impSession string
	var sessHash []byte
	for _, tok := range candidates {
		h := auth.HashToken(tok)
		p, err := a.gw.AuthenticateBearer(ctx.Context(), h)
		if err == nil {
			pr = p
			sessHash = h
			break
		}
		if !errors.Is(err, storage.ErrCredentialNotFound) {
			_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "authentication failed")
			return
		}
		// Bearer miss: try the impersonation-session fallback for the same token. It
		// resolves to the TARGET principal, carrying the real actor and the mode.
		ip, ra, mode, sid, ierr := a.gw.AuthenticateImpersonation(ctx.Context(), h)
		if ierr == nil {
			pr, realActorID, impMode, impSession = ip, ra, mode, sid
			break
		}
		if !errors.Is(ierr, storage.ErrCredentialNotFound) {
			_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "authentication failed")
			return
		}
		// both a bearer and an impersonation miss: try the next candidate token
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
	c = huma.WithValue(c, sessionHashCtxKey, sessHash)
	if impMode != "" {
		// Impersonated request: mark the mode and session, and set the real actor so
		// every audited mutation records who is really acting, never just the
		// impersonated principal.
		c = huma.WithValue(c, impModeCtxKey, impMode)
		c = huma.WithValue(c, impSessionCtxKey, impSession)
		c = huma.WithValue(c, storage.RealActorContextKey, realActorID)
	}
	// A view-as session is strictly read-only across EVERY route, enforced here in
	// authn (not in require), because the self-scoped routes (update-me,
	// change-password) skip the capability middleware. The HTTP method is the
	// reliable mutation signal; only ending the session (stop-impersonation) is
	// exempt. This is the single choke point a new mutating route cannot forget.
	if impMode == "view_as" && ctx.Method() != http.MethodGet && ctx.Method() != http.MethodHead && ctx.Operation().OperationID != "stop-impersonation" {
		_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, "read-only while viewing as another principal")
		return
	}
	// A principal an admin has flagged for a forced password change is gated to the
	// change-password lane on EVERY route until they change it: only reading their own
	// principal (so the console can see the flag and render the forced form) and the
	// change itself are allowed. Enforced here in authn (the single choke point) so no
	// route, read or write, can forget it. Not applied while impersonating (the real
	// actor, not the flagged target, is driving). Logout is public and never reaches here.
	if impMode == "" && pr.Human != nil && pr.Human.MustChangePassword &&
		ctx.Operation().OperationID != "get-auth-me" && ctx.Operation().OperationID != "change-auth-me-password" {
		_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, "password change required")
		return
	}
	next(c)
}

// require is Huma operation middleware enforcing a capability. It runs after
// authn; a principal whose flattened permissions do not allow the required
// permission is 403. The permission is given as its tokens, so a normal route
// declares require("location", "read") and an admin-sensitive one declares the
// third token, require("audit", "read", "admin"). Scope (which entities) is the
// gateway's job and lands when entities exist.
func (a *authenticator) require(tokens ...string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// view-as read-only is enforced in authn (a method-based choke point over
		// every route, including the capability-less self-scoped ones), not here.
		perms, ok := permsFrom(ctx.Context())
		if !ok || !perms.Allows(tokens...) {
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
		sg := scope.Grant{Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
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

// sessionCookieLifetime is the absolute lifetime of a login session: the bearer
// credential and the cookie both expire after it, so a stolen session cookie is not
// valid forever. Fixed for this slice (a sliding idle timeout is a later refinement).
// A CLI/API token (omniglass token) has its own, much longer default (auth.DefaultTokenLifetime).
const sessionCookieLifetime = 12 * time.Hour

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
		// The browser drops the cookie at the same absolute lifetime the server
		// bounds the credential to, so a closed session cannot linger client-side.
		MaxAge: int(sessionCookieLifetime.Seconds()),
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
		// A wrong password on a REAL account is audited (attributed to that
		// principal), a brute-force signal; an unknown username returns a nil
		// principal and is not audited, so scanning cannot flood the log. Best
		// effort: the login already failed, so an audit error does not change it.
		if pr != nil {
			_ = a.gw.WriteAuthEvent(ctx, pr.ID, "login_failed")
		}
		return nil, huma.Error401Unauthorized("invalid username or password")
	case errors.Is(err, storage.ErrAccountLocked):
		// Too many failed attempts: the account is in its lockout window. Return the
		// SAME generic 401 as a bad credential so the lock is not an enumeration
		// oracle; only the audit (attributed to the locked principal) records it.
		if pr != nil {
			_ = a.gw.WriteAuthEvent(ctx, pr.ID, "login_locked")
		}
		return nil, huma.Error401Unauthorized("invalid username or password")
	case errors.Is(err, storage.ErrAccountDisabled):
		// The password was correct but the account is disabled. A distinct 403 (not
		// the generic 401) so the sign-in screen can explain it; only reachable with
		// the right password, so it discloses nothing to an attacker without it. The
		// denied attempt is audited (attributed to the disabled principal).
		if pr != nil {
			_ = a.gw.WriteAuthEvent(ctx, pr.ID, "login_denied")
		}
		return nil, huma.Error403Forbidden("account disabled")
	case err != nil:
		return nil, huma.Error500InternalServerError("login failed")
	}
	token, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		return nil, huma.Error500InternalServerError("login failed")
	}
	expiresAt := time.Now().Add(sessionCookieLifetime)
	if _, err := a.gw.IssueBearerCredential(ctx, pr.Human.Username, hash, prefix, "session", &expiresAt); err != nil {
		return nil, huma.Error500InternalServerError("login failed")
	}
	if err := a.gw.WriteAuthEvent(ctx, pr.ID, "login"); err != nil {
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
		hash := auth.HashToken(tok)
		// Resolve the principal before revoking so the logout is attributed; a
		// best-effort audit (logout must clear the cookie regardless).
		if pr, err := a.gw.AuthenticateBearer(ctx, hash); err == nil {
			_ = a.gw.WriteAuthEvent(ctx, pr.ID, "logout")
		}
		_ = a.gw.RevokeBearer(ctx, hash)
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
	Username           string `json:"username"`
	Email              string `json:"email,omitempty"`
	DisplayName        string `json:"display_name,omitempty"`
	MustChangePassword bool   `json:"must_change_password,omitempty" doc:"True when an admin reset the password and the user must change it before doing anything else; the console gates every route to the change-password form until it clears."`
	HasAvatar          bool   `json:"has_avatar,omitempty" doc:"True when the principal has a profile picture; fetch it from the avatar endpoint."`
}

type svcBody struct {
	Label string `json:"label"`
}

type grantBody struct {
	ID        string `json:"id,omitempty"`
	Role      string `json:"role"`
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id,omitempty"`
	ScopeOp   string `json:"scope_op,omitempty" enum:"subtree,subtree_excl_root,self" doc:"How the scope root matches the tree: subtree (root + descendants), subtree_excl_root (descendants only for update/delete, root kept for read/create), or self (the root row only). Empty means subtree. Moot for the all scope."`
	GroupID   string `json:"group_id,omitempty" doc:"Set when this grant is inherited from a group the principal belongs to (the group's id); absent for a direct grant, which is the only kind revocable from the principal."`
	GroupName string `json:"group_name,omitempty" doc:"The source group's label, present when the grant is inherited."`
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
		out.Body.Human = &humanBody{
			Username:           pr.Human.Username,
			Email:              pr.Human.Email,
			DisplayName:        pr.Human.DisplayName,
			MustChangePassword: pr.Human.MustChangePassword,
			HasAvatar:          pr.Human.HasAvatar,
		}
	}
	if pr.Service != nil {
		out.Body.Service = &svcBody{Label: pr.Service.Label}
	}
	out.Body.Permissions = perms.Strings()
	out.Body.Grants = make([]grantBody, 0, len(pr.Grants))
	for i := range pr.Grants {
		out.Body.Grants = append(out.Body.Grants, toGrantBody(&pr.Grants[i]))
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
		NewPassword     string `json:"new_password" minLength:"12" maxLength:"256" doc:"The new password (at least 12 characters, not a common password, not containing the username)"`
	}
}

// changePasswordHandler verifies the caller's current password and sets a new one.
// Self-scoped and authn-only. A wrong current password is a 403 (the request is
// authenticated, but not permitted to rotate without the current secret). Changing
// the password signs out the caller's OTHER sessions and tokens (every bearer except
// the one making this request), so a changed password takes effect everywhere at once.
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
	if err := mapPasswordErr(auth.ValidatePassword(in.Body.NewPassword, pr.Human.Username)); err != nil {
		return nil, err
	}
	encoded, err := auth.HashPassword(in.Body.NewPassword)
	if err != nil {
		return nil, huma.Error500InternalServerError("change password")
	}
	if _, err := a.gw.SetPassword(ctx, pr.Human.Username, encoded); err != nil {
		return nil, huma.Error500InternalServerError("change password")
	}
	// Force logout of the caller's other sessions and tokens, keeping the current one
	// so the change does not sign the caller out of the session they just used.
	keep := [][]byte{}
	if h, ok := sessionHashFrom(ctx); ok {
		keep = append(keep, h)
	}
	if _, err := a.gw.RevokePrincipalBearers(ctx, pr.ID, keep); err != nil {
		return nil, huma.Error500InternalServerError("change password")
	}
	return nil, nil
}

// sessionBody is one of the caller's own bearer credentials in the self-service
// session list. It carries only non-secret metadata: the secret hash is never
// returned. Kind is the credential's purpose: a web-login "session" or a CLI/API
// "token"; both now carry an expiry, so the discriminator is the stored purpose, not
// whether expires_at is set. Current marks the credential that authenticated this very
// request, so the console can mark it and read its revoke as a sign-out.
type sessionBody struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind" enum:"session,token" doc:"session for a web login, token for a CLI/API credential (omniglass token)"`
	Prefix    string  `json:"prefix" doc:"The non-secret ogp locator, so a credential is recognizable without exposing the token"`
	CreatedAt string  `json:"created_at" doc:"When the credential was issued (RFC 3339)"`
	ExpiresAt *string `json:"expires_at,omitempty" doc:"When the credential expires (RFC 3339); every credential is now time-bounded"`
	Current   bool    `json:"current" doc:"True for the credential that authenticated this request; revoking it signs out the current session"`
}

// listMeSessionsOutput is the body of GET /api/v1/auth/me/sessions.
type listMeSessionsOutput struct {
	Body struct {
		Sessions []sessionBody `json:"sessions"`
	}
}

// toSessionBodies renders stored bearer credentials as the session wire shape,
// newest first, never leaking the secret. The kind is the stored purpose (a web-login
// "session" or a CLI/API "token"); both now carry an expiry, so a legacy row with no
// purpose falls back to its expiry shape. Shared by the self-service list and the admin
// per-principal list, so both expose the same shape.
func toSessionBodies(creds []storage.BearerCredential) []sessionBody {
	out := make([]sessionBody, 0, len(creds))
	for _, c := range creds {
		// The kind is the stored purpose (session vs token); a legacy row with no
		// purpose falls back to its expiry shape (a session had one, a token did not).
		kind := c.Purpose
		if kind == "" {
			kind = "token"
			if c.ExpiresAt != nil {
				kind = "session"
			}
		}
		s := sessionBody{
			ID:        c.ID,
			Kind:      kind,
			Prefix:    c.Prefix,
			CreatedAt: c.CreatedAt.Format(time.RFC3339),
			Current:   c.Current,
		}
		if c.ExpiresAt != nil {
			exp := c.ExpiresAt.Format(time.RFC3339)
			s.ExpiresAt = &exp
		}
		out = append(out, s)
	}
	return out
}

// listMeSessionsHandler returns the caller's own active bearer credentials (login
// sessions and API tokens) with their non-secret metadata, newest first. Self-scoped
// and authn-only: it lists only the principal resolved from the request, never
// another. The current credential is flagged so the console can mark it.
func (a *authenticator) listMeSessionsHandler(ctx context.Context, _ *struct{}) (*listMeSessionsOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	currentHash, _ := sessionHashFrom(ctx)
	creds, err := a.gw.ListBearerCredentials(ctx, pr.ID, currentHash)
	if err != nil {
		return nil, huma.Error500InternalServerError("list sessions")
	}
	out := &listMeSessionsOutput{}
	out.Body.Sessions = toSessionBodies(creds)
	return out, nil
}

// revokeMeSessionInput is the path input of POST /auth/me/sessions/{id}:revoke.
type revokeMeSessionInput struct {
	ID string `path:"id" doc:"The credential id to revoke (from the session list)"`
}

// revokeMeSessionHandler revokes one of the caller's own bearer credentials by id.
// Self-scoped and authn-only: the revoke is bounded to the caller's principal, so a
// credential id that is not theirs (or does not exist) is a non-disclosing 404, never
// a cross-principal revoke. Revoking the current credential is permitted (it is a
// logout of this session).
func (a *authenticator) revokeMeSessionHandler(ctx context.Context, in *revokeMeSessionInput) (*struct{}, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	revoked, err := a.gw.RevokeBearerByID(ctx, pr.ID, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("revoke session")
	}
	if !revoked {
		return nil, huma.Error404NotFound("no such session")
	}
	return nil, nil
}

// setAvatarMeInput is the body of POST /api/v1/auth/me:setAvatar: the image to
// use as the caller's own profile picture, base64-encoded.
type setAvatarMeInput struct {
	Body struct {
		ImageBase64 string `json:"image_base64" doc:"The image (JPEG, PNG, or WebP), base64-encoded; normalized server-side to a 256x256 JPEG"`
	}
}

// setMeAvatarHandler sets the caller's own profile picture. Self-scoped and
// authn-only: it writes the principal resolved from the session, never another. A
// bad or oversize image is a 422 (from normalizeAvatar).
func (a *authenticator) setMeAvatarHandler(ctx context.Context, in *setAvatarMeInput) (*struct{}, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	if pr.Human == nil {
		return nil, huma.Error422UnprocessableEntity("only human principals have a profile picture")
	}
	b64, err := normalizeAvatar(in.Body.ImageBase64)
	if err != nil {
		return nil, err
	}
	if err := a.gw.SetOwnAvatar(ctx, pr.ID, b64); err != nil {
		return nil, huma.Error500InternalServerError("set avatar")
	}
	return nil, nil
}

// removeMeAvatarHandler clears the caller's own profile picture. Self-scoped and
// authn-only; clearing an absent picture is a no-op.
func (a *authenticator) removeMeAvatarHandler(ctx context.Context, _ *struct{}) (*struct{}, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	if pr.Human == nil {
		return nil, huma.Error422UnprocessableEntity("only human principals have a profile picture")
	}
	if err := a.gw.ClearOwnAvatar(ctx, pr.ID); err != nil {
		return nil, huma.Error500InternalServerError("remove avatar")
	}
	return nil, nil
}

// meAvatarHandler returns the caller's own profile picture as base64. Self-scoped
// and authn-only: it reads the principal resolved from the session, never another.
// No picture is a 404 (not an empty 200).
func (a *authenticator) meAvatarHandler(ctx context.Context, _ *struct{}) (*avatarOutput, error) {
	pr, ok := principalFrom(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("unauthenticated")
	}
	if pr.Human == nil {
		return nil, huma.Error422UnprocessableEntity("only human principals have a profile picture")
	}
	b64, has, err := a.gw.GetHumanAvatar(ctx, pr.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get avatar")
	}
	if !has {
		return nil, huma.Error404NotFound("no profile picture")
	}
	out := &avatarOutput{}
	out.Body.ImageBase64 = b64
	return out, nil
}

// mapPasswordErr translates the auth password-policy sentinels into a 422 with a
// specific message, or nil when the password is acceptable. Shared by the create and
// change-password handlers so both enforce the same policy the same way.
func mapPasswordErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrPasswordTooShort):
		return huma.Error422UnprocessableEntity(fmt.Sprintf("password must be at least %d characters", auth.MinPasswordLength))
	case errors.Is(err, auth.ErrPasswordTooLong):
		return huma.Error422UnprocessableEntity(fmt.Sprintf("password must be at most %d characters", auth.MaxPasswordLength))
	case errors.Is(err, auth.ErrPasswordCommon):
		return huma.Error422UnprocessableEntity("password is too common; choose a less predictable one")
	case errors.Is(err, auth.ErrPasswordContainsIdentifier):
		return huma.Error422UnprocessableEntity("password must not contain the username")
	default:
		return huma.Error422UnprocessableEntity("password does not meet the policy")
	}
}

// rolesOutput is the body of GET /api/v1/roles.
type rolesOutput struct {
	Body struct {
		Roles []roleBody `json:"roles"`
	}
}

type roleBody struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Official    bool     `json:"official"`
	Permissions []string `json:"permissions"`
	Inherits    []string `json:"inherits"`
	// EffectivePermissions is what the role actually confers, flattened through the
	// role index (inheritance, wildcard, and the :read floor resolved), so the UI
	// shows a role's real reach without re-implementing the resolution.
	EffectivePermissions []string `json:"effective_permissions"`
}

func (a *authenticator) rolesHandler(gw storage.Gateway) func(context.Context, *struct{}) (*rolesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*rolesOutput, error) {
		roles, err := gw.ListRoles(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list roles")
		}
		idx, err := a.roleIndex(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list roles")
		}
		out := &rolesOutput{}
		out.Body.Roles = make([]roleBody, 0, len(roles))
		for _, r := range roles {
			out.Body.Roles = append(out.Body.Roles, roleBody{
				ID: r.ID, DisplayName: r.DisplayName, Description: r.Description,
				Official: r.Official, Permissions: r.Permissions, Inherits: r.Inherits,
				EffectivePermissions: idx.Flatten([]string{r.ID}).Strings(),
			})
		}
		return out, nil
	}
}
