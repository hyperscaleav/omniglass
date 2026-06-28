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
	tok, ok := bearerToken(ctx.Header("Authorization"))
	if !ok {
		_ = huma.WriteErr(a.api, ctx, http.StatusUnauthorized, "unauthenticated")
		return
	}
	pr, err := a.gw.AuthenticateBearer(ctx.Context(), auth.HashToken(tok))
	switch {
	case errors.Is(err, storage.ErrCredentialNotFound):
		_ = huma.WriteErr(a.api, ctx, http.StatusUnauthorized, "unauthenticated")
		return
	case err != nil:
		_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "authentication failed")
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
		gb := grantBody{Role: g.Role, ScopeKind: g.ScopeKind}
		if g.ScopeID != nil {
			gb.ScopeID = *g.ScopeID
		}
		out.Body.Grants = append(out.Body.Grants, gb)
	}
	return out, nil
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
