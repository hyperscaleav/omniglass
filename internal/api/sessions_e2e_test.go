package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	// The owner (a password login) plus its bootstrap bearer, which is a non-expiring
	// token (no expiry), distinct from a login session.
	pwHash, err := auth.HashPassword("orange-boat-42x")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp, PasswordHash: pwHash}); err != nil {
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
	for _, r := range rows {
		kinds[r.Kind]++
	}
	if kinds["token"] != 1 || kinds["session"] != 1 {
		t.Fatalf("want one token and one session, got %+v", kinds)
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
