package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The MCP server is one more client of the API (#1162): an external MCP client,
// connected to /mcp over streamable HTTP with a person's token, lists the
// generated tools and calls them, and every call is the route itself, with the
// route's gate, the token's scope and an audit row marked as arriving over MCP.
// These drive a real client over a real HTTP server on real Postgres. Skipped
// under -short.

// bearerTransport adds one token to every request, the way an MCP client
// configured with an Authorization header does.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

type mcpStack struct {
	c     *apiClient
	base  string
	owner string
	dsn   string
	ctx   context.Context
}

func newMCPStack(t *testing.T) *mcpStack {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithVersion("test")))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := bootstrapOwnerTok(t, ctx, gw)
	return &mcpStack{c: c, base: srv.URL, owner: owner, dsn: dsn, ctx: ctx}
}

func (s *mcpStack) connect(t *testing.T, token, client string) (*sdk.ClientSession, error) {
	t.Helper()
	hc := &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}}
	cl := sdk.NewClient(&sdk.Implementation{Name: client, Version: "1.0"}, nil)
	return cl.Connect(s.ctx, &sdk.StreamableClientTransport{Endpoint: s.base + "/mcp", HTTPClient: hc, MaxRetries: -1}, nil)
}

func (s *mcpStack) session(t *testing.T, token, client string) *sdk.ClientSession {
	t.Helper()
	cs, err := s.connect(t, token, client)
	if err != nil {
		t.Fatalf("connect as %s: %v", client, err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (*sdk.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return res, text.String()
}

// An external client lists exactly the generated catalog, each tool carrying
// the annotations the rule gave it.
func TestMCPServesTheGeneratedCatalog(t *testing.T) {
	s := newMCPStack(t)
	cs := s.session(t, s.owner, "catalog-check")
	tools := map[string]*sdk.Tool{}
	for tool, err := range cs.Tools(s.ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		tools[tool.Name] = tool
	}
	if len(tools) < 40 {
		t.Fatalf("listed %d tools, want the whole generated catalog (about forty)", len(tools))
	}
	for name, want := range map[string]struct{ readOnly, destructive bool }{
		"list-components":            {true, false},
		"get-component-reachability": {true, false},
		"create-secret":              {false, false},
		"update-endpoint":            {false, true},
		"delete-endpoint":            {false, true},
		"issue-component-command":    {false, true},
	} {
		tool, ok := tools[name]
		if !ok {
			t.Errorf("tool %s is not served", name)
			continue
		}
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint != want.readOnly || a.DestructiveHint == nil || *a.DestructiveHint != want.destructive {
			t.Errorf("%s annotations = %+v, want read-only %v destructive %v", name, a, want.readOnly, want.destructive)
		}
		if a != nil && (a.OpenWorldHint == nil || *a.OpenWorldHint) {
			t.Errorf("%s says open-world, want closed", name)
		}
	}
	if _, ok := tools["delete-component"]; ok {
		t.Error("an unstamped operation is served as a tool")
	}
}

// A read tool answers exactly what the route answers, by name.
func TestMCPToolCallIsTheRoute(t *testing.T) {
	s := newMCPStack(t)
	s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "mcp-probe-1", "product": "generic-device"}, http.StatusCreated)
	cs := s.session(t, s.owner, "route-check")
	res, text := callTool(t, cs, "get-component", map[string]any{"name": "mcp-probe-1"})
	if res.IsError {
		t.Fatalf("get-component errored: %s", text)
	}
	var viaMCP, direct map[string]any
	if err := json.Unmarshal([]byte(text), &viaMCP); err != nil {
		t.Fatalf("tool result is not the route's JSON: %v (%s)", err, text)
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/components/mcp-probe-1", nil, http.StatusOK), &direct); err != nil {
		t.Fatalf("direct read: %v", err)
	}
	if viaMCP["name"] != "mcp-probe-1" || viaMCP["id"] != direct["id"] || viaMCP["product"] != direct["product"] {
		t.Errorf("via MCP %v, direct %v: want the same component", viaMCP, direct)
	}

	// An argument the tool does not take is refused by name, never dropped.
	res, text = callTool(t, cs, "list-endpoints", map[string]any{"component": "mcp-probe-1"})
	if !res.IsError || !strings.Contains(text, "component") {
		t.Errorf("an unknown argument = %v %q, want a tool error naming it", res.IsError, text)
	}
}

// A write through MCP is audited as the user who made it, with the MCP channel
// and the client it declared; the same write made directly carries neither.
func TestMCPWriteIsAuditedAsTheUserViaMCP(t *testing.T) {
	s := newMCPStack(t)
	cs := s.session(t, s.owner, "audit-agent")
	res, text := callTool(t, cs, "create-secret", map[string]any{
		"name": "mcp-audit-key", "secret_type": "api-key", "owner_kind": "platform", "fields": map[string]any{"key": "k-123"},
	})
	if res.IsError {
		t.Fatalf("create-secret errored: %s", text)
	}
	if strings.Contains(text, "k-123") {
		t.Errorf("the write's result echoes the secret value: %s", text)
	}
	s.c.do(s.owner, http.MethodPost, "/secrets", map[string]any{
		"name": "direct-key", "secret_type": "api-key", "owner_kind": "platform", "fields": map[string]any{"key": "k-456"},
	}, http.StatusCreated)

	var out struct {
		Events []struct {
			ActorName string         `json:"actor_name"`
			Channel   string         `json:"channel"`
			Client    string         `json:"client"`
			New       map[string]any `json:"new"`
		} `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?resource=secret&verb=create", nil, http.StatusOK), &out); err != nil {
		t.Fatalf("audit-log: %v", err)
	}
	byName := map[string]int{}
	for i, e := range out.Events {
		if n, _ := e.New["Name"].(string); n != "" {
			byName[n] = i
		}
		if n, _ := e.New["name"].(string); n != "" {
			byName[n] = i
		}
	}
	mi, ok1 := byName["mcp-audit-key"]
	di, ok2 := byName["direct-key"]
	if !ok1 || !ok2 {
		t.Fatalf("secret create rows = %+v, want one for each secret", out.Events)
	}
	if e := out.Events[mi]; e.ActorName != "root" || e.Channel != "mcp" || e.Client != "audit-agent/1.0" {
		t.Errorf("MCP row = %+v, want actor root, channel mcp, client audit-agent/1.0", e)
	}
	if e := out.Events[di]; e.Channel != "" || e.Client != "" {
		t.Errorf("direct row = %+v, want no channel and no client", e)
	}
}

// Authorization through MCP is the route's: a viewer's write is the route's 403
// as a tool error and nothing is written; a scope-limited principal sees only
// its scope; and no token, a wrong token or a session cookie alone gets no MCP
// session at all.
func TestMCPAuthorizationIsTheRoutes(t *testing.T) {
	s := newMCPStack(t)
	viewer := principalWithGrants(t, s.ctx, s.dsn, "mcp-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	cs := s.session(t, viewer, "viewer-agent")
	res, text := callTool(t, cs, "create-secret", map[string]any{
		"name": "viewer-key", "secret_type": "api-key", "owner_kind": "platform", "fields": map[string]any{"key": "v"},
	})
	if !res.IsError || !strings.Contains(text, "403") && !strings.Contains(strings.ToLower(text), "forbidden") {
		t.Errorf("viewer create-secret = %v %q, want the route's 403", res.IsError, text)
	}
	if code, _ := s.c.send(s.owner, http.MethodGet, "/secrets/viewer-key", nil); code == http.StatusOK {
		t.Error("the refused write landed anyway")
	}

	// Scope: a component-scoped viewer lists its one component, not the fleet.
	s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "in-scope-1", "product": "generic-device"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "out-of-scope-1", "product": "generic-device"}, http.StatusCreated)
	inScope := entityID(t, s.c, s.owner, "/components", "in-scope-1")
	scoped := principalWithGrants(t, s.ctx, s.dsn, "mcp-scoped", []grant{{role: "viewer", scopeKind: "component", scopeID: inScope}})
	scs := s.session(t, scoped, "scoped-agent")
	_, list := callTool(t, scs, "list-components", nil)
	if !strings.Contains(list, "in-scope-1") || strings.Contains(list, "out-of-scope-1") {
		t.Errorf("scoped list-components = %s, want only in-scope-1", list)
	}
	res, text = callTool(t, scs, "get-component", map[string]any{"name": "out-of-scope-1"})
	if !res.IsError || !strings.Contains(text, "404") && !strings.Contains(strings.ToLower(text), "not found") {
		t.Errorf("out-of-scope get = %v %q, want the route's non-disclosing 404", res.IsError, text)
	}

	if _, err := s.connect(t, "", "anonymous"); err == nil {
		t.Error("an MCP session opened with no token")
	}
	if _, err := s.connect(t, "ogp_00000000_notarealtoken", "forger"); err == nil {
		t.Error("an MCP session opened with a token the API does not accept")
	}
	req, _ := http.NewRequest(http.MethodPost, s.base+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.AddCookie(&http.Cookie{Name: "og-session", Value: s.owner})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cookie request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a session cookie alone = %d, want 401: the MCP endpoint answers to tokens only", resp.StatusCode)
	}
}

// A token revoked mid-session stops working on the next call; impersonation
// behaves as everywhere (act-as writes as the target and records the real actor,
// view-as writes nothing).
func TestMCPTokenLifecycleAndImpersonation(t *testing.T) {
	s := newMCPStack(t)
	var minted struct {
		Token  string `json:"token"`
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "short-lived agent"}, http.StatusCreated), &minted); err != nil {
		t.Fatalf("mint: %v", err)
	}
	cs, err := s.connect(t, minted.Token, "revoked-agent")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()
	if res, text := callTool(t, cs, "get-auth-me", nil); res.IsError {
		t.Fatalf("get-auth-me before revoke: %s", text)
	}
	var mine struct {
		Sessions []struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK), &mine); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	for _, sess := range mine.Sessions {
		if sess.Prefix == minted.Prefix {
			s.c.do(s.owner, http.MethodPost, "/auth/me/sessions/"+sess.ID+":revoke", nil, http.StatusNoContent)
		}
	}
	if res, err := cs.CallTool(s.ctx, &sdk.CallToolParams{Name: "get-auth-me"}); err == nil && !res.IsError {
		t.Error("a revoked token still drives the MCP session")
	}

	target := principalWithGrants(t, s.ctx, s.dsn, "mcp-target", []grant{{role: "operator", scopeKind: "all"}})
	targetID := meID(t, s.c, target)
	impersonate := func(mode string) string {
		var r struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, "/principals/"+targetID+":impersonate", map[string]any{"mode": mode}, http.StatusCreated), &r); err != nil {
			t.Fatalf("impersonate %s: %v", mode, err)
		}
		return r.Token
	}
	view := s.session(t, impersonate("view_as"), "view-agent")
	if res, _ := callTool(t, view, "create-component", map[string]any{"name": "view-as-write", "product": "generic-device"}); !res.IsError {
		t.Error("a view-as token wrote through MCP")
	}
	act := s.session(t, impersonate("act_as"), "act-agent")
	if res, text := callTool(t, act, "create-component", map[string]any{"name": "act-as-write", "product": "generic-device"}); res.IsError {
		t.Fatalf("act-as create-component: %s", text)
	}
	var out struct {
		Events []struct {
			ActorName     string         `json:"actor_name"`
			RealActorName string         `json:"real_actor_name"`
			Channel       string         `json:"channel"`
			New           map[string]any `json:"new"`
		} `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?resource=component&verb=create", nil, http.StatusOK), &out); err != nil {
		t.Fatalf("audit-log: %v", err)
	}
	found := false
	for _, e := range out.Events {
		if strings.Contains(fmtImage(e.New), "act-as-write") {
			found = true
			if e.ActorName != "mcp-target" || e.RealActorName != "root" || e.Channel != "mcp" {
				t.Errorf("act-as row = %+v, want actor mcp-target, real actor root, channel mcp", e)
			}
		}
	}
	if !found {
		t.Fatalf("no audit row for the act-as write: %+v", out.Events)
	}
}

func fmtImage(img map[string]any) string {
	b, _ := json.Marshal(img)
	return string(b)
}
