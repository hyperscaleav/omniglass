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
// and is (observed, resolved across every instance of the key, ADR-0456), with
// config-drift computed on read. Like the reachability read it is a plain typed Huma GET (no ViewResult
// framework exists yet), gated by component:read and scope-injected through
// GetComponent: an out-of-scope component is a non-disclosing 404, so the pivot
// below only runs on a verified, in-scope component. Read-only.

type reconPropertyBody struct {
	Property   string          `json:"property" doc:"The property_type key"`
	Want       json.RawMessage `json:"want" doc:"The declared value, resolved live from the cascade (null if none)"`
	Told       json.RawMessage `json:"told" doc:"The newest intended value a command set, on any instance (null until commanded)"`
	Is         json.RawMessage `json:"is" doc:"The observed value, resolved across every live instance of the property (a deleted endpoint's frozen reading left out): an instance that disagrees with the declared value if any does (the newest such), else the newest observed (null if none)"`
	IsInstance string          `json:"is_instance" doc:"The instance the observed value came from: the endpoint that observed it or the instance its sender named, empty for the owner-level series (and when is is null)"`
	Drift      bool            `json:"drift" doc:"True when any live observed instance differs from the declared value"`
}

type reconciliationOutput struct {
	Body struct {
		Component  string              `json:"component"`
		Properties []reconPropertyBody `json:"properties"`
	}
}

// systemReconciliationOutput is the same pivot for a system (#422): a system
// owns observed properties a push lands on it (ADR-0422), so its is reads
// beside its declared want exactly as a component's does.
type systemReconciliationOutput struct {
	Body struct {
		System     string              `json:"system"`
		Properties []reconPropertyBody `json:"properties"`
	}
}

// reconBodies maps the gateway's pivot to the wire, for either owner.
func reconBodies(recs []storage.PropertyReconciliation) []reconPropertyBody {
	out := make([]reconPropertyBody, 0, len(recs))
	for _, r := range recs {
		out = append(out, reconPropertyBody{
			Property: r.PropertyTypeName, Want: r.Want, Told: r.Told, Is: r.Is, IsInstance: r.IsInstance, Drift: r.Drift,
		})
	}
	return out
}

// registerReconciliationRoutes wires the per-component and per-system
// reconciliation reads, the operator-facing surface over want/told/is and
// drift.
func registerReconciliationRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component-reconciliation",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/reconciliation",
		Summary:     "Read a component's property reconciliation (want/told/is)",
		Description: "Pivots, per declared property, the declared value (want, resolved live from the cascade), the intended value (told), and the observed value (is, resolved across every live instance of the property and naming the instance it came from; a deleted endpoint's frozen reading is left out), with config-drift computed on read: drift when any live observed instance differs from want. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*reconciliationOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		recs, err := gw.Reconciliation(ctx, "component", comp.ID, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("read reconciliation")
		}
		out := &reconciliationOutput{}
		out.Body.Component = comp.Name
		out.Body.Properties = reconBodies(recs)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-system-reconciliation",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/systems/{name}/reconciliation",
		Summary:     "Read a system's property reconciliation (want/told/is)",
		Description: "Pivots, per property the system's standard declares or the system sets, the declared value (want, resolved live from the cascade), the intended value (told), and the observed value (is: what a push reported for the system as a whole), with config-drift computed on read: drift when the observed value differs from want. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*systemReconciliationOutput, error) {
		sys, err := gw.GetSystem(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		recs, err := gw.Reconciliation(ctx, "system", sys.ID, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("read reconciliation")
		}
		out := &systemReconciliationOutput{}
		out.Body.System = sys.Name
		out.Body.Properties = reconBodies(recs)
		return out, nil
	})
}
