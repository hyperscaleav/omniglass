// Package api is the HTTP/API surface, built on Huma over chi. It is the single
// integration contract: the future CLI and SPA are both clients of this, never
// of Postgres directly. The Go API (the Huma operation registrations) is the
// source of truth; the OpenAPI 3.1 document is generated from it.
//
// Conventions (held from the first slice):
//   - All operations are served under the version prefix /api/v1.
//   - The OpenAPI doc declares /api/v1 as the server base, so generated clients
//     target the prefixed routes while operation paths stay relative.
package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/webui"
)

// healthOutput is the body of GET /api/v1/healthz. Status is the overall
// process verdict ("ok" when the database leg is reachable, "degraded" when it
// is not); DB reports the Storage Gateway Ping result ("ok" or "down").
type healthOutput struct {
	Body struct {
		Status string `json:"status" doc:"Overall health: ok when all legs pass, degraded otherwise"`
		DB     string `json:"db" doc:"Database leg via the Storage Gateway: ok or down"`
	}
}

// options are the handler's tunables, set with the Option functions.
type options struct{ secureCookies bool }

// Option configures NewHandler.
type Option func(*options)

// WithSecureCookies marks the session cookie Secure (set behind TLS).
func WithSecureCookies(b bool) Option { return func(o *options) { o.secureCookies = b } }

// NewHandler builds the routed HTTP handler. The gateway is the only dependency
// for the walking skeleton: healthz pings it. Later slices pass more
// collaborators here behind the same constructor.
func NewHandler(gw storage.Gateway, opts ...Option) http.Handler {
	var o options
	for _, f := range opts {
		f(&o)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		api := humachi.New(sub, apiConfig())
		registerRoutes(api, gw, o)
	})

	// The operator console SPA is nested under /web/* (namespaces stay explicit:
	// /api/v1 = data, /web = console). StripPrefix lets the SPA handler resolve
	// assets (/web/assets/* -> dist/assets/*) and fall back to the shell for the
	// SPA's own client routes (/web/locations -> index.html, resolved by the
	// Solid router whose base is /web). The console is embedded only in
	// `-tags web` builds; otherwise a build-the-console placeholder is served.
	spa := http.StripPrefix("/web", webui.SPA())
	r.Get("/web", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/web/", http.StatusMovedPermanently)
	})
	r.Handle("/web/*", spa)

	return r
}

// registerRoutes wires every Huma operation onto the API. Shared by NewHandler
// (the live server) and OpenAPIJSON (the server-less spec dump), so the routed
// surface and the generated spec can never drift.
func registerRoutes(api huma.API, gw storage.Gateway, o options) {
	a := &authenticator{gw: gw, api: api, secureCookies: o.secureCookies}

	huma.Register(api, huma.Operation{
		OperationID: "get-healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness and database-reachability probe",
		Description: "Reports process health and the database leg, pinged through the Storage Gateway.",
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		out := &healthOutput{}
		if err := gw.Ping(ctx); err != nil {
			out.Body.Status = "degraded"
			out.Body.DB = "down"
			return out, nil
		}
		out.Body.Status = "ok"
		out.Body.DB = "ok"
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-status",
		Method:      http.MethodGet,
		Path:        "/auth/status",
		Summary:     "Whether the system has an owner yet",
		Description: "Public: reports whether any owner has been bootstrapped, so the login screen can hide the bootstrap hint.",
	}, a.statusHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "login",
		Method:        http.MethodPost,
		Path:          "/auth/login",
		Summary:       "Log in with a username and password",
		Description:   "Verifies a human's password and sets an httpOnly session cookie. Public; a bad credential is a flat 401, and a correct password against a disabled account is a distinct 403 so the screen can explain it.",
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden},
	}, a.loginHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/auth/logout",
		Summary:       "Log out the current session",
		Description:   "Revokes the session token and clears the cookie. Public.",
		DefaultStatus: http.StatusNoContent,
	}, a.logoutHandler)

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-me",
		Method:      http.MethodGet,
		Path:        "/auth/me",
		Summary:     "The authenticated principal, its permissions, and grants",
		Description: "Returns the caller's principal, flattened permissions (a UI hint and the fast-reject set), and grants. Requires authentication.",
		Middlewares: huma.Middlewares{a.authn},
	}, meHandler)

	huma.Register(api, huma.Operation{
		OperationID: "update-auth-me",
		Method:      http.MethodPatch,
		Path:        "/auth/me",
		Summary:     "Update your own profile",
		Description: "Updates the caller's own display name (email is administrator-set). Requires authentication; self-scoped (edits only your own principal).",
		Middlewares: huma.Middlewares{a.authn},
	}, a.updateMeHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "change-auth-me-password",
		Method:        http.MethodPost,
		Path:          "/auth/me:changePassword",
		Summary:       "Change your own password",
		Description:   "Verifies the current password and sets a new one. Requires authentication; self-scoped.",
		DefaultStatus: http.StatusNoContent,
		Middlewares:   huma.Middlewares{a.authn},
	}, a.changePasswordHandler)

	huma.Register(api, huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/roles",
		Summary:     "List roles",
		Description: "Lists the roles with their metadata and effective (flattened) permissions. Gated by the role:read capability.",
		Middlewares: huma.Middlewares{a.authn, a.require("role", "read")},
	}, a.rolesHandler(gw))

	registerLocationRoutes(api, a, gw)
	registerSystemRoutes(api, a, gw)
	registerComponentRoutes(api, a, gw)
	registerPrincipalRoutes(api, a, gw)
	registerImpersonationRoutes(api, a, gw)
}

// apiConfig is the shared Huma config for the live server and the spec dump. It
// declares /api/v1 as the OpenAPI server base so generated clients target the
// version-prefixed routes (the prefix is applied to the live router by the
// /api/v1 chi subrouter in NewHandler; operation paths stay relative).
func apiConfig() huma.Config {
	c := huma.DefaultConfig("Omniglass", "0.0.1-skeleton")
	c.Servers = []*huma.Server{{URL: "/api/v1"}}
	return c
}

// specAPI builds the API surface server-less so the OpenAPI document can be
// emitted without a running server. The gateway is only used to register
// handlers (never invoked here), so a nil-backed stub is fine.
func specAPI(gw storage.Gateway) huma.API {
	api := humachi.New(chi.NewRouter(), apiConfig())
	registerRoutes(api, gw, options{})
	return api
}

// OpenAPIJSON returns the OpenAPI 3.1 document as JSON, the source downstream
// clients are generated from (`make gen` -> cmd/openapigen).
func OpenAPIJSON(gw storage.Gateway) ([]byte, error) {
	return specAPI(gw).OpenAPI().MarshalJSON()
}

// OpenAPIYAML returns the same document as YAML.
func OpenAPIYAML(gw storage.Gateway) ([]byte, error) {
	return specAPI(gw).OpenAPI().YAML()
}
