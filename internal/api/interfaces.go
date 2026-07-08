package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The interface CRUD surface: operator authoring of placement-bound connections.
// Both authz layers apply on every route: an interface:<action> permission
// (require middleware) AND scope injected by the gateway, cascading through the
// owning component (an out-of-scope component's interface is a non-disclosing
// 404, exactly like the component). Params is the endpoint/target jsonb, passed
// through as raw JSON.

type interfaceBody struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Component *string         `json:"component,omitempty" doc:"The owning component name; absent for a server-hosted interface"`
	Node      *string         `json:"node,omitempty" doc:"The node placement name, if assigned"`
	Params    json.RawMessage `json:"params,omitempty" doc:"The endpoint/target settings (jsonb)"`
}

func toInterfaceBody(it *storage.Interface) interfaceBody {
	b := interfaceBody{Name: it.Name, Type: it.Type, Component: it.Component, Node: it.Node}
	if len(it.Params) > 0 {
		b.Params = json.RawMessage(it.Params)
	}
	return b
}

type listInterfacesOutput struct {
	Body struct {
		Interfaces []interfaceBody `json:"interfaces"`
	}
}

type interfaceOutput struct {
	Body interfaceBody
}

type interfacePathInput struct {
	Name string `path:"name" doc:"The interface's unique name"`
}

type createInterfaceInput struct {
	Body struct {
		Name      string          `json:"name" minLength:"1" doc:"Globally unique name (the address)"`
		Type      string          `json:"type" minLength:"1" doc:"An interface_type name"`
		Component *string         `json:"component,omitempty" doc:"Owning component name; omit for a server-hosted interface (needs an all-scoped grant)"`
		Node      *string         `json:"node,omitempty" doc:"Node placement name"`
		Params    json.RawMessage `json:"params,omitempty" doc:"Endpoint/target settings (jsonb)"`
	}
}

type updateInterfaceInput struct {
	Name string `path:"name"`
	Body struct {
		Node   *string         `json:"node,omitempty" doc:"Reassign the node placement"`
		Params json.RawMessage `json:"params,omitempty" doc:"Replace the endpoint/target settings (jsonb)"`
	}
}

// registerInterfaceRoutes wires the interface CRUD surface, gated by
// interface:<action> and scope-injected through the owning component.
func registerInterfaceRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-interfaces",
		Method:      http.MethodGet,
		Path:        "/interfaces",
		Summary:     "List interfaces in scope",
		Description: "Lists the interfaces whose owning component the caller may read (the component cascade). Gated by interface:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("interface", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listInterfacesOutput, error) {
		ifaces, err := gw.ListInterfaces(ctx, a.scopeFor(ctx, "interface", "read"))
		if err != nil {
			return nil, mapInterfaceErr(err)
		}
		out := &listInterfacesOutput{}
		out.Body.Interfaces = make([]interfaceBody, 0, len(ifaces))
		for i := range ifaces {
			out.Body.Interfaces = append(out.Body.Interfaces, toInterfaceBody(&ifaces[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-interface",
		Method:      http.MethodGet,
		Path:        "/interfaces/{name}",
		Summary:     "Get an interface",
		Description: "Fetches an interface by name. An interface whose component is out of the caller's read scope is a non-disclosing 404. Gated by interface:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("interface", "read")},
	}, func(ctx context.Context, in *interfacePathInput) (*interfaceOutput, error) {
		it, err := gw.GetInterface(ctx, in.Name, a.scopeFor(ctx, "interface", "read"))
		if err != nil {
			return nil, mapInterfaceErr(err)
		}
		return &interfaceOutput{Body: toInterfaceBody(it)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-interface",
		Method:        http.MethodPost,
		Path:          "/interfaces",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create an interface",
		Description:   "Creates an interface owned by a component (or a server-hosted one, which needs an all-scoped grant). The create scope cascades through the owning component. Gated by interface:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("interface", "create")},
	}, func(ctx context.Context, in *createInterfaceInput) (*interfaceOutput, error) {
		it, err := gw.CreateInterface(ctx, actorID(ctx), storage.InterfaceSpec{
			Name:      in.Body.Name,
			Type:      in.Body.Type,
			Component: in.Body.Component,
			Node:      in.Body.Node,
			Params:    []byte(in.Body.Params),
		}, a.scopeFor(ctx, "interface", "create"))
		if err != nil {
			return nil, mapInterfaceErr(err)
		}
		return &interfaceOutput{Body: toInterfaceBody(it)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-interface",
		Method:      http.MethodPatch,
		Path:        "/interfaces/{name}",
		Summary:     "Update an interface",
		Description: "Patches an interface's node placement or params. Gated by interface:update; read and update scopes (through the component) drive the 404 versus 403 split.",
		Middlewares: huma.Middlewares{a.authn, a.require("interface", "update")},
	}, func(ctx context.Context, in *updateInterfaceInput) (*interfaceOutput, error) {
		it, err := gw.UpdateInterface(ctx, actorID(ctx), in.Name, storage.InterfacePatch{
			Node:   in.Body.Node,
			Params: []byte(in.Body.Params),
		}, a.scopeFor(ctx, "interface", "read"), a.scopeFor(ctx, "interface", "update"))
		if err != nil {
			return nil, mapInterfaceErr(err)
		}
		return &interfaceOutput{Body: toInterfaceBody(it)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-interface",
		Method:        http.MethodDelete,
		Path:          "/interfaces/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete an interface",
		Description:   "Deletes an interface, refused while a task still references it. Gated by interface:delete; read and delete scopes (through the component) drive the 404 versus 403 split.",
		Middlewares:   huma.Middlewares{a.authn, a.require("interface", "delete")},
	}, func(ctx context.Context, in *interfacePathInput) (*struct{}, error) {
		if err := gw.DeleteInterface(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "interface", "read"), a.scopeFor(ctx, "interface", "delete")); err != nil {
			return nil, mapInterfaceErr(err)
		}
		return nil, nil
	})
}

func mapInterfaceErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrInterfaceNotFound):
		return huma.Error404NotFound("interface not found")
	case errors.Is(err, storage.ErrInterfaceForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrInterfaceExists):
		return huma.Error409Conflict("interface name already exists")
	case errors.Is(err, storage.ErrInterfaceOccupied):
		return huma.Error409Conflict("interface still has tasks")
	case errors.Is(err, storage.ErrUnknownInterfaceType):
		return huma.Error422UnprocessableEntity("unknown interface type")
	case errors.Is(err, storage.ErrInterfaceComponentNotFound):
		return huma.Error422UnprocessableEntity("component not found")
	case errors.Is(err, storage.ErrInterfaceNodeNotFound):
		return huma.Error422UnprocessableEntity("node not found")
	default:
		return huma.Error500InternalServerError("interface operation failed")
	}
}
