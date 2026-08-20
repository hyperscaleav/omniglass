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
	TS          time.Time       `json:"ts" doc:"When the occurrence happened"`
	Key         string          `json:"key" doc:"The event_type name of the occurrence (e.g. call-started)"`
	EventTypeID string          `json:"event_type_id" doc:"The event_type's uuid, the stable form of key"`
	Origin      string          `json:"origin" doc:"How the occurrence arrived (caught/caused/derived/scheduled)"`
	Instance    string          `json:"instance,omitempty" doc:"The series discriminator (e.g. the interface), when set"`
	Message     string          `json:"message" doc:"The occurrence message"`
	Attributes  json.RawMessage `json:"attributes,omitempty" doc:"Structured attributes, when the occurrence carried a JSON payload"`
	Provenance  string          `json:"provenance" doc:"The lineage of the occurrence (observed for direct collection)"`
	Source      string          `json:"source,omitempty" doc:"The interface type that produced the occurrence"`
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
		rows, err := gw.ListComponentEvents(ctx, comp.ID, eventHistoryWindow, eventReadLimit)
		if err != nil {
			return nil, huma.Error500InternalServerError("read events")
		}
		out := &eventsOutput{}
		out.Body.Component = comp.Name
		out.Body.Events = make([]eventBody, 0, len(rows))
		for _, e := range rows {
			out.Body.Events = append(out.Body.Events, eventBody{
				TS:  e.TS,
				Key: e.Key, EventTypeID: e.EventTypeID,
				Origin:     e.Origin,
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

// The system-scoped read (#793): the system's own events and its members',
// one list, each row labeled by owner. The workspace's Events tab.
// Spelled out rather than embedding eventBody: Huma's schema generator does
// not flatten an embedded struct, so the embed would serve flattened JSON
// while the published schema hid the fields, which is drift by construction.
type systemEventBody struct {
	TS          time.Time       `json:"ts" doc:"When the occurrence happened"`
	Key         string          `json:"key" doc:"The event_type name of the occurrence (e.g. call-started)"`
	EventTypeID string          `json:"event_type_id" doc:"The event_type's uuid, the stable form of key"`
	Origin      string          `json:"origin" doc:"How the occurrence arrived (caught/caused/derived/scheduled)"`
	Instance    string          `json:"instance,omitempty" doc:"The series discriminator (e.g. the interface), when set"`
	Message     string          `json:"message" doc:"The occurrence message"`
	Attributes  json.RawMessage `json:"attributes,omitempty" doc:"Structured attributes, when the occurrence carried a JSON payload"`
	Provenance  string          `json:"provenance" doc:"The lineage of the occurrence (observed for direct collection)"`
	Source      string          `json:"source,omitempty" doc:"The interface type that produced the occurrence"`
	OwnerKind   string          `json:"owner_kind" doc:"Which arc raised the occurrence: system or component"`
	Owner       string          `json:"owner" doc:"The owning row's name (the system's, or the member component's)"`
}

type systemEventsOutput struct {
	Body struct {
		System string            `json:"system"`
		Events []systemEventBody `json:"events"`
	}
}

func registerSystemEventRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-events",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/events",
		Summary:     "List a system's recent events, members included",
		Description: "Returns the system's own events and its members', newest first, bounded to the last 24 hours, each row labeled by the owner that raised it. A component shared with another system appears in both systems' lists. Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*systemEventsOutput, error) {
		sys, err := gw.GetSystem(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		rows, err := gw.ListSystemEvents(ctx, sys.ID, eventHistoryWindow, eventReadLimit)
		if err != nil {
			return nil, huma.Error500InternalServerError("read system events")
		}
		out := &systemEventsOutput{}
		out.Body.System = sys.Name
		out.Body.Events = make([]systemEventBody, 0, len(rows))
		for _, e := range rows {
			out.Body.Events = append(out.Body.Events, systemEventBody{
				TS: e.TS, Key: e.Key, EventTypeID: e.EventTypeID, Origin: e.Origin, Instance: e.Instance,
				Message: e.Message, Attributes: e.Attributes, Provenance: e.Provenance, Source: e.Source,
				OwnerKind: e.OwnerKind, Owner: e.Owner,
			})
		}
		return out, nil
	})
}
