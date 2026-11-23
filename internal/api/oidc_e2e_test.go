package api_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth/oidctest"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/sso"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Single sign-on (#2362, ADR-2361) driven as a browser drives it: start at
// Omniglass, follow the redirect to the fake provider (a TLS server on
// loopback, named in the install's egress allow list), follow it back to the
// callback, and read what the browser ends up holding. Skipped under -short.

type ssoStack struct {
	c     *apiClient
	ctx   context.Context
	base  string
	owner string
	dsn   string
	gw    *storage.PG
	idp   *oidctest.Provider
}

func newSSOStack(t *testing.T) *ssoStack {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	idp := oidctest.New(t)
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw, api.WithVersion("test"), api.WithEgressOptions(egress.Options{RootCAs: idp.Roots()})))
	t.Cleanup(srv.Close)
	s := &ssoStack{c: &apiClient{t: t, ctx: ctx, base: srv.URL}, ctx: ctx, base: srv.URL, dsn: dsn, gw: gw, idp: idp}
	s.owner = bootstrapOwnerTok(t, ctx, gw)
	s.c.do(s.owner, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)
	s.c.do(s.owner, http.MethodPost, "/secrets", map[string]any{"name": "sso-client", "secret_type": "oauth2-client", "owner_kind": "platform",
		"fields": map[string]string{"client_id": oidctest.ClientID, "client_secret": oidctest.ClientSecret}}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPatch, "/settings/sso", map[string]any{"issuer": idp.Issuer(), "client_secret": "sso-client"}, http.StatusOK)
	return s
}

// person creates a person with a password and returns their id.
func (s *ssoStack) person(t *testing.T, username string) string {
	t.Helper()
	var u struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, "/users", map[string]string{"username": username, "password": "orange-boat-42x"}, http.StatusCreated), &u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// link links a person to the provider's subject sub, as the owner.
func (s *ssoStack) link(t *testing.T, userID, sub string) {
	t.Helper()
	s.c.do(s.owner, http.MethodPost, "/users/"+userID+"/identity-links", map[string]string{"identifier": s.idp.Issuer() + "|" + sub}, http.StatusCreated)
}

// browser is a cookie-keeping client that follows no redirect by itself and
// trusts the provider's certificate.
type browser struct {
	t  *testing.T
	hc *http.Client
	s  *ssoStack
}

func (s *ssoStack) browser(t *testing.T) *browser {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, s: s, hc: &http.Client{Jar: jar,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{RootCAs: s.idp.Roots(), MinVersion: tls.VersionTLS12}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// get fetches u and returns the status and where it redirects.
func (b *browser) get(u string) (int, string) {
	b.t.Helper()
	resp, err := b.hc.Get(u)
	if err != nil {
		b.t.Fatalf("GET %s: %v", u, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc != "" && strings.HasPrefix(loc, "/") {
		loc = b.s.base + loc
	}
	return resp.StatusCode, loc
}

// toCallback starts a sign-in and follows it to the provider, returning the
// callback URL the provider sends the browser back to.
func (b *browser) toCallback() string {
	b.t.Helper()
	code, idp := b.get(b.s.base + "/auth/oidc/start/default")
	if code != http.StatusFound || !strings.HasPrefix(idp, b.s.idp.Issuer()+"/authorize") {
		b.t.Fatalf("start: %d to %q, want a redirect to the provider", code, idp)
	}
	code, cb := b.get(idp)
	if code != http.StatusFound || !strings.HasPrefix(cb, b.s.base+sso.CallbackPath) {
		b.t.Fatalf("authorize: %d to %q, want a redirect to the callback", code, cb)
	}
	return cb
}

// signIn runs the whole flow and returns where the callback lands.
func (b *browser) signIn() string {
	b.t.Helper()
	_, to := b.get(b.toCallback())
	return to
}

// session is the session cookie the browser holds, empty when none.
func (b *browser) session() string {
	u, _ := url.Parse(b.s.base)
	for _, c := range b.hc.Jar.Cookies(u) {
		if c.Name == "og-session" {
			return c.Value
		}
	}
	return ""
}

// ssoError is the message a refused sign-in lands on the sign-in page with.
func ssoError(t *testing.T, to string) string {
	t.Helper()
	u, err := url.Parse(to)
	if err != nil || !strings.HasSuffix(u.Path, "/web/login") {
		t.Fatalf("landed on %q, want the sign-in page", to)
	}
	return u.Query().Get("sso_error")
}

func (s *ssoStack) username(t *testing.T, tok string) (int, string) {
	t.Helper()
	code, body := s.c.send(tok, http.MethodGet, "/auth/me", nil)
	var me struct {
		Person struct {
			Username string `json:"username"`
		} `json:"person"`
	}
	_ = json.Unmarshal(body, &me)
	return code, me.Person.Username
}

func (s *ssoStack) query(t *testing.T, sql string, args ...any) pgx.Row {
	t.Helper()
	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(s.ctx) })
	return conn.QueryRow(s.ctx, sql, args...)
}

func (s *ssoStack) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(s.ctx) }()
	if _, err := conn.Exec(s.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSSOSignsInALinkedPersonWithAnOrdinarySession(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	var status struct {
		SSOProviders []string `json:"sso_providers"`
	}
	_ = json.Unmarshal(s.c.do("", http.MethodGet, "/auth/status", nil, http.StatusOK), &status)
	if len(status.SSOProviders) != 1 || status.SSOProviders[0] != "default" {
		t.Fatalf("status sso_providers = %v, want [default]", status.SSOProviders)
	}

	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")
	b := s.browser(t)
	if to := b.signIn(); to != s.base+"/web/" {
		t.Fatalf("callback landed on %q, want the console", to)
	}
	if code, who := s.username(t, b.session()); code != http.StatusOK || who != "alice" {
		t.Fatalf("auth/me with the SSO session = %d %q, want alice", code, who)
	}
	var purpose string
	var expires time.Time
	if err := s.query(t, `select purpose, expires_at from credential where user_id = $1 and kind = 'bearer' and via_link is not null`, alice).Scan(&purpose, &expires); err != nil {
		t.Fatalf("the session through the link: %v", err)
	}
	if purpose != "session" || time.Until(expires) < 11*time.Hour {
		t.Fatalf("session = %s until %v, want a 12-hour session", purpose, expires)
	}
	var method string
	if err := s.query(t, `select new->>'method' from audit_log where actor_user_id = $1 and verb = 'login' and resource = 'auth'`, alice).Scan(&method); err != nil || method != "oidc" {
		t.Fatalf("login auth event method = %q (%v), want oidc", method, err)
	}
	// The binding cookie is spent with the state.
	u, _ := url.Parse(s.base + "/auth/oidc/callback")
	for _, c := range b.hc.Jar.Cookies(u) {
		if c.Name == "og-oidc" {
			t.Fatalf("binding cookie still held after the callback")
		}
	}
}

func TestSSORefusesEveryAnswerThatDoesNotVerify(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")

	cases := []struct {
		name  string
		setup func()
	}{
		{"bad signature", s.idp.SignWithForeignKey},
		{"wrong iss", func() { s.idp.Tamper(func(c map[string]any) { c["iss"] = "https://evil.example" }) }},
		{"wrong aud", func() { s.idp.Tamper(func(c map[string]any) { c["aud"] = "someone-else" }) }},
		{"expired", func() {
			s.idp.Tamper(func(c map[string]any) { c["exp"] = time.Now().Add(-10 * time.Minute).Unix() })
		}},
		{"nonce mismatch", func() { s.idp.Tamper(func(c map[string]any) { c["nonce"] = "not-the-sign-ins" }) }},
	}
	for _, tc := range cases {
		b := s.browser(t)
		cb := b.toCallback()
		tc.setup()
		_, to := b.get(cb)
		if msg := ssoError(t, to); !strings.Contains(msg, "did not verify") {
			t.Errorf("%s: sso_error = %q, want the refusal", tc.name, msg)
		}
		if b.session() != "" {
			t.Errorf("%s: a session cookie was set", tc.name)
		}
	}
}

func TestSSORefusesAReplayedStateAForeignBrowserAndAnUnlinkedIdentity(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")

	// Replayed: the callback URL answered once is refused the second time.
	b := s.browser(t)
	cb := b.toCallback()
	if _, to := b.get(cb); to != s.base+"/web/" {
		t.Fatalf("first callback landed on %q", to)
	}
	b2 := s.browser(t)
	_, to := b2.get(cb)
	if msg := ssoError(t, to); !strings.Contains(msg, "expired or was already used") {
		t.Fatalf("replayed state: sso_error = %q", msg)
	}

	// Login CSRF: a callback completed in one browser, presented by another
	// (the victim's, which holds no binding cookie), signs no one in.
	attacker := s.browser(t)
	cb = attacker.toCallback()
	victim := s.browser(t)
	_, to = victim.get(cb)
	if msg := ssoError(t, to); !strings.Contains(msg, "not started in this browser") || victim.session() != "" {
		t.Fatalf("browser with no binding: sso_error = %q, session %q", msg, victim.session())
	}
	// Nor when the victim's browser holds a binding of its own sign-in.
	cb = attacker.toCallback()
	victim.toCallback()
	_, to = victim.get(cb)
	if msg := ssoError(t, to); !strings.Contains(msg, "not started in this browser") || victim.session() != "" {
		t.Fatalf("foreign browser: sso_error = %q, session %q", msg, victim.session())
	}

	// Unlinked: the provider vouches for a subject no person is linked to.
	s.idp.SignIn("mallory-sub", nil)
	b3 := s.browser(t)
	msg := ssoError(t, b3.signIn())
	if !strings.Contains(msg, "not linked") || !strings.Contains(msg, s.idp.Issuer()+"|mallory-sub") || b3.session() != "" {
		t.Fatalf("unlinked: sso_error = %q, want a refusal naming the identity", msg)
	}
}

func TestSSOLinkTakesTheTakeoverGuardAndAnOwnerGrantDropsAForeignLink(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	alice := s.person(t, "alice")
	ident := map[string]string{"identifier": s.idp.Issuer() + "|alice-sub"}
	if code, _ := s.c.send(s.owner, http.MethodPost, "/users/"+alice+"/identity-links", map[string]string{"identifier": "alice@example.com"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("an email as identifier: %d, want 422", code)
	}
	s.c.do(s.owner, http.MethodPost, "/users/"+alice+"/identity-links", ident, http.StatusCreated)
	bob := s.person(t, "bob")
	if code, _ := s.c.send(s.owner, http.MethodPost, "/users/"+bob+"/identity-links", ident); code != http.StatusConflict {
		t.Fatalf("the same identity again: %d, want 409", code)
	}
	var root struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/auth/me", nil, http.StatusOK), &root)
	if code, _ := s.c.send(s.owner, http.MethodPost, "/users/"+root.User.ID+"/identity-links", map[string]string{"identifier": s.idp.Issuer() + "|root-sub"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("linking yourself: %d, want 422", code)
	}
	var links struct {
		Links []struct {
			ID       string `json:"id"`
			LinkedBy string `json:"linked_by"`
			Self     bool   `json:"self"`
		} `json:"links"`
	}
	_ = json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/users/"+alice+"/identity-links", nil, http.StatusOK), &links)
	if len(links.Links) != 1 || links.Links[0].LinkedBy != "root" || links.Links[0].Self {
		t.Fatalf("alice's links = %+v, want one made by root", links.Links)
	}

	// Granting alice owner deletes the link root made for her, audited.
	s.c.do(s.owner, http.MethodPost, "/users/"+alice+"/grants", map[string]string{"role": "owner", "scope_kind": "all"}, http.StatusCreated)
	_ = json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/users/"+alice+"/identity-links", nil, http.StatusOK), &links)
	if len(links.Links) != 0 {
		t.Fatalf("after the owner grant alice still has %d links", len(links.Links))
	}
	var n int
	if err := s.query(t, `select count(*) from audit_log where resource = 'identity_link' and verb = 'unlink'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("unlink audit rows = %d (%v), want 1", n, err)
	}
	// Now an owner, she is linked by no one else.
	if code, _ := s.c.send(s.owner, http.MethodPost, "/users/"+alice+"/identity-links", ident); code != http.StatusForbidden {
		t.Fatalf("linking an owner: %d, want 403", code)
	}
}

func TestSSOSelfLinkConfirmsThePasswordAndBindsToTheSession(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	s.person(t, "alice")
	sess := loginBearer(t, s.c, "alice", "orange-boat-42x")
	if code, _ := s.c.send(sess, http.MethodPost, "/auth/me/identity-links:start", map[string]string{"current_password": "wrong"}); code != http.StatusForbidden {
		t.Fatalf("a wrong password: %d, want 403", code)
	}
	b := s.browser(t)
	base, _ := url.Parse(s.base)
	b.hc.Jar.SetCookies(base, []*http.Cookie{{Name: "og-session", Value: sess, Path: "/"}})
	req, _ := http.NewRequest(http.MethodPost, s.base+"/api/v1/auth/me/identity-links:start", strings.NewReader(`{"current_password":"orange-boat-42x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.hc.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("start self-link: %v %v", err, resp)
	}
	var out struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	_, cb := b.get(out.URL)
	if _, to := b.get(cb); to != s.base+"/web/profile?sso_linked=1" {
		t.Fatalf("self-link landed on %q", to)
	}
	var links struct {
		Links []struct {
			Identifier string `json:"identifier"`
			Self       bool   `json:"self"`
		} `json:"links"`
	}
	_ = json.Unmarshal(s.c.do(sess, http.MethodGet, "/auth/me/identity-links", nil, http.StatusOK), &links)
	if len(links.Links) != 1 || !links.Links[0].Self || links.Links[0].Identifier != s.idp.Issuer()+"|alice-sub" {
		t.Fatalf("self links = %+v", links.Links)
	}
	// Linked, she signs in through the provider.
	if to := s.browser(t).signIn(); to != s.base+"/web/" {
		t.Fatalf("sign-in after self-link landed on %q", to)
	}
}

// mintToken mints an API token with tok, returning the status and the token.
func (s *ssoStack) mintToken(t *testing.T, tok string) (int, string) {
	t.Helper()
	code, body := s.c.send(tok, http.MethodPost, "/auth/me/tokens", map[string]any{"description": "script"})
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &out)
	return code, out.Token
}

func TestSSORecheckRevokesWhatTheProviderRefuses(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")
	b := s.browser(t)
	b.signIn()
	sess := b.session()
	code, tok := s.mintToken(t, sess)
	if code != http.StatusCreated {
		t.Fatalf("mint a token from the SSO session: %d", code)
	}
	sw := sso.NewSweeper(s.gw, egress.Options{RootCAs: s.idp.Roots()}, nil)

	// Accepted: both credentials stand.
	if p, _, err := sw.RunOnce(s.ctx); err != nil || p.Accepted != 1 {
		t.Fatalf("first pass = %+v, %v; want one accepted", p, err)
	}
	if code, _ := s.username(t, tok); code != http.StatusOK {
		t.Fatalf("token after an accepted re-check: %d", code)
	}

	// Refused: the session and the token through the link are revoked.
	s.idp.Revoke("alice-sub")
	if p, _, err := sw.RunOnce(s.ctx); err != nil || p.Revoked != 1 {
		t.Fatalf("pass after removal = %+v, %v; want one revoked", p, err)
	}
	for name, c := range map[string]string{"session": sess, "token": tok} {
		if code, _ := s.username(t, c); code != http.StatusUnauthorized {
			t.Errorf("%s after the refusal: %d, want 401", name, code)
		}
	}
	var revoked int
	if err := s.query(t, `select (new->>'credentials_revoked')::int from audit_log where resource = 'identity_link' and verb = 'revoke'`).Scan(&revoked); err != nil || revoked != 2 {
		t.Fatalf("revoke audit = %d (%v), want 2 credentials", revoked, err)
	}
	if err := s.query(t, `select 1 from audit_log where resource = 'credential' and verb = 'revoke'`).Scan(new(int)); err != nil {
		t.Fatalf("the token's revoke is not audited: %v", err)
	}
}

func TestSSORecheckOutagePastTheGraceRefusesWithoutRevoking(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")
	b := s.browser(t)
	b.signIn()
	sess := b.session()
	sw := sso.NewSweeper(s.gw, egress.Options{RootCAs: s.idp.Roots()}, nil)

	s.idp.Down(true)
	if p, _, err := sw.RunOnce(s.ctx); err != nil || p.Failed != 1 || p.Revoked != 0 {
		t.Fatalf("pass in an outage = %+v, %v; want one failed, none revoked", p, err)
	}
	if code, _ := s.username(t, sess); code != http.StatusOK {
		t.Fatalf("session inside the grace: %d, want 200", code)
	}
	// The grace runs out with no accepted refresh: refused, not revoked.
	s.exec(t, `update credential set recheck_grace_until = now() - interval '1 second' where user_id = $1 and kind = 'oidc'`, alice)
	if code, _ := s.username(t, sess); code != http.StatusUnauthorized {
		t.Fatalf("session past the grace: %d, want 401", code)
	}
	var n int
	_ = s.query(t, `select count(*) from credential where user_id = $1 and kind = 'bearer'`, alice).Scan(&n)
	if n != 1 {
		t.Fatalf("sessions after the outage = %d, want the one kept", n)
	}
	// The provider answers again: the next accepted refresh restores it.
	s.idp.Down(false)
	if p, _, err := sw.RunOnce(s.ctx); err != nil || p.Accepted != 1 {
		t.Fatalf("pass after the outage = %+v, %v; want one accepted", p, err)
	}
	if code, _ := s.username(t, sess); code != http.StatusOK {
		t.Fatalf("session after the outage: %d, want 200", code)
	}
}

func TestSSOWithoutARefreshTokenGivesAShortSessionAndNoAPIToken(t *testing.T) {
	t.Parallel()
	s := newSSOStack(t)
	alice := s.person(t, "alice")
	s.link(t, alice, "alice-sub")
	s.idp.WithholdRefresh()
	b := s.browser(t)
	if to := b.signIn(); to != s.base+"/web/" {
		t.Fatalf("sign-in landed on %q", to)
	}
	var expires time.Time
	if err := s.query(t, `select expires_at from credential where user_id = $1 and kind = 'bearer'`, alice).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if left := time.Until(expires); left > 61*time.Minute || left < 55*time.Minute {
		t.Fatalf("session lasts %v, want the 60-minute grace", left)
	}
	if code, _ := s.mintToken(t, b.session()); code != http.StatusForbidden {
		t.Fatalf("mint a token with no refresh token behind the session: %d, want 403", code)
	}
}
