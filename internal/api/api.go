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

// NewHandler builds the routed HTTP handler. The gateway is the only dependency
// for the walking skeleton: healthz pings it. Later slices pass more
// collaborators here behind the same constructor.
func NewHandler(gw storage.Gateway) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		api := humachi.New(sub, apiConfig())
		registerRoutes(api, gw)
	})
	return r
}

// registerRoutes wires every Huma operation onto the API. Shared by NewHandler
// (the live server) and OpenAPIJSON (the server-less spec dump), so the routed
// surface and the generated spec can never drift.
func registerRoutes(api huma.API, gw storage.Gateway) {
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
	registerRoutes(api, gw)
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
