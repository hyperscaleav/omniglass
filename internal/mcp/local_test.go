package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// failingPolicyAPI admits the good token but will not answer the agent policy.
type failingPolicyAPI struct{ fakeAPI }

func (f *failingPolicyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/auth/me/agent-policy" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	f.fakeAPI.ServeHTTP(w, r)
}

// channelAPI records the audit channel each dispatched route request carried.
type channelAPI struct {
	fakeAPI
	channel storage.AuditChannel
}

func (c *channelAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
		c.channel = storage.AuditChannelOf(r.Context())
	}
	c.fakeAPI.ServeHTTP(w, r)
}

// The in-process tools are the same catalog the MCP server serves, marked
// with the caller's agent policy, and a call is the same API request with the
// token's Authorization alone, on the MCP channel, so the API settles who it
// is and how the audit row reads exactly as it does for a remote agent.
func TestLocalToolsAreTheCatalogCalledInProcess(t *testing.T) {
	t.Parallel()
	api := &channelAPI{}
	l := NewLocal(api, "Bearer "+goodToken)
	ctx := context.Background()

	tools, err := l.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != len(catalog) {
		t.Fatalf("listed %d tools, want the catalog's %d", len(tools), len(catalog))
	}
	var marked bool
	for _, tool := range tools {
		if tool.Name == "list-components" {
			m, _ := tool.Meta[agentPolicyMeta].(map[string]string)
			marked = m["policy"] == "always"
		}
	}
	if !marked {
		t.Error("list-components is not marked with the caller's always policy")
	}

	res, err := l.Call(ctx, "get-component", map[string]any{"name": "amp-9"})
	if err != nil || res.IsError {
		t.Fatalf("call = %+v, %v", res, err)
	}
	req := api.last()
	if req == nil || !strings.HasSuffix(req.URL.Path, "/components/amp-9") {
		t.Fatalf("dispatched %v, want the component's route", req)
	}
	if got := api.channel; got != storage.AuditChannelMCP {
		t.Errorf("channel = %q, want mcp (the API settles omnibot from the token)", got)
	}
	if req.Header.Get("Cookie") != "" {
		t.Error("a cookie rode along with the call")
	}

	if _, err := l.Call(ctx, "no-such-tool", nil); err == nil {
		t.Error("a tool the catalog lacks was called")
	}
}

// Listing fails closed: a policy the API will not answer is an error, never
// an unmarked list a never tool could slip through.
func TestLocalListFailsClosedWithoutThePolicy(t *testing.T) {
	t.Parallel()
	if _, err := NewLocal(&failingPolicyAPI{}, "Bearer "+goodToken).List(context.Background()); err == nil {
		t.Fatal("listed the tools without their agent policy")
	}
}
