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

// TestPasswordChangeRevokesSessions proves the force-logout: an admin reset revokes
// ALL of the target's sessions (a live cookie stops working), and a self-service
// change-password revokes the caller's OTHER sessions but keeps the one making the
// request. Skipped under -short.
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

	// An admin reset revokes every one of the target's sessions.
	cookieA := loginCookie("alice", "orange-boat-42x")
	if meStatus(cookieA) != http.StatusOK {
		t.Fatalf("fresh session should work, got %d", meStatus(cookieA))
	}
	c.do(adminTok, "POST", "/principals/"+alice.ID+":resetPassword", map[string]string{"password": "purple-canyon-7"}, http.StatusNoContent)
	if code := meStatus(cookieA); code != http.StatusUnauthorized {
		t.Fatalf("reset should have revoked the session: want 401, got %d", code)
	}

	// A self change-password revokes the OTHER sessions but keeps the current one.
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
}
