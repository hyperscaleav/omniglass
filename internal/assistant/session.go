package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/mcp"
	"github.com/hyperscaleav/omniglass/internal/redact"
)

// sessionDescription names omnibot's tokens in the person's session list.
const sessionDescription = "omnibot session"

// Session is omnibot's connection to the MCP server. It holds an agent token
// of its own, minted from the person's for one day and the omnibot channel
// alone, so every call it makes is audited as omnibot's acting for that person
// and is refused anywhere but MCP. The person's own token is used only to mint
// it and, on Close, to revoke it.
type Session struct {
	cs     *sdk.ClientSession
	api    *url.URL
	person string
	prefix string
	hc     *http.Client
}

// Connect mints omnibot's token from personToken against server (the server
// root) and connects to its MCP endpoint with it. Neither token appears in an
// error it returns.
func Connect(ctx context.Context, server, personToken, version string) (*Session, error) {
	u, err := redact.ParseBaseURL(server, "the server URL")
	if err != nil {
		return nil, fmt.Errorf("omnibot: %w", err)
	}
	if personToken == "" {
		return nil, errors.New("omnibot: no token: set OMNIGLASS_TOKEN (or pass --token)")
	}
	s := &Session{
		api:    u.JoinPath("api", "v1"),
		person: personToken,
		hc:     &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	var minted struct {
		Token  string `json:"token"`
		Prefix string `json:"prefix"`
	}
	if err := s.request(ctx, http.MethodPost, "/auth/me/tokens", map[string]any{
		"description": sessionDescription, "agent": "omnibot", "ttl_days": 1, "channels": []string{"omnibot"},
	}, http.StatusCreated, &minted); err != nil {
		return nil, fmt.Errorf("omnibot: mint its token: %w", err)
	}
	s.prefix = minted.Prefix
	endpoint := strings.TrimRight(u.String(), "/") + "/mcp"
	cs, err := mcp.Dial(ctx, endpoint, minted.Token, &sdk.Implementation{Name: "omniglass-omnibot", Version: version})
	if err != nil {
		_ = s.revokeOnTheWayOut(ctx)
		return nil, fmt.Errorf("omnibot: %s", redact.Secrets(err.Error(), minted.Token, personToken))
	}
	s.cs = cs
	return s, nil
}

// List is the tools the server offers omnibot's token.
func (s *Session) List(ctx context.Context) ([]*sdk.Tool, error) {
	var out []*sdk.Tool
	for tool, err := range s.cs.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		out = append(out, tool)
	}
	return out, nil
}

// Call runs one tool on the server.
func (s *Session) Call(ctx context.Context, name string, args map[string]any) (*sdk.CallToolResult, error) {
	params := &sdk.CallToolParams{Name: name}
	if len(args) > 0 {
		params.Arguments = args
	}
	return s.cs.CallTool(ctx, params)
}

// Close disconnects and revokes omnibot's token with the person's, so it
// outlives the session only if the revoke cannot reach the server (it then
// expires within the day). The revoke still runs when ctx is already
// cancelled, as it is on an interrupted run.
func (s *Session) Close(ctx context.Context) error {
	if s.cs != nil {
		_ = s.cs.Close()
		s.cs = nil
	}
	if s.prefix == "" {
		return nil
	}
	err := s.revokeOnTheWayOut(ctx)
	s.prefix = ""
	return err
}

// revokeTimeout bounds the revoke on the way out, so a server that stops
// answering cannot hold an interrupted run open; the token then expires within
// the day.
const revokeTimeout = 10 * time.Second

// revokeOnTheWayOut revokes omnibot's token on a context that keeps ctx's
// values but not its cancellation, so an interrupt that ends the run does not
// also cancel the revoke.
func (s *Session) revokeOnTheWayOut(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
	defer cancel()
	return s.revoke(ctx)
}

// revoke finds omnibot's token among the person's sessions by its prefix and
// revokes it. A token already gone is not an error.
func (s *Session) revoke(ctx context.Context) error {
	var list struct {
		Sessions []struct {
			ID     string `json:"id"`
			Prefix string `json:"prefix"`
		} `json:"sessions"`
	}
	if err := s.request(ctx, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK, &list); err != nil {
		return fmt.Errorf("omnibot: revoke its token: %w", err)
	}
	for _, x := range list.Sessions {
		if x.Prefix == s.prefix {
			if err := s.request(ctx, http.MethodPost, "/auth/me/sessions/"+url.PathEscape(x.ID)+":revoke", nil, http.StatusNoContent, nil); err != nil {
				return fmt.Errorf("omnibot: revoke its token: %w", err)
			}
		}
	}
	return nil
}

// statusError is a REST answer other than the one a request wanted, so a
// caller can tell a missing thing from a failure.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// request is one call to the REST API with the person's token.
func (s *Session) request(ctx context.Context, method, path string, body any, want int, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.api.String()+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.person)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return errors.New(redact.Secrets(err.Error(), s.person))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != want {
		var problem struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &problem)
		msg := problem.Detail
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &statusError{code: resp.StatusCode, msg: fmt.Sprintf("%s %s: %d %s", method, path, resp.StatusCode, redact.Secrets(msg, s.person))}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
