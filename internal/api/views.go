package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
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
	FieldMapping map[string]string `json:"field_mapping" doc:"Renderer role to column name (value, label, sublabel, time, series)"`
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
// keeps idle connections alive through proxies; the lifetime bounds one
// connection, so a client re-enters the admission path (permission and scope)
// often enough that a revocation takes effect without a logout. Tests tighten
// all three through WithWatchIntervals and WithWatchLifetime.
const (
	defaultWatchDetectorInterval  = 5 * time.Second
	defaultWatchHeartbeatInterval = 15 * time.Second
	defaultWatchLifetime          = 15 * time.Minute
	// watchWriteTimeout bounds one frame's write, so a client that stops
	// reading cannot pin the writer forever.
	watchWriteTimeout = 10 * time.Second
	// watchRetryHintMillis is the reconnect delay published to EventSource
	// clients in the stream's opening frame.
	watchRetryHintMillis = 3000
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

// mapViewRunErr classifies a run failure. Only the two errors a view declares
// as the caller's fault get a specific status: an out-of-scope or absent
// parameterized target is a non-disclosing 404 (never a 403, which would
// confirm the target exists), and a binding the view rejects at run time is a
// 400. Everything else is a generic 500, so an unclassified failure cannot
// leak its shape.
func mapViewRunErr(err error) error {
	switch {
	case errors.Is(err, views.ErrTargetNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, views.ErrInvalidParam):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError("run view")
	}
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
	lifetime := o.watchLifetime
	if lifetime <= 0 {
		lifetime = defaultWatchLifetime
	}
	shutdown := o.streamShutdown

	// Each view's declared permission is enforced in the handlers below, so it
	// belongs in the permission universe: the roles view reports held-versus-
	// missing against the registry, and the generated spec publishes the set on
	// the routes that enforce it (x-omniglass-view-permissions), so a permission
	// a caller can be refused for is never invisible to either.
	declared := map[string]bool{}
	for _, d := range reg.All() {
		a.declarePermission(d.Permission...)
		declared[d.PermissionString()] = true
	}
	viewPermissions := make([]string, 0, len(declared))
	for p := range declared {
		viewPermissions = append(viewPermissions, p)
	}
	sort.Strings(viewPermissions)
	withViewPermissions := func(op huma.Operation) huma.Operation {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		op.Extensions["x-omniglass-view-permissions"] = viewPermissions
		return op
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

	huma.Register(api, withViewPermissions(a.gated(huma.Operation{
		OperationID: "run-view",
		Method:      http.MethodGet,
		Path:        "/views/{name}:run",
		Summary:     "Run a view",
		Description: "Runs a named view with its typed params bound from repeated param=name=value pairs, returning the uniform ViewResult. Gated by view:read plus the view's declared permission, enforced here; every query runs in the Gateway's scoped mode, so the rows are bounded by the caller's visible set.",
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound},
	}, "view", "read")), func(ctx context.Context, in *runViewInput) (*runViewOutput, error) {
		def, params, read, err := resolveViewRun(ctx, reg, a, in.Name, in.Param)
		if err != nil {
			return nil, err
		}
		rows, next, err := def.Run(ctx, gw, read, params, in.PageToken)
		if err != nil {
			return nil, mapViewRunErr(err)
		}
		if rows == nil {
			rows = [][]any{}
		}
		out := &runViewOutput{}
		out.Body = views.ViewResult{Columns: def.Columns, Rows: rows, NextPageToken: next}
		return out, nil
	})

	huma.Register(api, withViewPermissions(a.gated(huma.Operation{
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
	}, "view", "read")), func(ctx context.Context, in *watchViewInput) (*huma.StreamResponse, error) {
		def, params, read, err := resolveViewRun(ctx, reg, a, in.Name, in.Param)
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-cache")
			// Reverse proxies buffer responses by default, which on an endless
			// stream means the console sees nothing, ever. This is the header
			// nginx and its kin read to stream through untouched.
			hctx.SetHeader("X-Accel-Buffering", "no")
			w := hctx.BodyWriter()

			// A stream that cannot flush delivers nothing until it ends, which
			// for this route is never. Say so and close rather than hanging a
			// client on a silent connection.
			flusher, canFlush := w.(http.Flusher)
			if !canFlush {
				fmt.Fprint(w, "event: error\ndata: {\"error\": \"streaming unavailable\"}\n\n")
				return
			}
			// Per-frame write deadline: a client that stops reading must not pin
			// the writer, its goroutine, and a pool slot until TCP gives up. Not
			// every writer supports deadlines; where it does not, the lifetime
			// cap below is the backstop.
			rw, _ := w.(http.ResponseWriter)
			write := func(format string, args ...any) bool {
				if rw != nil {
					_ = http.NewResponseController(rw).SetWriteDeadline(time.Now().Add(watchWriteTimeout))
				}
				if _, err := fmt.Fprintf(w, format, args...); err != nil {
					return false
				}
				flusher.Flush()
				return true
			}

			// Every stream ends at the lifetime cap, and the client reconnects
			// through the full admission path. That is what keeps a revoked
			// grant or a narrowed scope from outliving one connection: the
			// permission and the visible set are resolved per connection, not
			// once per session.
			streamCtx, endStream := context.WithTimeout(hctx.Context(), lifetime)
			defer endStream()

			// The reconnect hint: EventSource clients retry after this delay,
			// and the baseline event on the fresh stream covers whatever was
			// missed while disconnected.
			if !write("retry: %d\n\n", watchRetryHintMillis) {
				return
			}

			runOnce := func(c context.Context) (string, error) {
				rows, _, err := def.Run(c, gw, read, params, "")
				if err != nil {
					return "", err
				}
				return views.ResultHash(rows), nil
			}
			detector := time.NewTicker(detectorInterval)
			defer detector.Stop()
			heartbeat := time.NewTicker(heartbeatInterval)
			defer heartbeat.Stop()

			// The detector loop runs beside the writer so heartbeats keep
			// flowing between re-runs; notifications hand off through the
			// channel and one goroutine owns every write. The goroutine ends
			// with streamCtx, which every return path cancels.
			changes := make(chan string, 8)
			watchDone := make(chan error, 1)
			go func() {
				watchDone <- views.Watch(streamCtx, detector.C, runOnce, func(h string) error {
					select {
					case changes <- h:
						return nil
					case <-streamCtx.Done():
						return streamCtx.Err()
					}
				})
			}()
			seq := 0
			for {
				select {
				case <-streamCtx.Done():
					return
				case <-shutdown:
					// A graceful shutdown: end the stream so the server can
					// drain. The client reconnects to the next instance.
					return
				case err := <-watchDone:
					if err != nil && streamCtx.Err() == nil {
						// The view failed mid-stream: say so generically and
						// close; the client's reconnect gets a fresh baseline.
						write("event: error\ndata: {\"error\": \"view run failed\"}\n\n")
					}
					return
				case h := <-changes:
					seq++
					if !write("event: change\nid: %d\ndata: {\"hash\": %q}\n\n", seq, h) {
						return
					}
				case <-heartbeat.C:
					if !write(": heartbeat\n\n") {
						return
					}
				}
			}
		}}, nil
	})
}
