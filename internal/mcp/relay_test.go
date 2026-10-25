package mcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const goodToken = "ogp_0000aaaa_relaytesttokenvalue"

// fakeAPI stands in for the omniglass API behind /mcp: it accepts one token on
// GET /auth/me (the endpoint's admission check) and records every request a
// tool dispatches, answering it with a fixed body. It is the dependency behind
// the system under test (the MCP endpoint and the relay), not that system.
type fakeAPI struct {
	mu   sync.Mutex
	seen []*http.Request
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+goodToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/api/v1/auth/me" {
		_, _ = io.WriteString(w, `{"principal":{"id":"p"}}`)
		return
	}
	f.mu.Lock()
	f.seen = append(f.seen, r.Clone(context.Background()))
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"name":"amp-9"}`)
}

func (f *fakeAPI) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		return nil
	}
	return f.seen[len(f.seen)-1]
}

// stdioClient runs the relay on a pair of pipes, the way an agent runs
// `omniglass mcp`, and returns an MCP client session over its stdio.
func stdioClient(t *testing.T, endpoint, token string) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Relay(ctx, inR, outW, endpoint, token, "test") }()
	cl := sdk.NewClient(&sdk.Implementation{Name: "stdio-agent", Version: "9.9"}, nil)
	cs, err := cl.Connect(ctx, &sdk.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect over stdio: %v (relay: %v)", err, firstErr(done))
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		_ = inW.Close()
		_ = outW.Close()
	})
	return cs
}

func firstErr(c chan error) error {
	select {
	case err := <-c:
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}

// Over stdio, the relay serves exactly the server's tools and forwards a call
// to the server, which dispatches it through the API with the relay's token
// and nothing else of the caller's.
func TestRelayServesTheServersToolsOverStdio(t *testing.T) {
	api := &fakeAPI{}
	srv := httptest.NewServer(NewHandler(api, "test"))
	// Closed after the relay (cleanups run last-registered first), since the
	// server waits on the relay's open requests.
	t.Cleanup(srv.Close)
	cs := stdioClient(t, srv.URL+"/mcp", goodToken)

	n := 0
	for _, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		n++
	}
	if n != len(catalog) {
		t.Fatalf("relay listed %d tools, the server serves %d", n, len(catalog))
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "get-component", Arguments: map[string]any{"name": "amp-9"}})
	if err != nil || res.IsError {
		t.Fatalf("call over stdio = %+v, %v", res, err)
	}
	r := api.last()
	if r == nil || r.Method != http.MethodGet || r.URL.Path != "/api/v1/components/amp-9" {
		t.Fatalf("dispatched %+v, want GET /api/v1/components/amp-9", r)
	}
	if r.Header.Get("Authorization") != "Bearer "+goodToken || r.Header.Get("Cookie") != "" {
		t.Errorf("dispatched headers %v, want the token and no cookie", r.Header)
	}
}

// The relay refuses to start without a token the server accepts, and says so
// without the token.
func TestRelayRefusesABadTokenWithoutEchoingIt(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&fakeAPI{}, "test"))
	defer srv.Close()
	const bad = "ogp_ffffffff_notthetokenthatworks"
	err := Relay(context.Background(), strings.NewReader(""), io.Discard, srv.URL+"/mcp", bad, "test")
	if err == nil {
		t.Fatal("the relay started on a token the server refuses")
	}
	if strings.Contains(err.Error(), bad) {
		t.Errorf("the error echoes the token: %v", err)
	}
	if err := Relay(context.Background(), strings.NewReader(""), io.Discard, srv.URL+"/mcp", "", "test"); err == nil || !strings.Contains(err.Error(), "OMNIGLASS_TOKEN") {
		t.Errorf("no token = %v, want an error naming OMNIGLASS_TOKEN", err)
	}
}

// The token goes only to the server the operator named: a redirect is not
// followed, so a server (or a proxy) that answers with one cannot carry the
// token elsewhere.
func TestRelayTokenNeverLeavesTheNamedOrigin(t *testing.T) {
	var mu sync.Mutex
	var leaked []string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		leaked = append(leaked, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer elsewhere.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	if err := Relay(context.Background(), strings.NewReader(""), io.Discard, redirector.URL+"/mcp", goodToken, "test"); err == nil {
		t.Fatal("the relay accepted a redirecting server")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, a := range leaked {
		if strings.Contains(a, goodToken) {
			t.Fatalf("the token reached another origin: %q", a)
		}
	}
	if len(leaked) > 0 {
		t.Errorf("the relay followed a redirect to another origin (%d requests)", len(leaked))
	}

	ob := originBearer{scheme: "https", host: "og.example", token: goodToken, base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Echo": r.Header.Values("Authorization")}}, nil
	})}
	for _, c := range []struct {
		url  string
		want bool
	}{
		{"https://og.example/mcp", true},
		{"http://og.example/mcp", false},
		{"https://og.example:8443/mcp", false},
		{"https://elsewhere.example/mcp", false},
	} {
		req, _ := http.NewRequest(http.MethodPost, c.url, nil)
		resp, err := ob.RoundTrip(req)
		if err != nil {
			t.Fatalf("round trip %s: %v", c.url, err)
		}
		got := len(resp.Header.Values("Echo")) > 0
		if got != c.want {
			t.Errorf("%s carried the token = %v, want %v", c.url, got, c.want)
		}
	}
}

// A server that cannot be reached is an error that names the server and not
// the token.
func TestRelayUnreachableServerDoesNotEchoTheToken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	err = Relay(context.Background(), strings.NewReader(""), io.Discard, "http://"+addr+"/mcp", goodToken, "test")
	if err == nil || strings.Contains(err.Error(), goodToken) {
		t.Errorf("unreachable server = %v, want an error without the token", err)
	}
	if err := Relay(context.Background(), strings.NewReader(""), io.Discard, "http://user:"+goodToken+"@"+addr+"/mcp", goodToken, "test"); err == nil || strings.Contains(err.Error(), goodToken) {
		t.Errorf("a URL carrying a password = %v, want an error that drops it", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
