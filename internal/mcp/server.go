// Package mcp is the omniglass MCP server (#1162, ADR-1162): one more generated
// client of the API. Its tools are the operations stamped for MCP in Go,
// generated into catalog_gen.go by cmd/mcpgen from api/openapi.json; a tool call
// runs its operation through the server's own HTTP handler with the caller's
// bearer token, so the route's gate, the Storage Gateway's scope and the
// same-transaction audit row are exactly a direct call's, the row marked with
// the MCP channel. It is served over streamable HTTP at /mcp; `omniglass mcp`
// relays stdio to it (relay.go).
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// instructions is what the server tells an agent on connect: how the tools
// behave, so it uses them the way the API means them to be used.
const instructions = `Omniglass is the observability and control plane for an AV and IT fleet: places, systems, components, the edge nodes that reach them, and the endpoints and drivers that collect from them. Every tool is one API operation, run with your token: you see and change exactly what your permissions and scope allow, and every change is audited as yours. Address a resource by its name (or its dotted address); an id works too. A tool that is not read-only changes the fleet: confirm it with your user before calling it. An error is the route's own problem+json, and its detail says what to fix.`

// sessionTimeout closes an MCP session no request has touched for this long.
const sessionTimeout = 30 * time.Minute

// NewHandler returns the /mcp handler: the catalog served over streamable HTTP,
// every request authenticated by its bearer token against api (the server's own
// HTTP handler, which every tool call is dispatched through). version is the
// server's build version, reported to clients.
func NewHandler(api http.Handler, version string) http.Handler {
	srv := newServer(api, version)
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, &sdk.StreamableHTTPOptions{
		SessionTimeout: sessionTimeout,
	})
	return requireBearer(api, http.NewCrossOriginProtection().Handler(h))
}

// newServer builds the MCP server over the catalog.
func newServer(api http.Handler, version string) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "omniglass", Title: "Omniglass", Version: version}, &sdk.ServerOptions{
		Instructions: instructions,
	})
	for _, d := range catalog {
		d := d
		srv.AddTool(toolFor(d), func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return call(ctx, api, d, req), nil
		})
	}
	return srv
}

// toolFor is a catalog entry as the MCP protocol describes a tool. The hints are
// all set explicitly, since the protocol's defaults (destructive, open-world)
// are the cautious opposite of most of the catalog.
func toolFor(d tooldef.Def) *sdk.Tool {
	destructive, openWorld := d.Annotations.Destructive, d.Annotations.OpenWorld
	return &sdk.Tool{
		Name:        d.Name,
		Title:       d.Title,
		Description: d.Description,
		InputSchema: json.RawMessage(d.InputSchema),
		Annotations: &sdk.ToolAnnotations{
			Title:           d.Title,
			ReadOnlyHint:    d.Annotations.ReadOnly,
			DestructiveHint: &destructive,
			IdempotentHint:  d.Annotations.Idempotent,
			OpenWorldHint:   &openWorld,
		},
	}
}

// call runs one tool: the API request built from the arguments, carrying the
// caller's Authorization header and nothing else of theirs (never a cookie),
// dispatched through api on a context stamped with the MCP channel and the
// client the caller declared, so the audit row records how it arrived. The
// response becomes the tool's result; a non-2xx status is a tool error whose
// text is the route's problem+json.
func call(ctx context.Context, api http.Handler, d tooldef.Def, req *sdk.CallToolRequest) *sdk.CallToolResult {
	var raw json.RawMessage
	if req.Params != nil {
		raw = req.Params.Arguments
	}
	dctx, release := detached(ctx)
	defer release()
	httpReq, err := buildRequest(dctx, d, raw)
	if err != nil {
		return toolError(err.Error())
	}
	if req.Extra != nil && req.Extra.Header != nil {
		if a := req.Extra.Header.Get("Authorization"); a != "" {
			httpReq.Header.Set("Authorization", a)
		}
	}
	httpReq = httpReq.WithContext(storage.WithAuditChannel(httpReq.Context(), storage.AuditChannelMCP, clientName(req)))
	rec := newRecorder()
	api.ServeHTTP(rec, httpReq)
	body := bytes.TrimSpace(rec.body.Bytes())
	if rec.status < 200 || rec.status > 299 {
		if len(body) == 0 {
			return toolError(fmt.Sprintf("%d %s", rec.status, http.StatusText(rec.status)))
		}
		return toolError(string(body))
	}
	if len(body) == 0 {
		body = []byte(fmt.Sprintf(`{"status":%d}`, rec.status))
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(body)}}}
}

func toolError(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: text}}}
}

// clientName is the client's self-declared name and version, "name/version",
// or empty when it declared none. It is recorded as asserted (and sanitized by
// the audit writer); nothing is decided on it.
func clientName(req *sdk.CallToolRequest) string {
	info := req.ClientInfo()
	if info == nil || info.Name == "" {
		return ""
	}
	if info.Version == "" {
		return info.Name
	}
	return info.Name + "/" + info.Version
}

// requireBearer admits an MCP request only with a bearer token the API accepts,
// checked by asking the API who the token is (GET /auth/me) with that header
// alone, so the MCP endpoint authenticates exactly as every route does
// (expiry, revocation, impersonation) without a second path. A session cookie is
// never consulted: the endpoint answers to tokens, so a browser's ambient
// credentials cannot drive it.
func requireBearer(api http.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		scheme, tok, ok := strings.Cut(authz, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(tok) == "" {
			unauthorized(w, http.StatusUnauthorized, "the MCP endpoint needs an Authorization: Bearer token")
			return
		}
		pctx, release := detached(r.Context())
		defer release()
		probe, err := http.NewRequestWithContext(pctx, http.MethodGet, apiBase+"/auth/me", nil)
		if err != nil {
			unauthorized(w, http.StatusInternalServerError, "authentication failed")
			return
		}
		probe.Header.Set("Authorization", authz)
		probe.Header.Set("Accept", "application/json")
		rec := newRecorder()
		api.ServeHTTP(rec, probe)
		if rec.status != http.StatusOK {
			status := rec.status
			if status != http.StatusForbidden {
				status = http.StatusUnauthorized
			}
			unauthorized(w, status, "the bearer token was not accepted")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func unauthorized(w http.ResponseWriter, status int, msg string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="omniglass"`)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(status), "status": status, "detail": msg})
}

// detached returns a fresh context for a request dispatched through the API: it
// is cancelled when parent is, but carries none of parent's values. A dispatched
// request must start clean, the way a request off the wire does: the router
// reads an inherited routing context as a sub-route already half matched, and
// nothing ambient from the MCP request may ride into the route. The caller's
// identity reaches the route only as its Authorization header.
func detached(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// recorder is the in-process response a dispatched request writes into.
type recorder struct {
	status int
	header http.Header
	body   bytes.Buffer
}

func newRecorder() *recorder { return &recorder{status: http.StatusOK, header: http.Header{}} }

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func (r *recorder) WriteHeader(status int) { r.status = status }
