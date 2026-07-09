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

// TestResetPasswordAPI drives the admin password-reset endpoint: an operator lacks the
// capability (403), a weak/common/username-containing new password is refused (422), an
// admin resets a user's password (204) and the user can then sign in with the new
// password but not the old, and an unknown id is 404. Skipped under -short.
func TestResetPasswordAPI(t *testing.T) {
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

	adminTok := principalWithGrants(t, ctx, dsn, "admin-all", []grant{{role: "admin", scopeKind: "all"}})
	opTok := principalWithGrants(t, ctx, dsn, "op-all", []grant{{role: "operator", scopeKind: "all"}})
	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	ownerID := meID(t, c, ownerTok)

	var created struct {
		ID string `json:"id"`
	}
	body := c.do(adminTok, "POST", "/principals", map[string]string{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated)
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		t.Fatalf("create alice: %v (%s)", err, body)
	}
	path := "/principals/" + created.ID + ":resetPassword"

	// An operator lacks principal:reset-password.
	c.do(opTok, "POST", path, map[string]string{"password": "purple-canyon-7"}, http.StatusForbidden)
	// A common password and a username-containing password are both refused.
	c.do(adminTok, "POST", path, map[string]string{"password": "administrator"}, http.StatusUnprocessableEntity)
	c.do(adminTok, "POST", path, map[string]string{"password": "alice-new-pass9"}, http.StatusUnprocessableEntity)

	// Self is refused: you reset your own password from your profile (with your
	// current password), not from the admin surface (which skips that confirmation).
	adminID := meID(t, c, adminTok)
	c.do(adminTok, "POST", "/principals/"+adminID+":resetPassword", map[string]string{"password": "purple-canyon-7"}, http.StatusUnprocessableEntity)

	// Owner protection (the takeover guard, mirroring impersonation): an owner cannot
	// have its password reset by a lesser admin, nor even by another owner.
	c.do(adminTok, "POST", "/principals/"+ownerID+":resetPassword", map[string]string{"password": "purple-canyon-7"}, http.StatusForbidden)
	otherOwnerTok := principalWithGrants(t, ctx, dsn, "other-owner", []grant{{role: "owner", scopeKind: "all"}})
	c.do(otherOwnerTok, "POST", "/principals/"+ownerID+":resetPassword", map[string]string{"password": "purple-canyon-7"}, http.StatusForbidden)

	// The admin resets the password.
	c.do(adminTok, "POST", path, map[string]string{"password": "purple-canyon-7"}, http.StatusNoContent)

	login := func(pw string) int {
		b, _ := json.Marshal(map[string]string{"username": "alice", "password": pw})
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := login("purple-canyon-7"); code != http.StatusNoContent {
		t.Fatalf("login with the new password: want 204, got %d", code)
	}
	if code := login("orange-boat-42x"); code == http.StatusNoContent {
		t.Fatal("login with the old password should fail after a reset")
	}

	// An unknown id is 404.
	c.do(adminTok, "POST", "/principals/00000000-0000-0000-0000-000000000000:resetPassword", map[string]string{"password": "purple-canyon-7"}, http.StatusNotFound)
}
