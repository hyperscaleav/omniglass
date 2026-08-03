package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/views"
)

// The views read side: the directory and the run route over the default view
// registry (internal/views). Both are gated view:read; a run additionally
// enforces the view's DECLARED permission in the handler (the handler-tier
// pattern), and every query executes in the Gateway's scoped mode with the
// caller's visible_set for the view's subject resource, so a view can never
// widen what its caller could read row by row.

// viewParamBody publishes one declared parameter in the directory.
type viewParamBody struct {
	Name     string `json:"name" doc:"The parameter name, the key of a param binding"`
	Type     string `json:"type" doc:"The value type: string, int, duration, or time"`
	Required bool   `json:"required" doc:"Whether a run without this parameter is a 400"`
	Doc      string `json:"doc,omitempty" doc:"What the parameter selects"`
}

// viewDescriptorBody is one directory entry: the whole client-facing contract
// of a view (name, params, columns, field-mapping, and the permission a run
// requires on top of view:read).
type viewDescriptorBody struct {
	Name         string            `json:"name" doc:"The view's addressable name"`
	Summary      string            `json:"summary" doc:"What the view answers"`
	Permission   string            `json:"permission" doc:"The declared permission a run requires on top of view:read"`
	Params       []viewParamBody   `json:"params" doc:"The declared run-time parameters"`
	Columns      []views.Column    `json:"columns" doc:"The result columns, in cell order"`
	FieldMapping map[string]string `json:"field_mapping" doc:"Renderer role to column name (value, label, time, series)"`
}

type viewsListOutput struct {
	Body struct {
		Views []viewDescriptorBody `json:"views" doc:"The default views, in name order"`
	}
}

type runViewInput struct {
	Name string `path:"name" doc:"The view name"`
	// explode: the pairs arrive as repeated param= keys (param=a=1&param=b=2),
	// the form the generated CLI and client emit; a pair's value may itself
	// contain '=' or ',' unharmed.
	Param     []string `query:"param,explode" doc:"A name=value binding for a declared view parameter; repeat for several"`
	PageToken string   `query:"page_token" doc:"The cursor from a previous page's next_page_token"`
}

type runViewOutput struct {
	Body views.ViewResult
}

// registerViewRoutes wires the view directory and the run route over the
// default registry. The registry is validated at construction; a misdeclared
// default view is a boot failure, never a run-time surprise.
func registerViewRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	reg, err := views.NewRegistry(views.Defaults()...)
	if err != nil {
		// Registration runs once at boot, before the first request; a default
		// view that fails validation must stop the process, not limp.
		panic(err)
	}

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-views",
		Method:      http.MethodGet,
		Path:        "/views",
		Summary:     "List the view directory",
		Description: "Lists every default view with its whole client contract: params, columns, field-mapping, and the permission a run requires. Gated by view:read.",
	}, "view", "read"), func(ctx context.Context, _ *struct{}) (*viewsListOutput, error) {
		out := &viewsListOutput{}
		defs := reg.All()
		out.Body.Views = make([]viewDescriptorBody, 0, len(defs))
		for _, d := range defs {
			params := make([]viewParamBody, 0, len(d.Params))
			for _, p := range d.Params {
				params = append(params, viewParamBody{Name: p.Name, Type: p.Type, Required: p.Required, Doc: p.Doc})
			}
			out.Body.Views = append(out.Body.Views, viewDescriptorBody{
				Name:         d.Name,
				Summary:      d.Summary,
				Permission:   d.PermissionString(),
				Params:       params,
				Columns:      d.Columns,
				FieldMapping: d.FieldMapping,
			})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "run-view",
		Method:      http.MethodGet,
		Path:        "/views/{name}:run",
		Summary:     "Run a view",
		Description: "Runs a named view with its typed params bound from repeated param=name=value pairs, returning the uniform ViewResult. Gated by view:read plus the view's declared permission, enforced here; every query runs in the Gateway's scoped mode, so the rows are bounded by the caller's visible set.",
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
	}, "view", "read"), func(ctx context.Context, in *runViewInput) (*runViewOutput, error) {
		def, ok := reg.Get(in.Name)
		if !ok {
			return nil, huma.Error404NotFound("view not found")
		}
		// The handler-tier gate: view:read admits the surface, the view's
		// declared permission decides this view.
		perms, havePerms := permsFrom(ctx)
		if !havePerms || !perms.Allows(def.Permission...) {
			return nil, huma.Error403Forbidden("running " + def.Name + " requires " + def.PermissionString())
		}
		params, err := views.BindParams(def.Params, in.Param)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		rows, next, err := def.Run(ctx, gw, a.scopeFor(ctx, def.ScopeResource, "read"), params, in.PageToken)
		if err != nil {
			return nil, huma.Error500InternalServerError("run view")
		}
		if rows == nil {
			rows = [][]any{}
		}
		out := &runViewOutput{}
		out.Body = views.ViewResult{Columns: def.Columns, Rows: rows, NextPageToken: next}
		return out, nil
	})
}
