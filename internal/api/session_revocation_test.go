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

// TestPasswordChangeRevokesSessions proves the force-logout revokes SESSIONS only,
// never tokens (issue #194): an admin reset revokes ALL of the target's sessions (a
// live cookie stops working) but leaves its API tokens; a self-service change-password
// revokes the caller's OTHER sessions, keeps the one making the request, and leaves all
// tokens. A token is its own bearer secret, not derived from the password. Skipped
// under -short.
func TestPasswordChangeRevokesSessions(t *testing.T) {
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

	loginCookie := func(username, password string) string {
		b, _ := json.Marshal(map[string]string{"username": username, "password": password})
		resp, err := http.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("login %s: want 204, got %d", username, resp.StatusCode)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "og_session" {
				return c.Value
			}
		}
		t.Fatalf("login %s: no session cookie", username)
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
	changePassword := func(cookie, current, next string) int {
		b, _ := json.Marshal(map[string]string{"current_password": current, "new_password": next})
		req, _ := http.NewRequest("POST", base+"/api/v1/auth/me:changePassword", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "og_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("change password: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	adminTok := principalWithGrants(t, ctx, dsn, "admin-all", []grant{{role: "admin", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: base}
	var alice struct {
		ID string `json:"id"`
	}
	body := c.do(adminTok, "POST", "/principals", map[string]string{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated)
	_ = json.Unmarshal(body, &alice)

	// Alice also holds an API token (the CLI lane). It must survive every password
	// change: a token is its own bearer secret, not tied to the password.
	tok, hash, prefix, _ := auth.NewBearerToken()
	future := time.Now().Add(90 * 24 * time.Hour)
	if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "alice", SecretHash: hash, Prefix: prefix, Purpose: "token", ExpiresAt: &future}); err != nil || !ok {
		t.Fatalf("mint alice token: ok=%v err=%v", ok, err)
	}
	tokenStatus := func() int {
		req, _ := http.NewRequest("GET", base+"/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("token me: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if tokenStatus() != http.StatusOK {
		t.Fatalf("alice's token should authenticate before any change, got %d", tokenStatus())
	}

	// An admin reset revokes every one of the target's sessions, but not its tokens.
	cookieA := loginCookie("alice", "orange-boat-42x")
	if meStatus(cookieA) != http.StatusOK {
		t.Fatalf("fresh session should work, got %d", meStatus(cookieA))
	}
	c.do(adminTok, "POST", "/principals/"+alice.ID+":resetPassword", map[string]string{"password": "purple-canyon-7"}, http.StatusNoContent)
	if code := meStatus(cookieA); code != http.StatusUnauthorized {
		t.Fatalf("reset should have revoked the session: want 401, got %d", code)
	}
	if code := tokenStatus(); code != http.StatusOK {
		t.Fatalf("reset must NOT revoke the token: want 200, got %d", code)
	}

	// A self change-password revokes the OTHER sessions but keeps the current one, and
	// leaves all tokens.
	cookieB := loginCookie("alice", "purple-canyon-7")
	cookieC := loginCookie("alice", "purple-canyon-7")
	if code := changePassword(cookieC, "purple-canyon-7", "green-river-88x"); code != http.StatusNoContent {
		t.Fatalf("change password: want 204, got %d", code)
	}
	if code := meStatus(cookieB); code != http.StatusUnauthorized {
		t.Fatalf("a change should revoke other sessions: want 401 for cookieB, got %d", code)
	}
	if code := meStatus(cookieC); code != http.StatusOK {
		t.Fatalf("a change should keep the current session: want 200 for cookieC, got %d", code)
	}
	if code := tokenStatus(); code != http.StatusOK {
		t.Fatalf("a change must NOT revoke the token: want 200, got %d", code)
	}
}
