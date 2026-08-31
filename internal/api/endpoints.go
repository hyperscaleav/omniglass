package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/transport"
)

// The endpoint CRUD surface: operator authoring of placement-bound connections
// (ADR-0134: the entity renamed from the old noun). Both authz layers
// apply on every route: an endpoint:<action> permission (require middleware)
// AND scope injected by the gateway, cascading through the owning component
// (an out-of-scope component's endpoint is a non-disclosing 404, exactly like
// the component). Params is the address/target jsonb, passed through as raw
// JSON. The transports an endpoint can speak are a code registry
// (internal/transport), served read-only at /transports for the console
// picker.

type endpointBody struct {
	ID          string          `json:"id" doc:"The endpoint's surrogate id (the address)"`
	Name        string          `json:"name" doc:"The derived name (its transport), unique within the owning component"`
	Label       string          `json:"label,omitempty" doc:"The friendly string an operator reads, and the only identity string an operator types here: the name is derived from the transport. Absent when unset, and a surface with none renders the name verbatim"`
	Transport   string          `json:"transport" doc:"The transport name (the wire this endpoint speaks over), from the code registry"`
	Component   *string         `json:"component,omitempty" doc:"The owning component name; absent for a server-hosted endpoint"`
	ComponentID *string         `json:"component_id,omitempty" doc:"The owning component's id; the stable form of component"`
	Node        *string         `json:"node,omitempty" doc:"The node placement name, if assigned"`
	NodeID      *string         `json:"node_id,omitempty" doc:"The placed node's id; the stable form of node"`
	Params      json.RawMessage `json:"params,omitempty" doc:"The address/target settings (jsonb)"`
	Driver      *string         `json:"driver,omitempty" doc:"The driver this endpoint was attached through (#813); absent for a bare probe endpoint"`
	DriverID    *string         `json:"driver_id,omitempty" doc:"The attached driver's id; the stable form of driver"`
	Inputs      json.RawMessage `json:"inputs,omitempty" doc:"The effective inputs supplied at attach, defaults baked; secret inputs are reference names, never values"`
}

func toEndpointBody(it *storage.Endpoint) endpointBody {
	b := endpointBody{
		ID: it.ID, Name: it.Name, Label: it.Label, Transport: it.Transport,
		Component: it.Component, ComponentID: it.ComponentID,
		Node: it.Node, NodeID: it.NodeID,
	}
	if len(it.Params) > 0 {
		b.Params = json.RawMessage(it.Params)
	}
	b.Driver, b.DriverID = it.Driver, it.DriverID
	// Inputs render for an attachment (a driver-authored endpoint), keyed off the
	// fact the row actually carries rather than the byte length of its jsonb.
	if it.DriverID != nil && len(it.Inputs) > 0 {
		b.Inputs = json.RawMessage(it.Inputs)
	}
	return b
}

type listEndpointsOutput struct {
	Body struct {
		Endpoints []endpointBody `json:"endpoints"`
	}
}

type endpointOutput struct {
	Body endpointBody
}

type endpointPathInput struct {
	ID string `path:"id" doc:"The endpoint's surrogate id"`
}

type createEndpointInput struct {
	Body struct {
		Transport string            `json:"transport,omitempty" doc:"A transport name from the code registry (GET /transports); the endpoint is named by it, unique within the component. Exactly one of transport (a bare probe endpoint) or driver (an attach) is set"`
		Label     string            `json:"label,omitempty" maxLength:"200" doc:"What an operator reads in lists (Control processor). Settable here because the name is derived from the transport, so it says how the device is reached and never what the connection is for"`
		Component *string           `json:"component,omitempty" doc:"Owning component, by name or id; omit for a server-hosted endpoint (needs an all-scoped grant)"`
		Node      *string           `json:"node,omitempty" doc:"Node placement, by name or id"`
		Params    json.RawMessage   `json:"params,omitempty" doc:"Address/target settings (jsonb); an attach derives them from the inputs instead"`
		Driver    *string           `json:"driver,omitempty" doc:"Attach this driver (by name or id): the spec derives the transport and params, and the endpoint's tasks derive from the spec's functions"`
		Inputs    map[string]string `json:"inputs,omitempty" doc:"The inputs the driver's spec declares (host, port, credentials as secret reference names); required ones must be supplied, defaults fill the rest"`
	}
}

type updateEndpointInput struct {
	ID   string `path:"id"`
	Body struct {
		Node   *string         `json:"node,omitempty" doc:"Reassign the node placement, by name or id"`
		Label  *string         `json:"label,omitempty" maxLength:"200" doc:"A new label; an empty string clears it, and the surface falls back to the derived name. Omit to leave it alone"`
		Params json.RawMessage `json:"params,omitempty" doc:"Replace the address/target settings (jsonb)"`
	}
}

type listTransportsOutput struct {
	Body struct {
		Transports []transport.Transport `json:"transports"`
	}
}

// registerEndpointRoutes wires the endpoint CRUD surface, gated by
// endpoint:<action> and scope-injected through the owning component, plus the
// read-only transport registry the console picker consumes.
func registerEndpointRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-transports",
		Method:      http.MethodGet,
		Path:        "/transports",
		Summary:     "List the transports the platform speaks",
		Description: "Lists the transport registry: the wires an endpoint can speak over, a build-time fact of the binary (ADR-0073). Gated by endpoint:read, the surface that consumes it.",
	}, "endpoint", "read"), func(ctx context.Context, _ *struct{}) (*listTransportsOutput, error) {
		out := &listTransportsOutput{}
		out.Body.Transports = transport.All()
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-endpoints",
		Method:      http.MethodGet,
		Path:        "/endpoints",
		Summary:     "List endpoints in scope",
		Description: "Lists the endpoints whose owning component the caller may read (the component cascade). Gated by endpoint:read.",
	}, "endpoint", "read"), func(ctx context.Context, _ *struct{}) (*listEndpointsOutput, error) {
		eps, err := gw.ListEndpoints(ctx, a.scopeFor(ctx, "endpoint", "read"))
		if err != nil {
			return nil, mapEndpointErr(err)
		}
		out := &listEndpointsOutput{}
		out.Body.Endpoints = make([]endpointBody, 0, len(eps))
		for i := range eps {
			out.Body.Endpoints = append(out.Body.Endpoints, toEndpointBody(&eps[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-endpoint",
		Method:      http.MethodGet,
		Path:        "/endpoints/{id}",
		Summary:     "Get an endpoint",
		Description: "Fetches an endpoint by id. An endpoint whose component is out of the caller's read scope is a non-disclosing 404. Gated by endpoint:read.",
	}, "endpoint", "read"), func(ctx context.Context, in *endpointPathInput) (*endpointOutput, error) {
		it, err := gw.GetEndpoint(ctx, in.ID, a.scopeFor(ctx, "endpoint", "read"))
		if err != nil {
			return nil, mapEndpointErr(err)
		}
		return &endpointOutput{Body: toEndpointBody(it)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-endpoint",
		Method:        http.MethodPost,
		Path:          "/endpoints",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create an endpoint",
		Description:   "Creates an endpoint owned by a component (or a server-hosted one, which needs an all-scoped grant), named by its transport; the optional label is the only identity string an operator types, and is where what the connection is FOR goes. The create scope cascades through the owning component. Gated by endpoint:create.",
	}, "endpoint", "create"), func(ctx context.Context, in *createEndpointInput) (*endpointOutput, error) {
		if in.Body.Transport == "" && in.Body.Driver == nil {
			return nil, huma.Error422UnprocessableEntity("set a transport (a bare probe endpoint) or a driver (an attach)")
		}
		it, err := gw.CreateEndpoint(ctx, actorID(ctx), storage.EndpointSpec{
			Transport: in.Body.Transport,
			Label:     in.Body.Label,
			Component: in.Body.Component,
			Node:      in.Body.Node,
			Params:    []byte(in.Body.Params),
			Driver:    in.Body.Driver,
			Inputs:    in.Body.Inputs,
			// An attach resolves its secret references under the caller's own
			// secret read scope and admin tier, so it can only bind a secret the
			// operator could themselves read (never another tenant's, never an
			// admin-sensitive one without the tier).
			SecretRead: a.scopeFor(ctx, "secret", "read"),
			CanAdmin:   a.canSecretAdmin(ctx, "read"),
		}, a.scopeFor(ctx, "endpoint", "create"))
		if err != nil {
			return nil, mapEndpointErr(err)
		}
		return &endpointOutput{Body: toEndpointBody(it)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-endpoint",
		Method:      http.MethodPatch,
		Path:        "/endpoints/{id}",
		Summary:     "Update an endpoint",
		Description: "Patches an endpoint's node placement, params or label; an empty label clears it and the surface falls back to the derived name. Gated by endpoint:update; read and update scopes (through the component) drive the 404 versus 403 split.",
	}, "endpoint", "update"), func(ctx context.Context, in *updateEndpointInput) (*endpointOutput, error) {
		it, err := gw.UpdateEndpoint(ctx, actorID(ctx), in.ID, storage.EndpointPatch{
			Node:   in.Body.Node,
			Params: []byte(in.Body.Params),
			Label:  in.Body.Label,
		}, a.scopeFor(ctx, "endpoint", "read"), a.scopeFor(ctx, "endpoint", "update"))
		if err != nil {
			return nil, mapEndpointErr(err)
		}
		return &endpointOutput{Body: toEndpointBody(it)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-endpoint",
		Method:        http.MethodDelete,
		Path:          "/endpoints/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete an endpoint",
		Description:   "Deletes an endpoint, refused while a task still references it. Gated by endpoint:delete; read and delete scopes (through the component) drive the 404 versus 403 split.",
	}, "endpoint", "delete"), func(ctx context.Context, in *endpointPathInput) (*struct{}, error) {
		if err := gw.DeleteEndpoint(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "endpoint", "read"), a.scopeFor(ctx, "endpoint", "delete")); err != nil {
			return nil, mapEndpointErr(err)
		}
		return nil, nil
	})
}

func mapEndpointErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrAttachInvalid), errors.Is(err, storage.ErrSpecInvalid):
		// The attach gate names the fault (a missing input, a secret reference
		// that resolves to nothing); surface it so the author can fix the call.
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: "))
	case errors.Is(err, storage.ErrTypeNotFound):
		return huma.Error422UnprocessableEntity("driver not found")
	case errors.Is(err, storage.ErrEndpointNotFound):
		return huma.Error404NotFound("endpoint not found")
	case errors.Is(err, storage.ErrEndpointForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrEndpointExists):
		return huma.Error409Conflict("an endpoint of that transport already exists on this component")
	case errors.Is(err, storage.ErrUnknownTransport):
		return huma.Error422UnprocessableEntity("unknown transport")
	case errors.Is(err, storage.ErrEndpointComponentNotFound):
		return huma.Error422UnprocessableEntity("component not found")
	case errors.Is(err, storage.ErrEndpointNodeNotFound), errors.Is(err, storage.ErrNodeNotFound):
		return huma.Error422UnprocessableEntity("node not found")
	case errors.Is(err, storage.ErrEndpointDriverParams):
		return huma.Error422UnprocessableEntity("a driver-attached endpoint's address is derived from its inputs; re-attach the driver to change it")
	default:
		return huma.Error500InternalServerError("endpoint operation failed")
	}
}
