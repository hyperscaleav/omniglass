package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// An MCP client signs in with OAuth 2.1 (#2048): it finds the authorization
// server from the 401 on /mcp, registers itself, sends its person through the
// console's consent page with PKCE, and gets an agent token carrying only the
// roles the person approved, accepted on the MCP channel alone. These drive
// the SDK's own OAuth client over a real HTTP server on real Postgres.
// Skipped under -short.

const oauthRedirect = "http://127.0.0.1:53682/callback"

// noRedirect is a client that reports a redirect instead of following it, the
// way a browser's address bar shows where the server sent it.
var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// consentAs is the person in the browser: it follows the authorization URL to
// the console's consent page and approves the given roles there with the
// person's session, returning where the console sends the browser back to.
func consentAs(s *mcpStack, session string, roles []string, approved bool) func(context.Context, string) (string, int, error) {
	return func(ctx context.Context, authURL string) (string, int, error) {
		resp, err := noRedirect.Get(authURL)
		if err != nil {
			return "", 0, err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			return "", resp.StatusCode, fmt.Errorf("authorize answered %d, want a redirect to the consent page", resp.StatusCode)
		}
		page, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			return "", 0, err
		}
		if page.Path != "/web/oauth/authorize" {
			return "", 0, fmt.Errorf("authorize sent the browser to %s, want the consent page", page)
		}
		q := page.Query()
		body := map[string]any{
			"redirect_uri":          q.Get("redirect_uri"),
			"code_challenge":        q.Get("code_challenge"),
			"code_challenge_method": q.Get("code_challenge_method"),
			"state":                 q.Get("state"),
			"resource":              q.Get("resource"),
			"roles":                 roles,
			"approved":              approved,
		}
		code, out := s.c.send(session, http.MethodPost, "/auth/oauth-clients/"+q.Get("client_id")+":authorize", body)
		if code != http.StatusOK {
			return "", code, fmt.Errorf("consent answered %d: %s", code, out)
		}
		var res struct {
			RedirectTo string `json:"redirect_to"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return "", code, err
		}
		return res.RedirectTo, code, nil
	}
}

// oauthHandler is the SDK's authorization-code handler registering itself by
// DCR under the given name, its browser step played by consent.
func oauthHandler(t *testing.T, name string, consent func(context.Context, string) (string, int, error)) *auth.AuthorizationCodeHandler {
	t.Helper()
	h, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{ClientName: name, RedirectURIs: []string{oauthRedirect}},
		},
		AuthorizationCodeFetcher: oauthHandlerFetcher(consent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// oauthHandlerFetcher is the SDK's browser step played by consent: it reads the
// code back off the redirect the console sends the browser to.
func oauthHandlerFetcher(consent func(context.Context, string) (string, int, error)) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		back, _, err := consent(ctx, args.URL)
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(back)
		if err != nil {
			return nil, err
		}
		if u.Scheme+"://"+u.Host+u.Path != oauthRedirect {
			return nil, fmt.Errorf("consent sent the browser to %s, want the registered redirect", back)
		}
		q := u.Query()
		if e := q.Get("error"); e != "" {
			return nil, errors.New(e)
		}
		return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
	}
}

func (s *mcpStack) oauthConnect(h *auth.AuthorizationCodeHandler) (*sdk.ClientSession, error) {
	cl := sdk.NewClient(&sdk.Implementation{Name: "oauth-client", Version: "1.0"}, nil)
	return cl.Connect(s.ctx, &sdk.StreamableClientTransport{Endpoint: s.base + "/mcp", OAuthHandler: h, MaxRetries: -1}, nil)
}

func issuedToken(t *testing.T, h *auth.AuthorizationCodeHandler) string {
	t.Helper()
	ts, err := h.TokenSource(context.Background())
	if err != nil || ts == nil {
		t.Fatalf("no token source after sign-in: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatal(err)
	}
	return tok.AccessToken
}

// The whole flow: discovery from the 401, registration, consent, PKCE, the
// token, and tools listed and called as the agent, on the person's behalf.
// Revoking the token in the session list stops the client's next call.
func TestMCPClientSignsInWithOAuth(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	// The 401 names where the resource's metadata lives.
	resp, err := http.Post(s.base+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+s.base+`/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("unauthenticated /mcp = %d %q, want a 401 naming the resource metadata", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	h := oauthHandler(t, "Claude Desktop", consentAs(s, s.owner, []string{"operator"}, true))
	cs, err := s.oauthConnect(h)
	if err != nil {
		t.Fatalf("connect over oauth: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	n := 0
	for _, err := range cs.Tools(s.ctx, nil) {
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		n++
	}
	if n < 40 {
		t.Fatalf("listed %d tools over oauth, want the catalog", n)
	}
	if res, text := callTool(t, cs, "create-component", map[string]any{"name": "oauth-comp", "product": "generic-device"}); res.IsError {
		t.Fatalf("create over oauth: %s", text)
	}

	// The write is the client's, on the owner's behalf.
	var trail struct {
		Events []struct {
			Resource string `json:"resource"`
			Agent    string `json:"agent"`
			Channel  string `json:"channel"`
		} `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/audit-log?agent=Claude%20Desktop&resource=component&verb=create", nil, http.StatusOK), &trail); err != nil {
		t.Fatal(err)
	}
	if len(trail.Events) != 1 || trail.Events[0].Agent != "Claude Desktop" || trail.Events[0].Channel != "mcp" {
		t.Errorf("audit trail = %+v, want one component create by Claude Desktop over mcp", trail.Events)
	}

	// The token is an agent token: MCP only, never the API directly.
	tok := issuedToken(t, h)
	if code, body := s.c.send(tok, http.MethodGet, "/components", nil); code != http.StatusForbidden {
		t.Errorf("the oauth token on the API = %d %s, want 403 (mcp channel only)", code, body)
	}

	// The person sees the client in their session list and revokes it.
	var list struct {
		Sessions []struct {
			ID          string   `json:"id"`
			Agent       string   `json:"agent"`
			AgentRoles  []string `json:"agent_roles"`
			OAuthClient string   `json:"oauth_client"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	var id string
	for _, ss := range list.Sessions {
		if ss.OAuthClient == "Claude Desktop" {
			id = ss.ID
			if ss.Agent != "Claude Desktop" || len(ss.AgentRoles) != 1 || ss.AgentRoles[0] != "operator" {
				t.Errorf("the client's token = %+v, want agent Claude Desktop with operator", ss)
			}
		}
	}
	if id == "" {
		t.Fatalf("no session carries the oauth client: %+v", list.Sessions)
	}
	s.c.do(s.owner, http.MethodPost, "/auth/me/sessions/"+id+":revoke", nil, http.StatusNoContent)

	if _, err := s.connect(t, tok, "revoked"); err == nil {
		t.Error("a revoked oauth token still connects")
	}
}

// A person approves only roles they hold: the consent refuses a role beyond
// their grants, so no token can carry it, and a denial sends the client back
// with access_denied and no code.
func TestOAuthConsentNeverIssuesARoleThePersonLacks(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	var alice struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, "/users", map[string]string{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated), &alice); err != nil {
		t.Fatal(err)
	}
	s.c.do(s.owner, http.MethodPost, "/users/"+alice.ID+"/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	aliceSession := loginBearer(t, s.c, "alice", "orange-boat-42x")

	var status int
	consent := consentAs(s, aliceSession, []string{"admin"}, true)
	h := oauthHandler(t, "greedy", func(ctx context.Context, u string) (string, int, error) {
		back, code, err := consent(ctx, u)
		status = code
		return back, code, err
	})
	if _, err := s.oauthConnect(h); err == nil {
		t.Fatal("alice approved admin for her client and it connected")
	}
	if status != http.StatusForbidden {
		t.Errorf("consenting to a role alice lacks = %d, want 403", status)
	}

	// Denied: the client is sent back with access_denied.
	if _, err := s.oauthConnect(oauthHandler(t, "denied", consentAs(s, aliceSession, nil, false))); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("a denied consent = %v, want access_denied", err)
	}

	// Approved within her grants: the client reads and cannot write.
	h = oauthHandler(t, "reader", consentAs(s, aliceSession, []string{"viewer"}, true))
	cs, err := s.oauthConnect(h)
	if err != nil {
		t.Fatalf("connect as alice's viewer client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	if res, text := callTool(t, cs, "create-component", map[string]any{"name": "nope", "product": "generic-device"}); !res.IsError {
		t.Errorf("alice's viewer client wrote: %s", text)
	}
}

// The protocol edges an attacker would try: an unregistered redirect, a
// non-loopback http redirect, a wrong PKCE verifier, a replayed code, and the
// grants OAuth 2.1 drops.
func TestOAuthProtocolRefusals(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	register := func(redirect string) (int, string) {
		b, _ := json.Marshal(map[string]any{"client_name": "edge", "redirect_uris": []string{redirect}})
		resp, err := http.Post(s.base+"/oauth/register", "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			ClientID string `json:"client_id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.ClientID
	}
	if code, _ := register("http://evil.example/callback"); code != http.StatusBadRequest {
		t.Errorf("registering a non-loopback http redirect = %d, want 400", code)
	}
	code, client := register(oauthRedirect)
	if code != http.StatusCreated || client == "" {
		t.Fatalf("register = %d %q", code, client)
	}

	verifier := "a-verifier-long-enough-to-be-valid-under-rfc-7636-xyz"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	authQ := url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {oauthRedirect},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st"}, "resource": {s.base + "/mcp"}}

	// A redirect that is not the registered one is never redirected to.
	bad := url.Values{}
	for k, v := range authQ {
		bad[k] = v
	}
	bad.Set("redirect_uri", "http://127.0.0.1:53682/other")
	resp, err := noRedirect.Get(s.base + "/oauth/authorize?" + bad.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("authorize with an unregistered redirect = %d (Location %q), want 400", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Implicit flow is not offered.
	implicit := url.Values{}
	for k, v := range authQ {
		implicit[k] = v
	}
	implicit.Set("response_type", "token")
	resp, err = noRedirect.Get(s.base + "/oauth/authorize?" + implicit.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if loc, _ := url.Parse(resp.Header.Get("Location")); resp.StatusCode != http.StatusFound || loc.Query().Get("error") != "unsupported_response_type" {
		t.Errorf("implicit authorize = %d %q, want a redirect with unsupported_response_type", resp.StatusCode, resp.Header.Get("Location"))
	}

	back, status, err := consentAs(s, s.owner, []string{"viewer"}, true)(s.ctx, s.base+"/oauth/authorize?"+authQ.Encode())
	if err != nil {
		t.Fatalf("consent = %d %v", status, err)
	}
	u, _ := url.Parse(back)
	authCode := u.Query().Get("code")
	if authCode == "" || u.Query().Get("state") != "st" || u.Query().Get("iss") != s.base {
		t.Fatalf("consent sent the browser back to %s, want a code, the state and the issuer", back)
	}

	token := func(form url.Values) (int, string) {
		resp, err := http.PostForm(s.base+"/oauth/token", form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out struct {
			Error       string `json:"error"`
			AccessToken string `json:"access_token"`
		}
		_ = json.Unmarshal(b, &out)
		if out.Error != "" {
			return resp.StatusCode, out.Error
		}
		return resp.StatusCode, out.AccessToken
	}
	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {authCode}, "client_id": {client},
		"redirect_uri": {oauthRedirect}, "code_verifier": {verifier + "-wrong"}, "resource": {s.base + "/mcp"}}

	if code, e := token(url.Values{"grant_type": {"password"}, "username": {"owner"}, "password": {"x"}, "client_id": {client}}); code != http.StatusBadRequest || e != "unsupported_grant_type" {
		t.Errorf("password grant = %d %s, want 400 unsupported_grant_type", code, e)
	}
	if code, e := token(exchange); code != http.StatusBadRequest || e != "invalid_grant" {
		t.Errorf("a wrong verifier = %d %s, want 400 invalid_grant", code, e)
	}
	// A failed exchange spends the code: a code is tried once.
	exchange.Set("code_verifier", verifier)
	if code, e := token(exchange); code != http.StatusBadRequest || e != "invalid_grant" {
		t.Errorf("the code after a failed exchange = %d %s, want 400 invalid_grant", code, e)
	}
}
