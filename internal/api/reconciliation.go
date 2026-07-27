package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The reconciliation read BFF: a per-component pivot of each declared property's
// want (declared, resolved live from the cascade), told (intended, from a command),
// and is (observed, from the latest-value cache), with config-drift computed on
// read. Like the reachability read it is a plain typed Huma GET (no ViewResult
// framework exists yet), gated by component:read and scope-injected through
// GetComponent: an out-of-scope component is a non-disclosing 404, so the pivot
// below only runs on a verified, in-scope component. Read-only.

type reconPropertyBody struct {
	Property string          `json:"property" doc:"The property_type key"`
	Want     json.RawMessage `json:"want" doc:"The declared value, resolved live from the cascade (null if none)"`
	Told     json.RawMessage `json:"told" doc:"The intended value from a command (null until commanded)"`
	Is       json.RawMessage `json:"is" doc:"The observed value from the latest-value cache (null if none)"`
	Drift    bool            `json:"drift" doc:"True when the observed value is present and differs from the declared one"`
}

type reconciliationOutput struct {
	Body struct {
		Component  string              `json:"component"`
		Properties []reconPropertyBody `json:"properties"`
	}
}

// registerReconciliationRoutes wires the per-component reconciliation read, the
// operator-facing surface over want/told/is and drift.
func registerReconciliationRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-reconciliation",
		Method:      http.MethodGet,
		Path:        "/components/{name}/reconciliation",
		Summary:     "Read a component's property reconciliation (want/told/is)",
		Description: "Pivots, per declared property, the declared value (want, resolved live from the cascade), the intended value (told), and the observed value (is), with config-drift computed on read. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*reconciliationOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		recs, err := gw.Reconciliation(ctx, "component", comp.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("read reconciliation")
		}
		out := &reconciliationOutput{}
		out.Body.Component = comp.Name
		out.Body.Properties = make([]reconPropertyBody, 0, len(recs))
		for _, r := range recs {
			out.Body.Properties = append(out.Body.Properties, reconPropertyBody{
				Property: r.PropertyTypeName, Want: r.Want, Told: r.Told, Is: r.Is, Drift: r.Drift,
			})
		}
		return out, nil
	})
}
