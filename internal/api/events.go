package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The event read BFF: a per-component log of recent occurrences (the log-kind
// sink, the mirror of the reachability read over the metric/state sinks). A plain
// typed Huma GET, gated by component:read and scope-injected through GetComponent
// (an out-of-scope component is a non-disclosing 404, so the event read below only
// ever runs on a verified, in-scope component). Read-only.

// eventHistoryWindow bounds the occurrences returned; eventReadLimit caps the count.
const (
	eventHistoryWindow = 24 * time.Hour
	eventReadLimit     = 200
)

type eventBody struct {
	TS         time.Time       `json:"ts" doc:"When the occurrence was observed"`
	Key        string          `json:"key" doc:"The property name of the log (e.g. syslog.line)"`
	PropertyID string          `json:"property_id" doc:"The property's uuid, the stable form of key"`
	Instance   string          `json:"instance,omitempty" doc:"The series discriminator (e.g. the interface), when set"`
	Message    string          `json:"message" doc:"The occurrence message"`
	Attributes json.RawMessage `json:"attributes,omitempty" doc:"Structured attributes, when the occurrence carried a JSON payload"`
	Provenance string          `json:"provenance" doc:"The lineage of the occurrence (observed for direct collection)"`
	Source     string          `json:"source,omitempty" doc:"The interface type that produced the occurrence"`
}

type eventsOutput struct {
	Body struct {
		Component string      `json:"component"`
		Events    []eventBody `json:"events"`
	}
}

// registerEventRoutes wires the per-component event log read, the operator-facing
// surface over the collection log data.
func registerEventRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-events",
		Method:      http.MethodGet,
		Path:        "/components/{name}/events",
		Summary:     "List a component's recent events",
		Description: "Returns the component's recent log occurrences (the log-kind sink), newest first, bounded to the last 24 hours. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*eventsOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		since := time.Now().UTC().Add(-eventHistoryWindow)
		rows, err := gw.ListComponentEvents(ctx, comp.Name, since, eventReadLimit)
		if err != nil {
			return nil, huma.Error500InternalServerError("read events")
		}
		out := &eventsOutput{}
		out.Body.Component = comp.Name
		out.Body.Events = make([]eventBody, 0, len(rows))
		for _, e := range rows {
			out.Body.Events = append(out.Body.Events, eventBody{
				TS:  e.TS,
				Key: e.Key, PropertyID: e.PropertyID,
				Instance:   e.Instance,
				Message:    e.Message,
				Attributes: json.RawMessage(e.Attributes),
				Provenance: e.Provenance,
				Source:     e.Source,
			})
		}
		return out, nil
	})
}
