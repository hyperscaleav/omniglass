package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/scope"
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

type watchViewInput struct {
	Name string `path:"name" doc:"The view name"`
	// The same binding form as :run; the detector re-runs the view with these
	// exact params, so a watcher and its refetch see one query.
	Param []string `query:"param,explode" doc:"A name=value binding for a declared view parameter; repeat for several"`
}

// Default pacing for the :watch seam. The detector interval bounds how stale a
// live surface can be (a change notifies within one interval); the heartbeat
// keeps idle connections alive through proxies. Tests tighten both through
// WithWatchIntervals.
const (
	defaultWatchDetectorInterval  = 5 * time.Second
	defaultWatchHeartbeatInterval = 15 * time.Second
)

// resolveViewRun is the shared admission path of :run and :watch, in gate
// order: the view must exist (404), the caller must hold the view's DECLARED
// permission on top of the route's view:read stamp (403), and the param pairs
// must bind against the declaration (400). It returns the definition, the
// bound params, and the caller's resolved read scope for the view's subject
// resource, the scope every query (run or detector) executes under.
func resolveViewRun(ctx context.Context, reg *views.Registry, a *authenticator, name string, pairs []string) (views.Definition, map[string]any, scope.Set, error) {
	def, ok := reg.Get(name)
	if !ok {
		return views.Definition{}, nil, scope.Set{}, huma.Error404NotFound("view not found")
	}
	perms, havePerms := permsFrom(ctx)
	if !havePerms || !perms.Allows(def.Permission...) {
		return views.Definition{}, nil, scope.Set{}, huma.Error403Forbidden("running " + def.Name + " requires " + def.PermissionString())
	}
	params, err := views.BindParams(def.Params, pairs)
	if err != nil {
		return views.Definition{}, nil, scope.Set{}, huma.Error400BadRequest(err.Error())
	}
	return def, params, a.scopeFor(ctx, def.ScopeResource, "read"), nil
}

// registerViewRoutes wires the view directory, the run route, and the watch
// seam over the default registry. The registry is validated at construction; a
// misdeclared default view is a boot failure, never a run-time surprise.
func registerViewRoutes(api huma.API, a *authenticator, gw storage.Gateway, o options) {
	reg, err := views.NewRegistry(views.Defaults()...)
	if err != nil {
		// Registration runs once at boot, before the first request; a default
		// view that fails validation must stop the process, not limp.
		panic(err)
	}
	detectorInterval := o.watchDetectorInterval
	if detectorInterval <= 0 {
		detectorInterval = defaultWatchDetectorInterval
	}
	heartbeatInterval := o.watchHeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultWatchHeartbeatInterval
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
		def, params, read, err := resolveViewRun(ctx, reg, a, in.Name, in.Param)
		if err != nil {
			return nil, err
		}
		rows, next, err := def.Run(ctx, gw, read, params, in.PageToken)
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

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "watch-view",
		Method:      http.MethodGet,
		Path:        "/views/{name}:watch",
		Summary:     "Watch a view for changes",
		Description: "Opens an SSE stream with notify-then-refetch semantics: a change event on connect (the baseline, so a reconnecting client always refetches once), a change event whenever the caller's scoped result changes, and heartbeat comments on a quiet stream. No data rides the stream; the client re-runs the view. Gated exactly like :run (view:read plus the view's declared permission), and the change detector re-runs the view under the caller's scope, so a watcher is never notified of out-of-scope changes.",
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The SSE notification stream: change events carrying a result hash, heartbeat comments between them",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {Schema: &huma.Schema{Type: "string", Description: "SSE frames: `event: change` with `data: {\"hash\": \"...\"}`, and `: heartbeat` comments"}},
				},
			},
		},
	}, "view", "read"), func(ctx context.Context, in *watchViewInput) (*huma.StreamResponse, error) {
		def, params, read, err := resolveViewRun(ctx, reg, a, in.Name, in.Param)
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-cache")
			w := hctx.BodyWriter()
			flusher, _ := w.(http.Flusher)
			flush := func() {
				if flusher != nil {
					flusher.Flush()
				}
			}
			// The reconnect hint: EventSource clients retry after this delay,
			// and the baseline event on the fresh stream covers whatever was
			// missed while disconnected.
			fmt.Fprint(w, "retry: 3000\n\n")
			flush()

			connCtx := hctx.Context()
			runOnce := func(c context.Context) (string, error) {
				rows, _, err := def.Run(c, gw, read, params, "")
				if err != nil {
					return "", err
				}
				return views.ResultHash(rows)
			}
			detector := time.NewTicker(detectorInterval)
			defer detector.Stop()
			heartbeat := time.NewTicker(heartbeatInterval)
			defer heartbeat.Stop()

			// The detector loop runs beside the writer so heartbeats keep
			// flowing between re-runs; notifications hand off through the
			// channel and one goroutine owns every write.
			changes := make(chan string, 8)
			watchDone := make(chan error, 1)
			go func() {
				watchDone <- views.Watch(connCtx, detector.C, runOnce, func(h string) error {
					select {
					case changes <- h:
						return nil
					case <-connCtx.Done():
						return connCtx.Err()
					}
				})
			}()
			seq := 0
			for {
				select {
				case <-connCtx.Done():
					return
				case err := <-watchDone:
					if err != nil && connCtx.Err() == nil {
						// The view failed mid-stream: say so generically and
						// close; the client's reconnect gets a fresh baseline.
						fmt.Fprint(w, "event: error\ndata: {\"error\": \"view run failed\"}\n\n")
						flush()
					}
					return
				case h := <-changes:
					seq++
					fmt.Fprintf(w, "event: change\nid: %d\ndata: {\"hash\": %q}\n\n", seq, h)
					flush()
				case <-heartbeat.C:
					fmt.Fprint(w, ": heartbeat\n\n")
					flush()
				}
			}
		}}, nil
	})
}
