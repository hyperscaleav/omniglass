package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
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
