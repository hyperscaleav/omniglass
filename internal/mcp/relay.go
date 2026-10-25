package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
	defer remote.Close()

	info := remote.InitializeResult()
	name, ver, instr := "omniglass", version, ""
	if info != nil {
		if info.ServerInfo != nil {
			ver = info.ServerInfo.Version
		}
		instr = info.Instructions
	}
	local := sdk.NewServer(&sdk.Implementation{Name: name, Title: "Omniglass", Version: ver}, &sdk.ServerOptions{Instructions: instr})
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
			return remote.CallTool(ctx, params)
		})
	}
	err = local.Run(ctx, &sdk.IOTransport{Reader: io.NopCloser(in), Writer: nopWriteCloser{out}})
	if err != nil && (errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)) {
		return nil
	}
	return err
}

// Dial opens an MCP client session to the server's streamable HTTP endpoint
// with token, identifying as impl. The token rides only to the endpoint's own
// origin, and a redirect is never followed, so it cannot be carried elsewhere.
func Dial(ctx context.Context, endpoint, token string, impl *sdk.Implementation) (*sdk.ClientSession, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("omniglass mcp: the server URL %q is not an http(s) URL", redactURL(endpoint))
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
		return nil, fmt.Errorf("omniglass mcp: connect to %s: %s", redactURL(endpoint), scrub(err.Error(), token))
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

// redactURL drops any userinfo from a URL before it is shown.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "(unparseable URL)"
	}
	u.User = nil
	return u.String()
}

// scrub removes the token from a message, should any layer below have echoed it.
func scrub(msg, token string) string {
	if token == "" {
		return msg
	}
	return strings.ReplaceAll(msg, token, "[token]")
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
