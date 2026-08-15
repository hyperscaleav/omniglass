package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// eventTypeBody is the wire shape of an event type: an occurrence-keyspace entry,
// the twin of a property. payload_schema is an optional JSON Schema for the
// occurrence payload; official marks a seed-owned, read-only type.
type eventTypeBody struct {
	ID            string          `json:"id" doc:"The event type's uuid, the stable form of name"`
	Name          string          `json:"name"`
	Label         string          `json:"label,omitempty"`
	Description   string          `json:"description,omitempty"`
	PayloadSchema json.RawMessage `json:"payload_schema,omitempty" doc:"A JSON Schema fragment for the occurrence payload"`
	Official      bool            `json:"official"`
}

func toEventTypeBody(et *storage.EventType) eventTypeBody {
	b := eventTypeBody{
		ID: et.ID, Name: et.Name, Label: et.Label, Description: et.Description, Official: et.Official,
	}
	if len(et.PayloadSchema) > 0 {
		b.PayloadSchema = json.RawMessage(et.PayloadSchema)
	}
	return b
}

type listEventTypesOutput struct {
	Body struct {
		EventTypes []eventTypeBody `json:"event_types"`
	}
}

type eventTypeOutput struct{ Body eventTypeBody }

type eventTypeNameInput struct {
	Name string `path:"name" doc:"The event type's name"`
}

type createEventTypeInput struct {
	Body struct {
		Name          string `json:"name" minLength:"1" doc:"The event type name (lowercase kebab)"`
		Label         string `json:"label,omitempty" doc:"A human label"`
		Description   string `json:"description,omitempty" doc:"What the occurrence means"`
		PayloadSchema any    `json:"payload_schema,omitempty" doc:"A JSON Schema fragment for the payload"`
	}
}

type updateEventTypeInput struct {
	Name string `path:"name" doc:"The event type's name"`
	Body struct {
		Label         *string `json:"label,omitempty" doc:"A human label"`
		Description   *string `json:"description,omitempty" doc:"What the occurrence means"`
		PayloadSchema any     `json:"payload_schema,omitempty" doc:"A JSON Schema fragment (replaces wholesale)"`
	}
}

// registerEventTypeRoutes wires the event_type catalog: the estate-wide occurrence
// keyspace (no scope injection, it is reference data) and its custom-type CRUD. Read
// rides the viewer floor; create/update/delete are gated by event_type:create /
// event_type:update / event_type:delete. Official (seed-owned) event types are
// read-only. Mirrors the property_type catalog.
func registerEventTypeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-event-type",
		Method:      http.MethodGet,
		Path:        "/event-types",
		Summary:     "List event types",
		Description: "Lists every registered event type (official and custom). The catalog is estate-wide reference data. Gated by event_type:read.",
	}, "event_type", "read"), func(ctx context.Context, _ *struct{}) (*listEventTypesOutput, error) {
		ets, err := gw.ListEventTypes(ctx)
		if err != nil {
			return nil, mapEventTypeErr(err)
		}
		out := &listEventTypesOutput{}
		out.Body.EventTypes = make([]eventTypeBody, 0, len(ets))
		for i := range ets {
			out.Body.EventTypes = append(out.Body.EventTypes, toEventTypeBody(&ets[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-event-type",
		Method:      http.MethodGet,
		Path:        "/event-types/{name}",
		Summary:     "Get an event type",
		Description: "Returns one event type by name. Gated by event_type:read.",
	}, "event_type", "read"), func(ctx context.Context, in *eventTypeNameInput) (*eventTypeOutput, error) {
		et, err := gw.GetEventType(ctx, in.Name)
		if err != nil {
			return nil, mapEventTypeErr(err)
		}
		return &eventTypeOutput{Body: toEventTypeBody(et)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-event-type",
		Method:        http.MethodPost,
		Path:          "/event-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create an event type",
		Description:   "Registers a custom event type (official=false). The name must be a single kebab token, e.g. call-started. Gated by event_type:create.",
	}, "event_type", "create"), func(ctx context.Context, in *createEventTypeInput) (*eventTypeOutput, error) {
		schema, err := marshalValidation(in.Body.PayloadSchema)
		if err != nil {
			return nil, err
		}
		et, err := gw.CreateEventType(ctx, actorID(ctx), storage.EventTypeSpec{
			Name:          in.Body.Name,
			Label:         in.Body.Label,
			Description:   in.Body.Description,
			PayloadSchema: schema,
		})
		if err != nil {
			return nil, mapEventTypeErr(err)
		}
		return &eventTypeOutput{Body: toEventTypeBody(et)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-event-type",
		Method:      http.MethodPatch,
		Path:        "/event-types/{name}",
		Summary:     "Update an event type",
		Description: "Patches a custom event type's label, description, or payload schema (a nil field is unchanged). The name is fixed at creation. Official event types are read-only. Gated by event_type:update.",
	}, "event_type", "update"), func(ctx context.Context, in *updateEventTypeInput) (*eventTypeOutput, error) {
		schema, err := marshalValidation(in.Body.PayloadSchema)
		if err != nil {
			return nil, err
		}
		et, err := gw.UpdateEventType(ctx, actorID(ctx), in.Name, storage.EventTypePatch{
			Label:         in.Body.Label,
			Description:   in.Body.Description,
			PayloadSchema: schema,
		})
		if err != nil {
			return nil, mapEventTypeErr(err)
		}
		return &eventTypeOutput{Body: toEventTypeBody(et)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-event-type",
		Method:        http.MethodDelete,
		Path:          "/event-types/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete an event type",
		Description:   "Removes a custom event type by name. Official event types are read-only. Gated by event_type:delete.",
	}, "event_type", "delete"), func(ctx context.Context, in *eventTypeNameInput) (*struct{}, error) {
		if err := gw.DeleteEventType(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapEventTypeErr(err)
		}
		return nil, nil
	})
}

// mapEventTypeErr translates the gateway's event-type sentinels into HTTP
// status. mapRefErr runs first: an event type's name stays a single global
// token (#627 Task 12), so a dotted ref is storage.ErrAddressNotAccepted, a
// 422 naming the kind, rather than falling through to the plain not-found
// below.
func mapEventTypeErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrEventTypeNotFound):
		return huma.Error404NotFound("event type not found")
	case errors.Is(err, storage.ErrEventTypeExists):
		return huma.Error409Conflict("an event type with this name already exists")
	case errors.Is(err, storage.ErrEventTypeOfficial):
		return huma.Error409Conflict("an official event type is read-only")
	case errors.Is(err, storage.ErrEventTypeInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("event type operation failed")
	}
}
