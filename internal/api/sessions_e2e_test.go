package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type sessionRow struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Prefix    string  `json:"prefix"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
	Current   bool    `json:"current"`
}

// TestSelfServiceSessions drives the self-service session surface against the real
// binary (issue #172, slice 1): a signed-in user lists their own sessions with the
// current one flagged (a bounded "session", not a "token"), the response never leaks
// the secret, a user can revoke another of their own sessions so it stops
// authenticating, a credential id that is not theirs is a 404 (no cross-principal
// revoke), and revoking the current session signs it out. Skipped under -short.
func TestSelfServiceSessions(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The owner (a password login) plus its bootstrap bearer, an API "token" with a
	// bounded (90-day) expiry, distinct from a web-login "session". Both are bearers;
	// their purpose is the discriminator, not whether an expiry is set.
	pwHash, err := auth.HashPassword("orange-boat-42x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	tokenExpiry := time.Now().Add(auth.DefaultTokenLifetime)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp, PasswordHash: pwHash, ExpiresAt: &tokenExpiry}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	base := srv.URL

	login := func(user, pw string) string {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"username": user, "password": pw})
		resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("login %s: %v", user, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login %s: want 204, got %d", user, resp.StatusCode)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "og_session" {
				return c.Value
			}
		}
		t.Fatalf("login %s: no session cookie", user)
		return ""
	}
	listSessions := func(cookie string) []sessionRow {
		t.Helper()
		req, _ := http.NewRequest("GET", base+"/api/v1/auth/me/sessions", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list sessions: want 200, got %d", resp.StatusCode)
		}
		var body struct {
			Sessions []sessionRow `json:"sessions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode sessions: %v", err)
		}
		return body.Sessions
	}
	revoke := func(cookie, id string) int {
		t.Helper()
		req, _ := http.NewRequest("POST", base+"/api/v1/auth/me/sessions/"+id+":revoke", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("revoke: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	meStatus := func(cookie string) int {
		t.Helper()
		req, _ := http.NewRequest("GET", base+"/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("me: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	current := func(rows []sessionRow) sessionRow {
		t.Helper()
		var found sessionRow
		n := 0
		for _, r := range rows {
			if r.Current {
				found = r
				n++
			}
		}
		if n != 1 {
			t.Fatalf("want exactly one current session, got %d in %+v", n, rows)
		}
		return found
	}

	// A login session lists the bootstrap token plus this session; exactly the login
	// session is current, and it is a bounded "session" (not a "token").
	cookieA := login("ops", "orange-boat-42x")
	rows := listSessions(cookieA)
	if len(rows) != 2 {
		t.Fatalf("want 2 credentials after one login (bootstrap token + session), got %d: %+v", len(rows), rows)
	}
	cur := current(rows)
	if cur.Kind != "session" || cur.ExpiresAt == nil {
		t.Fatalf("current credential should be a bounded session, got %+v", cur)
	}
	var kinds = map[string]int{}
	var token sessionRow
	for _, r := range rows {
		kinds[r.Kind]++
		if r.Kind == "token" {
			token = r
		}
	}
	if kinds["token"] != 1 || kinds["session"] != 1 {
		t.Fatalf("want one token and one session, got %+v", kinds)
	}
	// The minted API token is now itself time-bounded (no eternal secret): it carries a
	// future expiry, and it is distinguished from the session by its purpose, not by the
	// presence of an expiry (both have one).
	if token.ExpiresAt == nil {
		t.Fatalf("the API token should carry a future expiry, got %+v", token)
	}

	// The list never leaks a secret.
	raw, _ := json.Marshal(rows)
	if bytes.Contains(bytes.ToLower(raw), []byte("secret")) || bytes.Contains(bytes.ToLower(raw), []byte("hash")) {
		t.Fatalf("session list must not leak the secret: %s", raw)
	}

	// A second login is a second session; from that session's own view it is current,
	// which identifies its id. The first session can revoke it, and it stops
	// authenticating, while the first session is untouched.
	cookieB := login("ops", "orange-boat-42x")
	sessionB := current(listSessions(cookieB))
	if meStatus(cookieB) != http.StatusOK {
		t.Fatalf("session B should authenticate before revoke, got %d", meStatus(cookieB))
	}
	if code := revoke(cookieA, sessionB.ID); code != http.StatusNoContent {
		t.Fatalf("revoke another own session: want 204, got %d", code)
	}
	if code := meStatus(cookieB); code != http.StatusUnauthorized {
		t.Fatalf("revoked session should stop authenticating: want 401, got %d", code)
	}
	if code := meStatus(cookieA); code != http.StatusOK {
		t.Fatalf("the revoking session should be untouched: want 200, got %d", code)
	}

	// A malformed id and an unknown-but-valid id are both a non-disclosing 404.
	if code := revoke(cookieA, "not-a-uuid"); code != http.StatusNotFound {
		t.Fatalf("malformed id: want 404, got %d", code)
	}
	if code := revoke(cookieA, "00000000-0000-0000-0000-000000000000"); code != http.StatusNotFound {
		t.Fatalf("unknown id: want 404, got %d", code)
	}

	// Cross-principal: another user's credential id is a 404, and that user is
	// untouched (the revoke is scoped to the caller's own principal).
	alicePw, _ := auth.HashPassword("purple-canyon-7z")
	opsID, err := gw.ResolvePrincipalRef(ctx, "ops")
	if err != nil {
		t.Fatalf("resolve ops: %v", err)
	}
	if _, err := gw.CreateHumanPrincipal(ctx, opsID, storage.HumanSpec{Username: "alice", PasswordHash: alicePw}, scope.Set{All: true}); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	cookieAlice := login("alice", "purple-canyon-7z")
	aliceSession := current(listSessions(cookieAlice))
	if code := revoke(cookieA, aliceSession.ID); code != http.StatusNotFound {
		t.Fatalf("revoking another principal's session: want 404, got %d", code)
	}
	if code := meStatus(cookieAlice); code != http.StatusOK {
		t.Fatalf("alice's session must be untouched by ops's revoke: want 200, got %d", code)
	}

	// Revoking the current session is allowed: it signs this session out.
	sessionA := current(listSessions(cookieA))
	if code := revoke(cookieA, sessionA.ID); code != http.StatusNoContent {
		t.Fatalf("revoking the current session: want 204, got %d", code)
	}
	if code := meStatus(cookieA); code != http.StatusUnauthorized {
		t.Fatalf("after revoking the current session it should sign out: want 401, got %d", code)
	}
}

// TestSelfServiceRevokeAll drives the self-service bulk revoke against the real binary
// (issue #195): a signed-in user ends ALL of their own sessions or ALL their tokens by
// purpose, always keeping the credential making the request (never a self-logout), and
// the two purposes never cross. Skipped under -short.
func TestSelfServiceRevokeAll(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pwHash, err := auth.HashPassword("orange-boat-42x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	future := time.Now().Add(auth.DefaultTokenLifetime)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp, PasswordHash: pwHash, ExpiresAt: &future}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// A second token beyond the bootstrap one, so a token revoke ends more than one.
	_, th, tp, _ := auth.NewBearerToken()
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "ops", SecretHash: th, Prefix: tp, Purpose: "token", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("mint token: ok=%v err=%v", ok, err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	base := srv.URL

	login := func() string {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"username": "ops", "password": "orange-boat-42x"})
		resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if err != nil || resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login: err %v status %v", err, resp.StatusCode)
		}
		defer resp.Body.Close()
		for _, c := range resp.Cookies() {
			if c.Name == "og_session" {
				return c.Value
			}
		}
		t.Fatalf("login: no cookie")
		return ""
	}
	meStatus := func(cookie string) int {
		req, _ := http.NewRequest("GET", base+"/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("me: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	kinds := func(cookie string) (sessions, tokens int) {
		req, _ := http.NewRequest("GET", base+"/api/v1/auth/me/sessions", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Sessions []sessionRow `json:"sessions"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		for _, r := range body.Sessions {
			if r.Kind == "session" {
				sessions++
			} else {
				tokens++
			}
		}
		return
	}
	revokeAll := func(cookie, purpose string) (int, int) {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"purpose": purpose})
		req, _ := http.NewRequest("POST", base+"/api/v1/auth/me/sessions:revokeAll", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("revokeAll: %v", err)
		}
		defer resp.Body.Close()
		revoked := -1
		if resp.StatusCode == http.StatusOK {
			var out struct {
				Revoked int `json:"revoked"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&out)
			revoked = out.Revoked
		}
		return resp.StatusCode, revoked
	}

	cookieA := login()
	cookieB := login()
	// Precondition: ops now has 2 sessions and 2 tokens (bootstrap + minted).
	if s, tk := kinds(cookieA); s != 2 || tk != 2 {
		t.Fatalf("precondition: want 2 sessions + 2 tokens, got %d + %d", s, tk)
	}

	// A bad purpose is a 422.
	if code, _ := revokeAll(cookieA, "banana"); code != http.StatusUnprocessableEntity {
		t.Fatalf("bad purpose: want 422, got %d", code)
	}

	// Revoke all sessions from cookieA: the OTHER session (cookieB) dies, cookieA (the
	// current one) is kept, and both tokens survive. Count is 1 (only the non-current).
	if code, n := revokeAll(cookieA, "session"); code != http.StatusOK || n != 1 {
		t.Fatalf("revoke all sessions: want (200, 1), got (%d, %d)", code, n)
	}
	if meStatus(cookieB) != http.StatusUnauthorized {
		t.Fatalf("the other session should be signed out")
	}
	if meStatus(cookieA) != http.StatusOK {
		t.Fatalf("the current session must be kept")
	}
	if s, tk := kinds(cookieA); s != 1 || tk != 2 {
		t.Fatalf("after session revoke-all: want 1 session (current) + 2 tokens, got %d + %d", s, tk)
	}

	// Revoke all tokens: both tokens go, the current session stays. Count 2.
	if code, n := revokeAll(cookieA, "token"); code != http.StatusOK || n != 2 {
		t.Fatalf("revoke all tokens: want (200, 2), got (%d, %d)", code, n)
	}
	if meStatus(cookieA) != http.StatusOK {
		t.Fatalf("revoking tokens must not sign out the current session")
	}
	if s, tk := kinds(cookieA); s != 1 || tk != 0 {
		t.Fatalf("after token revoke-all: want 1 session + 0 tokens, got %d + %d", s, tk)
	}
}

// TestSelfServiceCreateToken drives the self-service token mint against the real binary
// (issue #204): a signed-in user creates its own API token with a required description
// and optional ttl, gets the secret back once, and that token authenticates and appears
// in the session list as a described 'token'. A blank description or an over-cap ttl is a
// 422. Skipped under -short.
func TestSelfServiceCreateToken(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pwHash, err := auth.HashPassword("orange-boat-42x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	future := time.Now().Add(auth.DefaultTokenLifetime)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp, PasswordHash: pwHash, ExpiresAt: &future}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	base := srv.URL

	// A login session cookie to act as the caller.
	b, _ := json.Marshal(map[string]string{"username": "ops", "password": "orange-boat-42x"})
	resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: err %v status %v", err, resp.StatusCode)
	}
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == "og_session" {
			cookie = c.Value
		}
	}
	resp.Body.Close()

	createToken := func(body map[string]any) (int, string, string) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", base+"/api/v1/auth/me/tokens", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		defer r.Body.Close()
		var out struct{ Token, Prefix, Description, ExpiresAt string }
		if r.StatusCode == http.StatusCreated {
			_ = json.NewDecoder(r.Body).Decode(&out)
		}
		return r.StatusCode, out.Token, out.Description
	}

	// A blank description is a 422 (a token must say what it is for).
	if code, _, _ := createToken(map[string]any{"description": ""}); code != http.StatusUnprocessableEntity {
		t.Fatalf("blank description: want 422, got %d", code)
	}
	// A ttl over the 365-day cap is a 422.
	if code, _, _ := createToken(map[string]any{"description": "x", "ttl_days": 400}); code != http.StatusUnprocessableEntity {
		t.Fatalf("over-cap ttl: want 422, got %d", code)
	}

	// A valid create returns the token once and its description.
	code, token, desc := createToken(map[string]any{"description": "ci pipeline", "ttl_days": 30})
	if code != http.StatusCreated || token == "" || desc != "ci pipeline" {
		t.Fatalf("create: want (201, token, 'ci pipeline'), got (%d, %q, %q)", code, token, desc)
	}

	// The returned token authenticates a request.
	req, _ := http.NewRequest("GET", base+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("new token should authenticate: err %v status %v", err, r.StatusCode)
	}
	r.Body.Close()

	// It shows in the session list as a described token.
	req, _ = http.NewRequest("GET", base+"/api/v1/auth/me/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
	r, _ = http.DefaultClient.Do(req)
	var listed struct {
		Sessions []struct {
			Kind, Description string
		} `json:"sessions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&listed)
	r.Body.Close()
	found := false
	for _, s := range listed.Sessions {
		if s.Kind == "token" && s.Description == "ci pipeline" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the created token should list as a described token: %+v", listed.Sessions)
	}
}
