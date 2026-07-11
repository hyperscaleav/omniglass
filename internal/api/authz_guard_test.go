package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// ungated is the explicit allow-list of operations that carry NO capability
// gate, by design: the public liveness probe, and the authn-only identity
// endpoint (any authenticated principal may read its own /auth/me). Every other
// operation MUST reject a no-permission principal. Keep this list short and
// justified; a new entry is a security decision.
var ungated = map[string]bool{
	"GET /healthz":                       true, // public, no auth
	"GET /auth/status":                   true, // public: drives the login screen's bootstrap hint
	"GET /auth/me":                       true, // authn-only: returns the caller's own principal
	"GET /auth/me/avatar":                true, // authn-only, self-scoped: reads only the caller's own profile picture
	"PATCH /auth/me":                     true, // authn-only, self-scoped: edits only the caller's own profile
	"POST /auth/login":                   true, // public by necessity: it establishes a session
	"POST /auth/logout":                  true, // public: clearing a session must always succeed
	"POST /auth/me:changePassword":       true, // authn-only, self-scoped: changes only the caller's own password
	"POST /auth/me:setAvatar":            true, // authn-only, self-scoped: sets only the caller's own profile picture
	"POST /auth/me:removeAvatar":         true, // authn-only, self-scoped: clears only the caller's own profile picture
	"GET /auth/me/sessions":              true, // authn-only, self-scoped: lists only the caller's own sessions
	"POST /auth/me/sessions/{id}:revoke": true, // authn-only, self-scoped: revokes only the caller's own (a foreign id is a 404)

}

// TestEveryRouteIsGated is the no-unguarded-route guard. It enumerates every
// operation in the generated OpenAPI and drives it with an AUTHENTICATED but
// zero-permission principal: a capability-gated route must answer 403. A new
// route that forgets its `require(...)` gate would answer something else and
// fail here, so authorization coverage scales with the surface automatically
// instead of relying on per-route discipline.
func TestEveryRouteIsGated(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// An authenticated principal with NO grants: authn passes, every capability
	// check fails.
	noGrant := principalWithGrants(t, ctx, dsn, "no-grant", nil)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	for _, op := range operations(t, gw) {
		key := op.method + " " + op.path
		if ungated[key] {
			continue
		}
		t.Run(key, func(t *testing.T) {
			// Substitute any path params with a placeholder; the capability gate
			// runs before the handler, so the target need not exist.
			reqPath := op.path
			for _, p := range op.pathParams {
				reqPath = strings.ReplaceAll(reqPath, "{"+p+"}", "x")
			}
			// A minimal body for writes; capability runs before body validation,
			// so its contents do not matter.
			c := &apiClient{t: t, ctx: ctx, base: srv.URL}
			var body any
			if op.method == http.MethodPost || op.method == http.MethodPatch || op.method == http.MethodPut {
				body = map[string]any{}
			}
			code, _ := c.send(noGrant, op.method, reqPath, body)
			if code != http.StatusForbidden {
				t.Errorf("%s with a no-permission principal = %d, want 403 (route appears ungated)", key, code)
			}
		})
	}
}

type apiOp struct {
	method     string
	path       string // base-relative (the apiClient prepends /api/v1)
	pathParams []string
}

// operations parses the generated OpenAPI for the live operation set, so the
// guard tracks the surface exactly (a new route appears here automatically).
func operations(t *testing.T, gw storage.Gateway) []apiOp {
	t.Helper()
	raw, err := api.OpenAPIJSON(gw)
	if err != nil {
		t.Fatalf("openapi: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	var out []apiOp
	for p, methods := range doc.Paths {
		for m, op := range methods {
			var params []string
			for _, pa := range op.Parameters {
				if pa.In == "path" {
					params = append(params, pa.Name)
				}
			}
			out = append(out, apiOp{method: strings.ToUpper(m), path: p, pathParams: params})
		}
	}
	return out
}
