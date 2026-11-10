package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// componentDependencyBody is one dependency edge on the wire (#1648): the
// component depends on the upstream, by power (fed from one of its outlets),
// host (hosted by it) or path (reached through it). Containment (a component's
// parent) is a different fact and never an edge here.
type componentDependencyBody struct {
	Component   string    `json:"component" doc:"Name of the dependent component, the one that goes down with the upstream"`
	ComponentID string    `json:"component_id" doc:"The dependent component's uuid"`
	Upstream    string    `json:"upstream" doc:"Name of the component it depends on"`
	UpstreamID  string    `json:"upstream_id" doc:"The upstream component's uuid"`
	Kind        string    `json:"kind" enum:"power,host,path" doc:"How it depends: power (fed from an outlet of the upstream, a PDU), host (hosted by the upstream, a codec and the peripheral it reboots, a chassis and its line card) or path (reached through the upstream, a gateway)"`
	Outlet      string    `json:"outlet,omitempty" doc:"The upstream's outlet that feeds the component; set on a power edge only"`
	CreatedAt   time.Time `json:"created_at" doc:"When the edge was recorded"`
}

func toComponentDependencyBody(d storage.ComponentDependency) componentDependencyBody {
	return componentDependencyBody{
		Component: d.ComponentName, ComponentID: d.ComponentID, Upstream: d.UpstreamName, UpstreamID: d.UpstreamID,
		Kind: d.Kind, Outlet: d.Outlet, CreatedAt: d.CreatedAt,
	}
}

type componentDependenciesInput struct {
	Name string `path:"name" doc:"Name of the component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"codec-1"`
}

type createComponentDependencyInput struct {
	Name string `path:"name" doc:"Name of the dependent component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"codec-1"`
	Body struct {
		Upstream string `json:"upstream" minLength:"1" doc:"Name or uuid of the component this one depends on, or a dotted address" example:"pdu-1"`
		Kind     string `json:"kind" enum:"power,host,path" doc:"How it depends: power, host or path" example:"power"`
		Outlet   string `json:"outlet,omitempty" maxLength:"64" doc:"The upstream's outlet that feeds this component: required on a power edge, refused on any other" example:"3"`
	}
}

type deleteComponentDependencyInput struct {
	Name     string `path:"name" doc:"Name of the dependent component, or a dotted address (e.g. boi.17c.415a.$comp.display-1)" example:"codec-1"`
	Kind     string `path:"kind" enum:"power,host,path" doc:"The edge's kind" example:"power"`
	Upstream string `path:"upstream" doc:"Name or uuid of the upstream component, or a dotted address" example:"pdu-1"`
}

type listComponentDependenciesOutput struct {
	Body struct {
		Component  string                    `json:"component" doc:"The component, as addressed"`
		Upstreams  []componentDependencyBody `json:"upstreams" doc:"What this component depends on, ordered by kind then upstream name"`
		Dependents []componentDependencyBody `json:"dependents" doc:"What depends on this component, ordered by kind then dependent name"`
		Hidden     int                       `json:"hidden" doc:"How many more edges this component has whose other end is outside the caller's component:read: counted, never named"`
	}
}

type componentDependencyOutput struct {
	Body componentDependencyBody
}

// registerComponentDependencyRoutes wires a component's dependency edges: the
// list both ways, the create and the delete by kind and upstream. An edge is
// a fact about the dependent, so a write costs component:update on it and a
// read on the upstream.
func registerComponentDependencyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-dependencies",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/dependencies",
		Summary:     "List what a component depends on and what depends on it",
		Description: "This component's dependency edges both ways: its upstreams (what it depends on) and its dependents (what depends on it), each of kind power (fed from an outlet of the upstream), host (hosted by it) or path (reached through it). A disruptive command follows host edges down from its target and a power command follows the power edges from its outlet, so these edges are what a reboot or an outlet cycle holds. Containment (a component's parent) is a different fact and never appears here. An edge whose other end is outside the caller's component:read is counted in hidden, never named. Gated by component:read; a component outside it is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentDependenciesInput) (*listComponentDependenciesOutput, error) {
		l, err := gw.ListComponentDependencies(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentDependencyErr(err)
		}
		out := &listComponentDependenciesOutput{}
		out.Body.Component = in.Name
		out.Body.Upstreams = make([]componentDependencyBody, 0, len(l.Upstreams))
		for _, d := range l.Upstreams {
			out.Body.Upstreams = append(out.Body.Upstreams, toComponentDependencyBody(d))
		}
		out.Body.Dependents = make([]componentDependencyBody, 0, len(l.Dependents))
		for _, d := range l.Dependents {
			out.Body.Dependents = append(out.Body.Dependents, toComponentDependencyBody(d))
		}
		out.Body.Hidden = l.Hidden
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-component-dependency",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/components/{name}/dependencies",
		DefaultStatus: http.StatusCreated,
		Summary:       "Record that a component depends on another",
		Description:   "Records that this component depends on the upstream: power (fed from the named outlet of the upstream; the outlet is required on this kind and refused on the others), host (hosted by the upstream, so a reboot of the upstream holds this component's endpoints too) or path (reached through the upstream; recorded, and not yet followed by a hold). A component never depends on itself, one edge of a kind joins a pair once (a 409 for the second), and an edge whose upstream already depends on this component through edges of the same kind would close a cycle and is a 422. The edge is a fact about the dependent: gated by component:update on it (a dependent the caller reads but cannot write is a 403), with the upstream resolved in the caller's component:read (outside it, a non-disclosing 404).",
	}, "component", "update"), func(ctx context.Context, in *createComponentDependencyInput) (*componentDependencyOutput, error) {
		d, err := gw.CreateComponentDependency(ctx, actorID(ctx), in.Name, storage.ComponentDependencySpec{
			Upstream: in.Body.Upstream, Kind: in.Body.Kind, Outlet: in.Body.Outlet,
		}, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update"))
		if err != nil {
			return nil, mapComponentDependencyErr(err)
		}
		return &componentDependencyOutput{Body: toComponentDependencyBody(*d)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-component-dependency",
		Extensions:    mcpExposed(),
		Method:        http.MethodDelete,
		Path:          "/components/{name}/dependencies/{kind}/{upstream}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove a component's dependency on another",
		Description:   "Removes this component's edge of the kind to the upstream. Gated by component:update on the dependent (read but not writable, a 403). An upstream outside the caller's component:read is a 404, as an edge that does not exist is: the list does not name it either.",
	}, "component", "update"), func(ctx context.Context, in *deleteComponentDependencyInput) (*struct{}, error) {
		if err := gw.DeleteComponentDependency(ctx, actorID(ctx), in.Name, in.Kind, in.Upstream,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapComponentDependencyErr(err)
		}
		return nil, nil
	})
}

// mapComponentDependencyErr translates the dependency sentinels; a refusal of
// the edge's shape keeps its own reason so the 422 says what to change.
func mapComponentDependencyErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrDependencyNotFound):
		return huma.Error404NotFound("no dependency of this kind on this upstream")
	case errors.Is(err, storage.ErrDependencyUpstreamNotFound):
		return huma.Error404NotFound("upstream component not found")
	case errors.Is(err, storage.ErrDependencyInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: "))
	case errors.Is(err, storage.ErrDependencyCycle):
		return huma.Error422UnprocessableEntity("the upstream already depends on this component through edges of this kind, so this edge would close a cycle")
	case errors.Is(err, storage.ErrDependencyExists):
		return huma.Error409Conflict("this component already depends on the upstream by this kind")
	case errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden("this component is outside the component:update scope your grants resolve to; a dependency is a fact about the dependent, so it costs component:update on it. Ask for a component:update grant covering it.")
	}
	return mapComponentErr(err)
}
