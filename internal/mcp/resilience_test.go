package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// panickyAPI admits the good token and panics on any route it is asked to
// serve, the shape of a latent bug in a route behind a tool.
type panickyAPI struct{ fakeAPI }

func (p *panickyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/auth/me" {
		p.fakeAPI.ServeHTTP(w, r)
		return
	}
	panic("a route bug")
}

// A route that panics behind a tool is that call's 500, as net/http makes it
// for a direct call, never a crash of the server that hosts the API and the
// bus: the tool runs off the HTTP goroutine, where nothing else recovers.
func TestAToolWhoseRoutePanicsIsAToolErrorNotAnOutage(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&panickyAPI{}, "test"))
	t.Cleanup(srv.Close)
	cs, err := Dial(context.Background(), srv.URL+"/mcp", goodToken, &sdk.Implementation{Name: "panic-check", Version: "1"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	for i := 0; i < 2; i++ {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "list-components"})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		text := res.Content[0].(*sdk.TextContent).Text
		if !res.IsError || !strings.Contains(text, "500") {
			t.Errorf("call %d = %v %q, want a 500 tool error", i, res.IsError, text)
		}
		if strings.Contains(text, "a route bug") {
			t.Errorf("the panic value leaked into the result: %q", text)
		}
	}
}

// swappable serves whatever handler it currently holds, so a test can drop
// every MCP session at once, as a server restart or the idle timeout does.
type swappable struct{ h atomic.Value }

func (s *swappable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.h.Load().(http.Handler).ServeHTTP(w, r)
}

// The relay outlives the server's session: an agent that launched `omniglass
// mcp` and sat idle past the session timeout, or across a server restart, gets
// its next call answered over a fresh session rather than an error until it
// restarts.
func TestTheRelayReconnectsWhenItsSessionIsGone(t *testing.T) {
	api := &fakeAPI{}
	sw := &swappable{}
	sw.h.Store(NewHandler(api, "test"))
	srv := httptest.NewServer(sw)
	t.Cleanup(srv.Close)
	cs := stdioClient(t, srv.URL+"/mcp", goodToken)

	call := func(i int) {
		t.Helper()
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get-component", Arguments: map[string]any{"name": "amp-9"}})
		if err != nil || res.IsError {
			t.Fatalf("call %d = %+v, %v", i, res, err)
		}
	}
	call(1)
	sw.h.Store(NewHandler(api, "test")) // every session the relay knew is gone
	call(2)
	call(3)
	if n := len(api.seen); n != 3 {
		t.Errorf("the API saw %d dispatches, want 3 (each call once, none doubled)", n)
	}
}
