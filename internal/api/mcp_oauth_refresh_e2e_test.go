package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// An OAuth-connected MCP client holds a short-lived access token and a
// rotating refresh token (#2073): it refreshes past the access token's expiry
// without its person, a spent refresh token presented again ends the grant,
// and revoking the client in the session list ends the refresh chain.
// Skipped under -short.

// oauthTokens is the token endpoint's answer, or its error.
type oauthTokens struct {
	Status       int
	Error        string `json:"error"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *mcpStack) tokenRequest(t *testing.T, form url.Values) oauthTokens {
	t.Helper()
	resp, err := http.PostForm(s.base+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out oauthTokens
	_ = json.Unmarshal(b, &out)
	out.Status = resp.StatusCode
	return out
}

// oauthSignIn registers a client by name, has the owner approve viewer, and
// exchanges the code, returning the client id and what the exchange issued.
func (s *mcpStack) oauthSignIn(t *testing.T, name string) (string, oauthTokens) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"client_name": name, "redirect_uris": []string{oauthRedirect}})
	resp, err := http.Post(s.base+"/oauth/register", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	_ = resp.Body.Close()
	verifier := "a-verifier-long-enough-to-be-valid-under-rfc-7636-" + name
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {oauthRedirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"state": {"st"}, "resource": {s.base + "/mcp"}}
	back, status, err := consentAs(s, s.owner, []string{"viewer"}, true)(s.ctx, s.base+"/oauth/authorize?"+q.Encode())
	if err != nil {
		t.Fatalf("consent = %d %v", status, err)
	}
	u, _ := url.Parse(back)
	tr := s.tokenRequest(t, url.Values{"grant_type": {"authorization_code"}, "code": {u.Query().Get("code")},
		"client_id": {reg.ClientID}, "redirect_uri": {oauthRedirect}, "code_verifier": {verifier}})
	if tr.Status != http.StatusOK || tr.AccessToken == "" {
		t.Fatalf("exchange = %+v", tr)
	}
	return reg.ClientID, tr
}

func (s *mcpStack) refresh(t *testing.T, client, refreshToken string) oauthTokens {
	t.Helper()
	return s.tokenRequest(t, url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {refreshToken}})
}

// expireAccess runs the client's access token past its lifetime, the way an
// hour going by would.
func (s *mcpStack) expireAccess(t *testing.T, client string) {
	t.Helper()
	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(s.ctx)
	if _, err := conn.Exec(s.ctx, `update credential set expires_at = now() - interval '1 second' where oauth_client_id = $1`, client); err != nil {
		t.Fatal(err)
	}
}

func (s *mcpStack) mcpAccepts(t *testing.T, token string) bool {
	t.Helper()
	cs, err := s.connect(t, token, "probe")
	if err != nil {
		return false
	}
	_ = cs.Close()
	return true
}

// oauthSessionID is the id of the session-list row carrying the client.
func (s *mcpStack) oauthSessionID(t *testing.T, name string) string {
	t.Helper()
	var list struct {
		Sessions []struct {
			ID          string `json:"id"`
			OAuthClient string `json:"oauth_client"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/auth/me/sessions", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	for _, ss := range list.Sessions {
		if ss.OAuthClient == name {
			return ss.ID
		}
	}
	return ""
}

func TestOAuthRefreshRotates(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	client, first := s.oauthSignIn(t, "Refresher")
	if first.RefreshToken == "" || first.ExpiresIn != 3600 {
		t.Fatalf("the exchange issued %+v, want a refresh token and an access token good for an hour", first)
	}
	row := s.oauthSessionID(t, "Refresher")

	// An hour on, the access token is refused, and the client stays in its
	// person's session list because its refresh token is still good.
	s.expireAccess(t, client)
	if s.mcpAccepts(t, first.AccessToken) {
		t.Fatal("an expired access token still connects")
	}
	if got := s.oauthSessionID(t, "Refresher"); got != row {
		t.Fatalf("the session row after expiry = %q, want %q still listed", got, row)
	}

	// The client refreshes without its person: a new pair, the old access
	// token dead, the same session row.
	second := s.refresh(t, client, first.RefreshToken)
	if second.Status != http.StatusOK || second.AccessToken == "" || second.RefreshToken == "" || second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh = %+v, want a new access and refresh token", second)
	}
	if !s.mcpAccepts(t, second.AccessToken) {
		t.Fatal("the refreshed access token does not connect")
	}
	if got := s.oauthSessionID(t, "Refresher"); got != row {
		t.Errorf("the session row after refresh = %q, want the same row %q", got, row)
	}

	// Another client cannot spend this client's refresh token.
	if r := s.tokenRequest(t, url.Values{"grant_type": {"refresh_token"}, "client_id": {"00000000-0000-0000-0000-000000000000"},
		"refresh_token": {second.RefreshToken}}); r.Status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Errorf("refresh as another client = %+v, want 400 invalid_grant", r)
	}
	if third := s.refresh(t, client, second.RefreshToken); third.Status != http.StatusOK {
		t.Errorf("the client's own refresh after another client tried = %+v, want 200", third)
	}

	// Each rotation is on the trail as the person's, taken by the client.
	if n := s.auditCount(t, "/audit-log?agent=Refresher&resource=credential&verb=update"); n != 2 {
		t.Errorf("rotation rows naming the client = %d, want 2", n)
	}
}

// The authorization server's metadata advertises the refresh grant, so a
// client knows to ask for one.
func TestOAuthMetadataAdvertisesRefresh(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)
	resp, err := http.Get(s.base + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var meta struct {
		GrantTypes []string `json:"grant_types_supported"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(meta.GrantTypes, "refresh_token") {
		t.Errorf("grant_types_supported = %v, want refresh_token among them", meta.GrantTypes)
	}
}

func (s *mcpStack) auditCount(t *testing.T, path string) int {
	t.Helper()
	var trail struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, path, nil, http.StatusOK), &trail); err != nil {
		t.Fatal(err)
	}
	return len(trail.Events)
}

// A spent refresh token presented again means it leaked: the grant ends, so
// neither the thief's nor the client's tokens work, and the trail says so.
func TestOAuthRefreshReplayEndsTheGrant(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	client, first := s.oauthSignIn(t, "Replayed")
	second := s.refresh(t, client, first.RefreshToken)
	if second.Status != http.StatusOK {
		t.Fatalf("refresh = %+v", second)
	}
	if r := s.refresh(t, client, first.RefreshToken); r.Status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Fatalf("a spent refresh token again = %+v, want 400 invalid_grant", r)
	}
	if r := s.refresh(t, client, second.RefreshToken); r.Status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Errorf("the live refresh token after a replay = %+v, want 400 invalid_grant (the grant ended)", r)
	}
	if s.mcpAccepts(t, second.AccessToken) {
		t.Error("the access token still connects after a replay ended its grant")
	}
	if id := s.oauthSessionID(t, "Replayed"); id != "" {
		t.Errorf("the session list still carries the client after a replay: %s", id)
	}

	if n := s.auditCount(t, "/audit-log?agent=Replayed&resource=credential&verb=revoke"); n != 1 {
		t.Errorf("revoke rows naming the client = %d, want one", n)
	}
}

// Revoking the client in the session list ends the refresh chain.
func TestOAuthRevokeEndsRefresh(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	client, first := s.oauthSignIn(t, "Revoked")
	s.c.do(s.owner, http.MethodPost, "/auth/me/sessions/"+s.oauthSessionID(t, "Revoked")+":revoke", nil, http.StatusNoContent)
	if r := s.refresh(t, client, first.RefreshToken); r.Status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Errorf("refresh after revoke = %+v, want 400 invalid_grant", r)
	}
}

// A disabled person's client cannot refresh, and the refused attempt does not
// spend the token: enabled again, the client refreshes as before.
func TestOAuthRefreshRefusedWhileDisabled(t *testing.T) {
	t.Parallel()
	s := newMCPStack(t)

	client, first := s.oauthSignIn(t, "Paused")
	setActive := func(active bool) {
		conn, err := pgx.Connect(s.ctx, s.dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(s.ctx)
		if _, err := conn.Exec(s.ctx, `update principal set active = $2
			where id = (select principal_id from credential where oauth_client_id = $1)`, client, active); err != nil {
			t.Fatal(err)
		}
	}
	setActive(false)
	if r := s.refresh(t, client, first.RefreshToken); r.Status != http.StatusBadRequest || r.Error != "invalid_grant" {
		t.Errorf("refresh while disabled = %+v, want 400 invalid_grant", r)
	}
	setActive(true)
	if r := s.refresh(t, client, first.RefreshToken); r.Status != http.StatusOK {
		t.Errorf("refresh once enabled again = %+v, want 200 (the refused attempt spent nothing)", r)
	}
}
