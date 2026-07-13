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
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestAdminSessionsAPI drives the admin session surface against the real binary
// (issue #172, slice 2): an admin lists another user's active sessions (the secret
// never leaks and no row is flagged current), revokes one so it stops
// authenticating, and the revoke is audited with the admin as the actor. A
// non-admin operator is 403; a credential id that is not the target's is a 404 (no
// cross-principal revoke); and an owner's session cannot be revoked by a lesser
// admin (the takeover guard, shared with the password reset). Skipped under -short.
func TestAdminSessionsAPI(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	base := srv.URL
	c := &apiClient{t: t, ctx: ctx, base: base}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	ownerID := meID(t, c, ownerTok)
	adminTok := principalWithGrants(t, ctx, dsn, "admin-all", []grant{{role: "admin", scopeKind: "all"}})
	adminID := meID(t, c, adminTok)
	opTok := principalWithGrants(t, ctx, dsn, "op-all", []grant{{role: "operator", scopeKind: "all"}})

	// Two human targets with passwords, created by the owner.
	create := func(username string) string {
		t.Helper()
		var out struct {
			ID string `json:"id"`
		}
		body := c.do(ownerTok, http.MethodPost, "/principals", map[string]string{"username": username, "password": "orange-boat-42x"}, http.StatusCreated)
		if err := json.Unmarshal(body, &out); err != nil || out.ID == "" {
			t.Fatalf("create %s: %v (%s)", username, err, body)
		}
		return out.ID
	}
	aliceID := create("alice")
	bobID := create("bob")

	// login returns a target's session cookie (a real, authenticating web session).
	login := func(user string) string {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"username": user, "password": "orange-boat-42x"})
		resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if err != nil || resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login %s: err %v status %v", user, err, resp.StatusCode)
		}
		defer resp.Body.Close()
		for _, ck := range resp.Cookies() {
			if ck.Name == "og_session" {
				return ck.Value
			}
		}
		t.Fatalf("login %s: no session cookie", user)
		return ""
	}
	meStatus := func(cookie string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("me: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// adminList lists a principal's sessions through the admin surface, asserting 200.
	adminList := func(tok, principalID string) []sessionRow {
		t.Helper()
		var out struct {
			Sessions []sessionRow `json:"sessions"`
		}
		body := c.do(tok, http.MethodGet, "/principals/"+principalID+"/sessions", nil, http.StatusOK)
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode sessions: %v (%s)", err, body)
		}
		return out.Sessions
	}
	adminRevoke := func(tok, principalID, sid string) int {
		t.Helper()
		code, _ := c.send(tok, http.MethodPost, "/principals/"+principalID+"/sessions/"+sid+":revoke", nil)
		return code
	}

	aliceCookie := login("alice")
	bobCookie := login("bob")

	// The admin lists alice's sessions: exactly her one login session, a bounded
	// "session" (not a "token"), and no row is ever flagged current (there is no
	// "this request's own session" when viewing someone else).
	aliceSessions := adminList(adminTok, aliceID)
	if len(aliceSessions) != 1 {
		t.Fatalf("want 1 session for alice, got %d: %+v", len(aliceSessions), aliceSessions)
	}
	if aliceSessions[0].Kind != "session" || aliceSessions[0].ExpiresAt == nil {
		t.Fatalf("alice's login should be a bounded session, got %+v", aliceSessions[0])
	}
	for _, s := range aliceSessions {
		if s.Current {
			t.Fatalf("admin view must never flag a row current, got %+v", s)
		}
	}

	// The list never leaks a secret.
	raw, _ := json.Marshal(aliceSessions)
	if bytes.Contains(bytes.ToLower(raw), []byte("secret")) || bytes.Contains(bytes.ToLower(raw), []byte("hash")) {
		t.Fatalf("admin session list must not leak the secret: %s", raw)
	}

	// An operator lacks principal:revoke-session: it can neither list nor revoke.
	if code, _ := c.send(opTok, http.MethodGet, "/principals/"+aliceID+"/sessions", nil); code != http.StatusForbidden {
		t.Fatalf("operator listing sessions: want 403, got %d", code)
	}
	if code := adminRevoke(opTok, aliceID, aliceSessions[0].ID); code != http.StatusForbidden {
		t.Fatalf("operator revoking a session: want 403, got %d", code)
	}

	// A credential id that is not the target's is a non-disclosing 404, and the owning
	// principal is untouched: bob's session id revoked against alice must not touch bob.
	bobSessions := adminList(adminTok, bobID)
	if len(bobSessions) != 1 {
		t.Fatalf("want 1 session for bob, got %d", len(bobSessions))
	}
	if code := adminRevoke(adminTok, aliceID, bobSessions[0].ID); code != http.StatusNotFound {
		t.Fatalf("revoking bob's session under alice: want 404, got %d", code)
	}
	if code := meStatus(bobCookie); code != http.StatusOK {
		t.Fatalf("bob's session must be untouched by an alice-scoped revoke: want 200, got %d", code)
	}
	// A random valid uuid that belongs to no one is also a 404.
	if code := adminRevoke(adminTok, aliceID, "00000000-0000-0000-0000-000000000000"); code != http.StatusNotFound {
		t.Fatalf("revoking an unknown credential id: want 404, got %d", code)
	}
	// An unknown principal is a 404.
	if code := adminRevoke(adminTok, "11111111-1111-1111-1111-111111111111", aliceSessions[0].ID); code != http.StatusNotFound {
		t.Fatalf("revoking under an unknown principal: want 404, got %d", code)
	}

	// The takeover guard: an owner's session cannot be revoked by a lesser admin, even
	// though the admin can SEE it (the list is read-only, only the revoke is guarded).
	ownerSessions := adminList(adminTok, ownerID)
	if len(ownerSessions) == 0 {
		t.Fatalf("owner should have at least its bootstrap credential")
	}
	if code := adminRevoke(adminTok, ownerID, ownerSessions[0].ID); code != http.StatusForbidden {
		t.Fatalf("revoking an owner's session as a lesser admin: want 403 (takeover guard), got %d", code)
	}

	// The admin revokes alice's session: it stops authenticating at once.
	if code := meStatus(aliceCookie); code != http.StatusOK {
		t.Fatalf("alice's session should authenticate before revoke, got %d", meStatus(aliceCookie))
	}
	if code := adminRevoke(adminTok, aliceID, aliceSessions[0].ID); code != http.StatusNoContent {
		t.Fatalf("admin revoking alice's session: want 204, got %d", code)
	}
	if code := meStatus(aliceCookie); code != http.StatusUnauthorized {
		t.Fatalf("revoked session should stop authenticating: want 401, got %d", code)
	}

	// The revoke is audited with the acting admin as the actor.
	var auditDoc struct {
		Events []auditEvent `json:"events"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/audit-log?verb=revoke_session", nil, http.StatusOK), &auditDoc); err != nil {
		t.Fatalf("decode audit-log: %v", err)
	}
	found := false
	for _, e := range auditDoc.Events {
		if e.Verb == "revoke_session" && e.Actor == adminID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no revoke_session audit event attributed to the admin (%s) in %+v", adminID, auditDoc.Events)
	}
}

// TestAdminRevokeAllAPI drives the admin bulk-revoke surface against the real binary
// (issue #172): an admin ends ALL of a target's sessions or ALL its tokens in one
// action, filtered by purpose so one never touches the other, returning the count.
// It is gated by principal:revoke-session (operator 403), guarded by the shared
// takeover guard (an owner's credentials cannot be bulk-revoked by a lesser admin),
// and audited with the admin as actor. Skipped under -short.
func TestAdminRevokeAllAPI(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	base := srv.URL
	c := &apiClient{t: t, ctx: ctx, base: base}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	ownerID := meID(t, c, ownerTok)
	adminTok := principalWithGrants(t, ctx, dsn, "admin-all", []grant{{role: "admin", scopeKind: "all"}})
	adminID := meID(t, c, adminTok)
	opTok := principalWithGrants(t, ctx, dsn, "op-all", []grant{{role: "operator", scopeKind: "all"}})

	// Alice, created by the owner, given two login sessions (via /auth/login) and two
	// API tokens (minted directly, the CLI lane). She then holds 2 sessions + 2 tokens.
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/principals", map[string]string{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	aliceID := created.ID
	loginCookie := func() string {
		t.Helper()
		b, _ := json.Marshal(map[string]string{"username": "alice", "password": "orange-boat-42x"})
		resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if err != nil || resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login alice: err %v status %v", err, resp.StatusCode)
		}
		defer resp.Body.Close()
		for _, ck := range resp.Cookies() {
			if ck.Name == "og_session" {
				return ck.Value
			}
		}
		t.Fatalf("login alice: no cookie")
		return ""
	}
	cookieA := loginCookie()
	cookieB := loginCookie()
	future := time.Now().Add(90 * 24 * time.Hour)
	for i := 0; i < 2; i++ {
		_, h, pfx, _ := auth.NewBearerToken()
		if ok, err := gw.IssueBearerCredential(ctx, "alice", h, pfx, "token", &future); err != nil || !ok {
			t.Fatalf("mint alice token: ok=%v err=%v", ok, err)
		}
	}
	meStatus := func(cookie string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("me: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	revokeAll := func(tok, principalID, purpose string) (int, int) {
		t.Helper()
		code, body := c.send(tok, http.MethodPost, "/principals/"+principalID+"/sessions:revokeAll", map[string]string{"purpose": purpose})
		revoked := -1
		if code == http.StatusOK {
			var out struct {
				Revoked int `json:"revoked"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("decode revokeAll: %v (%s)", err, body)
			}
			revoked = out.Revoked
		}
		return code, revoked
	}
	listKinds := func(principalID string) (sessions, tokens int) {
		t.Helper()
		var out struct {
			Sessions []sessionRow `json:"sessions"`
		}
		if err := json.Unmarshal(c.do(adminTok, http.MethodGet, "/principals/"+principalID+"/sessions", nil, http.StatusOK), &out); err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, s := range out.Sessions {
			if s.Kind == "session" {
				sessions++
			} else {
				tokens++
			}
		}
		return
	}

	// Precondition: alice holds 2 sessions + 2 tokens, and both cookies authenticate.
	if s, tk := listKinds(aliceID); s != 2 || tk != 2 {
		t.Fatalf("alice precondition: want 2 sessions + 2 tokens, got %d + %d", s, tk)
	}

	// An operator lacks principal:revoke-session: bulk revoke is 403.
	if code, _ := revokeAll(opTok, aliceID, "session"); code != http.StatusForbidden {
		t.Fatalf("operator bulk revoke: want 403, got %d", code)
	}

	// The takeover guard: an owner's credentials cannot be bulk-revoked by a lesser admin.
	if code, _ := revokeAll(adminTok, ownerID, "token"); code != http.StatusForbidden {
		t.Fatalf("bulk-revoking an owner's tokens as a lesser admin: want 403, got %d", code)
	}

	// A bad purpose is a 422 (the enum is enforced), and revokes nothing.
	if code, _ := revokeAll(adminTok, aliceID, "banana"); code != http.StatusUnprocessableEntity {
		t.Fatalf("bulk revoke with a bad purpose: want 422, got %d", code)
	}

	// Revoke all of alice's SESSIONS: count 2, both cookies stop authenticating, her
	// two tokens survive (the purpose filter never crosses).
	if code, n := revokeAll(adminTok, aliceID, "session"); code != http.StatusOK || n != 2 {
		t.Fatalf("revoke all sessions: want (200, 2), got (%d, %d)", code, n)
	}
	if meStatus(cookieA) != http.StatusUnauthorized || meStatus(cookieB) != http.StatusUnauthorized {
		t.Fatalf("both sessions should stop authenticating after revoke-all")
	}
	if s, tk := listKinds(aliceID); s != 0 || tk != 2 {
		t.Fatalf("after session revoke-all: want 0 sessions + 2 tokens, got %d + %d", s, tk)
	}

	// Revoke all of alice's TOKENS: count 2, nothing left.
	if code, n := revokeAll(adminTok, aliceID, "token"); code != http.StatusOK || n != 2 {
		t.Fatalf("revoke all tokens: want (200, 2), got (%d, %d)", code, n)
	}
	if s, tk := listKinds(aliceID); s != 0 || tk != 0 {
		t.Fatalf("after token revoke-all: want 0 + 0, got %d + %d", s, tk)
	}

	// A second bulk revoke is a clean (200, 0), not an error.
	if code, n := revokeAll(adminTok, aliceID, "session"); code != http.StatusOK || n != 0 {
		t.Fatalf("idempotent bulk revoke: want (200, 0), got (%d, %d)", code, n)
	}

	// The bulk revoke is audited with the acting admin as actor.
	var auditDoc struct {
		Events []auditEvent `json:"events"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/audit-log?verb=revoke_session", nil, http.StatusOK), &auditDoc); err != nil {
		t.Fatalf("decode audit-log: %v", err)
	}
	found := false
	for _, e := range auditDoc.Events {
		if e.Verb == "revoke_session" && e.Actor == adminID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no revoke_session audit event attributed to the admin (%s)", adminID)
	}
}
