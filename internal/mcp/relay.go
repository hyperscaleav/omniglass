package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/redact"
)

// metaKeyClientInfo is the per-request _meta key a client's identity travels
// under (protocol 2026-07-28), the key the server reads the client from.
const metaKeyClientInfo = "io.modelcontextprotocol/clientInfo"

// Relay serves MCP over in and out (stdio, for an agent that launches
// `omniglass mcp` as a local server) by relaying to the server's streamable
// HTTP endpoint at endpoint with token (#1162). There is one MCP implementation,
// the server's: the relay connects first, serves exactly the tools the server
// lists, and forwards each call, with the calling client's declared identity,
// so the server dispatches, authorizes and audits it as it does any MCP call.
// The token is sent only to the endpoint's own origin and never followed across
// a redirect, and it appears in no error the relay returns.
func Relay(ctx context.Context, in io.Reader, out io.Writer, endpoint, token, version string) error {
	remote, err := Dial(ctx, endpoint, token, &sdk.Implementation{Name: "omniglass-mcp", Version: version})
	if err != nil {
		return err
	}

	info := remote.InitializeResult()
	name, ver, instr := "omniglass", version, ""
	if info != nil {
		if info.ServerInfo != nil {
			ver = info.ServerInfo.Version
		}
		instr = info.Instructions
	}
	local := sdk.NewServer(&sdk.Implementation{Name: name, Title: "Omniglass", Version: ver}, &sdk.ServerOptions{Instructions: instr})
	conn := &remoteConn{cs: remote, redial: func(ctx context.Context) (*sdk.ClientSession, error) {
		return Dial(ctx, endpoint, token, &sdk.Implementation{Name: "omniglass-mcp", Version: version})
	}}
	defer conn.close()
	for tool, err := range remote.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("omniglass mcp: list the server's tools: %w", err)
		}
		name := tool.Name
		local.AddTool(tool, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			params := &sdk.CallToolParams{Name: name}
			if req.Params != nil && len(req.Params.Arguments) > 0 {
				params.Arguments = req.Params.Arguments
			}
			if ci := req.ClientInfo(); ci != nil && ci.Name != "" {
				params.Meta = sdk.Meta{metaKeyClientInfo: ci}
			}
			return conn.callTool(ctx, params)
		})
	}
	err = local.Run(ctx, &sdk.IOTransport{Reader: io.NopCloser(in), Writer: nopWriteCloser{out}})
	if err != nil && (errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)) {
		return nil
	}
	return err
}

// remoteConn is the relay's session to the server, renewed when the server has
// let it go: it closes an idle session after its timeout, and a restart drops
// every one. A call the server refused for an unknown session (its 404) was
// never dispatched, so it is sent once more over a fresh session. A closed
// connection renews the session for the next call but is not retried, since a
// connection can close with a call in flight and a write must never run twice.
// Any other error is the caller's.
type remoteConn struct {
	mu     sync.Mutex
	cs     *sdk.ClientSession
	redial func(context.Context) (*sdk.ClientSession, error)
}

func (c *remoteConn) session() *sdk.ClientSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cs
}

func (c *remoteConn) callTool(ctx context.Context, params *sdk.CallToolParams) (*sdk.CallToolResult, error) {
	cs := c.session()
	res, err := cs.CallTool(ctx, params)
	switch {
	case err == nil:
		return res, nil
	case errors.Is(err, sdk.ErrSessionMissing):
		fresh, derr := c.renew(ctx, cs)
		if derr != nil {
			return nil, derr
		}
		return fresh.CallTool(ctx, params)
	case errors.Is(err, sdk.ErrConnectionClosed):
		if _, derr := c.renew(ctx, cs); derr != nil {
			return nil, derr
		}
		return nil, err
	default:
		return nil, err
	}
}

// renew replaces stale with a fresh session, unless another call already did.
func (c *remoteConn) renew(ctx context.Context, stale *sdk.ClientSession) (*sdk.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cs != stale {
		return c.cs, nil
	}
	fresh, err := c.redial(ctx)
	if err != nil {
		return nil, err
	}
	_ = stale.Close()
	c.cs = fresh
	return fresh, nil
}

func (c *remoteConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.cs.Close()
}

// Dial opens an MCP client session to the server's streamable HTTP endpoint
// with token, identifying as impl. The token rides only to the endpoint's own
// origin, and a redirect is never followed, so it cannot be carried elsewhere.
func Dial(ctx context.Context, endpoint, token string, impl *sdk.Implementation) (*sdk.ClientSession, error) {
	u, err := redact.ParseBaseURL(endpoint, "the server URL")
	if err != nil {
		return nil, fmt.Errorf("omniglass mcp: %w", err)
	}
	if token == "" {
		return nil, errors.New("omniglass mcp: no token: set OMNIGLASS_TOKEN (or pass --token)")
	}
	hc := &http.Client{
		Transport: originBearer{scheme: u.Scheme, host: u.Host, token: token, base: http.DefaultTransport},
		// Never follow: a redirect would carry the request (and, were the header
		// re-added, the token) somewhere the operator did not name.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	cl := sdk.NewClient(impl, nil)
	// No standalone stream: the relay forwards calls and their answers, and holds no
	// connection open between them.
	cs, err := cl.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: endpoint, HTTPClient: hc, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("omniglass mcp: connect to %s: %s", redactURL(u), redact.Secrets(err.Error(), token))
	}
	return cs, nil
}

// originBearer adds the token to a request bound for the endpoint's own scheme
// and host, and to nothing else.
type originBearer struct {
	scheme, host, token string
	base                http.RoundTripper
}

func (o originBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme == o.scheme && r.URL.Host == o.host {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+o.token)
	}
	return o.base.RoundTrip(r)
}

// redactURL is a URL as it may be shown: its userinfo dropped.
func redactURL(u *url.URL) string {
	c := *u
	c.User = nil
	return c.String()
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
