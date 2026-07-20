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
	"github.com/hyperscaleav/omniglass/internal/settings"
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
type options struct {
	secureCookies bool
	// natsURL is the address the node-claim reply hands back, so a node needs only
	// the server URL to reach both the API and the bus.
	natsURL     string
	settingsSvc *settings.Service
}

// Option configures NewHandler.
type Option func(*options)

// WithSecureCookies marks the session cookie Secure (set behind TLS).
func WithSecureCookies(b bool) Option { return func(o *options) { o.secureCookies = b } }

// WithNatsURL sets the advertised NATS URL returned by the node-claim exchange.
func WithNatsURL(u string) Option { return func(o *options) { o.natsURL = u } }

// WithSettingsService supplies the settings engine service that backs the
// settings routes. When unset, NewHandler builds a code-defaults-only service so
// the handler always resolves settings (the boot path passes the real one, wired
// over the Storage Gateway).
func WithSettingsService(svc *settings.Service) Option {
	return func(o *options) { o.settingsSvc = svc }
}

// NewHandler builds the routed HTTP handler. The gateway backs data routes and
// healthz; the settings service backs the settings engine routes. Later slices
// pass more collaborators here behind the same constructor.
func NewHandler(gw storage.Gateway, opts ...Option) http.Handler {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if o.settingsSvc == nil {
		o.settingsSvc = defaultSettingsService(gw)
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		// Capture the User-Agent and client IP before Huma, so login and self-service
		// token creation can stamp a credential with the device and address behind it.
		sub.Use(captureClientMeta)
		api := humachi.New(sub, apiConfig())
		registerRoutes(api, gw, o.settingsSvc, o)
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
func registerRoutes(api huma.API, gw storage.Gateway, svc *settings.Service, o options) {
	a := &authenticator{gw: gw, api: api, secureCookies: o.secureCookies, perms: map[string]struct{}{}}

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
		OperationID: "list-auth-me-sessions",
		Method:      http.MethodGet,
		Path:        "/auth/me/sessions",
		Summary:     "List your own sessions and tokens",
		Description: "Lists the caller's own active bearer credentials (time-bounded web-login sessions and CLI/API tokens) with their non-secret metadata; the current one is flagged. Requires authentication; self-scoped (never another principal's). The token secret is never returned.",
		Middlewares: huma.Middlewares{a.authn},
	}, a.listMeSessionsHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-auth-me-session",
		Method:        http.MethodPost,
		Path:          "/auth/me/sessions/{id}:revoke",
		Summary:       "Revoke one of your own sessions",
		Description:   "Revokes one of the caller's own sessions or tokens by id (from the session list); revoking the current one signs it out. Requires authentication; self-scoped, so a credential id that is not yours is a 404.",
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound},
		Middlewares:   huma.Middlewares{a.authn},
	}, a.revokeMeSessionHandler)

	huma.Register(api, huma.Operation{
		OperationID: "revoke-all-auth-me-sessions",
		Method:      http.MethodPost,
		Path:        "/auth/me/sessions:revokeAll",
		Summary:     "Revoke all of your own sessions or tokens",
		Description: "Revokes every one of the caller's own web-login sessions, or every one of its CLI/API tokens (chosen by purpose), returning how many were ended. Requires authentication; self-scoped. Always keeps the credential that made this request, so you are never signed out of the one you are on; sessions and tokens never cross.",
		Middlewares: huma.Middlewares{a.authn},
	}, a.revokeAllMeSessionsHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "create-auth-me-token",
		Method:        http.MethodPost,
		Path:          "/auth/me/tokens",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create one of your own API tokens",
		Description:   "Mints a CLI/API token for the caller and returns it once (store it now; it cannot be retrieved again). A description is required (what the token is for); an optional ttl_days bounds its lifetime (default 90, maximum 365). Requires authentication; self-scoped (always issued for you). The token is stamped with the device and address that created it.",
		Middlewares:   huma.Middlewares{a.authn},
	}, a.createMeTokenHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "set-auth-me-avatar",
		Method:        http.MethodPost,
		Path:          "/auth/me:setAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Set your own profile picture",
		Description:   "Sets the caller's profile picture (JPEG, PNG, or WebP, base64-encoded), normalized server-side to a 256x256 JPEG. Requires authentication; self-scoped. A bad or oversize image is a 422.",
		Middlewares:   huma.Middlewares{a.authn},
	}, a.setMeAvatarHandler)

	huma.Register(api, huma.Operation{
		OperationID:   "remove-auth-me-avatar",
		Method:        http.MethodPost,
		Path:          "/auth/me:removeAvatar",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove your own profile picture",
		Description:   "Clears the caller's profile picture. Requires authentication; self-scoped.",
		Middlewares:   huma.Middlewares{a.authn},
	}, a.removeMeAvatarHandler)

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-me-avatar",
		Method:      http.MethodGet,
		Path:        "/auth/me/avatar",
		Summary:     "Get your own profile picture",
		Description: "Returns the caller's profile picture as a base64-encoded JPEG. Requires authentication; self-scoped. No picture is a 404.",
		Middlewares: huma.Middlewares{a.authn},
	}, a.meAvatarHandler)

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/roles",
		Summary:     "List roles",
		Description: "Lists the roles with their metadata and effective (flattened) permissions. Gated by the role:read:admin capability.",
	}, "role", "read", "admin"), a.rolesHandler(gw))

	registerLocationRoutes(api, a, gw)
	registerSystemRoutes(api, a, gw)
	registerComponentRoutes(api, a, gw)
	registerComponentMakeRoutes(api, a, gw)
	registerInterfaceRoutes(api, a, gw)
	registerTaskRoutes(api, a, gw)
	registerReachabilityRoutes(api, a, gw)
	registerNodeRoutes(api, a, gw, o.natsURL)
	registerSecretRoutes(api, a, gw)
	registerVariableRoutes(api, a, gw)
	registerFieldRoutes(api, a, gw)
	registerKeyRoutes(api, a, gw)
	registerTagRoutes(api, a, gw)
	registerFileRoutes(api, a, gw)
	registerPrincipalRoutes(api, a, gw)
	registerPrincipalGroupRoutes(api, a, gw)
	registerImpersonationRoutes(api, a, gw)
	registerAuditRoutes(api, a, gw)
	registerSettingsRoutes(api, a, gw, svc)
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
	registerRoutes(api, gw, defaultSettingsService(gw), options{})
	return api
}

// defaultSettingsService builds a settings service with no operator file, reading
// the global override live from the Gateway (the same seam the boot path uses,
// minus the operator file). It is the fallback when no service is wired via
// WithSettingsService, so NewHandler(gw) resolves real overrides; the boot path
// supplies a file-aware service instead. The override reader is never invoked
// during the server-less spec dump (handlers are only registered, not called), so
// specAPI's stub gateway is fine.
func defaultSettingsService(gw storage.Gateway) *settings.Service {
	return settings.NewService(nil, func(ctx context.Context, scope string) (settings.Doc, map[string][]string, error) {
		rows, err := gw.GetSettingOverrides(ctx, scope)
		if err != nil {
			return nil, nil, err
		}
		doc := settings.Doc{}
		locks := map[string][]string{}
		for _, r := range rows {
			doc[r.Namespace] = r.Doc
			if len(r.Locks) > 0 {
				locks[r.Namespace] = r.Locks
			}
		}
		return doc, locks, nil
	})
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
