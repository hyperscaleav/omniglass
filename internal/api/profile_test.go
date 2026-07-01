package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSelfProfileAndChangePassword drives the slice-2 self-service surface against
// the real binary: a signed-in human changes their own password (wrong current is
// refused, a too-short new one is rejected, the right one rotates the login) and
// edits their own display name and email, with both requiring authentication.
// Skipped under -short.
func TestSelfProfileAndChangePassword(t *testing.T) {
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

	pwHash, err := auth.HashPassword("s3cret-pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
		Username: "ops", Email: "ops@old.example", DisplayName: "Old Name",
		SecretHash: bh, Prefix: bp, PasswordHash: pwHash,
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	var session *http.Cookie
	login := func(pw string) int {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": "ops", "password": pw})
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		defer resp.Body.Close()
		var cookie *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == "og_session" {
				cookie = c
			}
		}
		if resp.StatusCode == http.StatusNoContent && cookie != nil {
			session = cookie
		}
		return resp.StatusCode
	}

	// Establish a session.
	if code := login("s3cret-pw"); code != http.StatusNoContent {
		t.Fatalf("initial login: want 204, got %d", code)
	}

	send := func(method, path string, withCookie bool, body any) (int, []byte) {
		t.Helper()
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, r)
		req.Header.Set("Content-Type", "application/json")
		if withCookie {
			req.AddCookie(session)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// --- change password ---
	// Wrong current password is refused.
	if code, _ := send(http.MethodPost, "/api/v1/auth/me:changePassword", true,
		map[string]string{"current_password": "nope", "new_password": "brand-new-pw"}); code != http.StatusForbidden {
		t.Fatalf("wrong current: want 403, got %d", code)
	}
	// A too-short new password is rejected by validation.
	if code, _ := send(http.MethodPost, "/api/v1/auth/me:changePassword", true,
		map[string]string{"current_password": "s3cret-pw", "new_password": "short"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("short new: want 422, got %d", code)
	}
	// Unauthenticated is 401.
	if code, _ := send(http.MethodPost, "/api/v1/auth/me:changePassword", false,
		map[string]string{"current_password": "s3cret-pw", "new_password": "brand-new-pw"}); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated change: want 401, got %d", code)
	}
	// The right current password rotates it.
	if code, _ := send(http.MethodPost, "/api/v1/auth/me:changePassword", true,
		map[string]string{"current_password": "s3cret-pw", "new_password": "brand-new-pw"}); code != http.StatusNoContent {
		t.Fatalf("change password: want 204, got %d", code)
	}
	// The old password no longer logs in; the new one does.
	if code := login("s3cret-pw"); code != http.StatusUnauthorized {
		t.Fatalf("old password after change: want 401, got %d", code)
	}
	if code := login("brand-new-pw"); code != http.StatusNoContent {
		t.Fatalf("new password after change: want 204, got %d", code)
	}

	// --- update profile ---
	// The display name is self-editable and keeps the bootstrapped email untouched.
	code, body := send(http.MethodPatch, "/api/v1/auth/me", true,
		map[string]string{"display_name": "Ops Lead"})
	if code != http.StatusOK {
		t.Fatalf("update profile: want 200, got %d (%s)", code, body)
	}
	if !bytes.Contains(body, []byte(`"display_name":"Ops Lead"`)) {
		t.Fatalf("update profile body missing the new display name: %s", body)
	}
	if !bytes.Contains(body, []byte(`"email":"ops@old.example"`)) {
		t.Fatalf("email must be preserved, but it changed: %s", body)
	}
	// Email is not a self-editable field: a body carrying it is rejected outright.
	if code, _ := send(http.MethodPatch, "/api/v1/auth/me", true,
		map[string]string{"email": "hacker@evil.example"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("email in the self-service body: want 422, got %d", code)
	}
	// GET /auth/me reflects the display name and keeps the original email.
	_, me := send(http.MethodGet, "/api/v1/auth/me", true, nil)
	if !bytes.Contains(me, []byte(`"display_name":"Ops Lead"`)) || !bytes.Contains(me, []byte(`"email":"ops@old.example"`)) {
		t.Fatalf("me after update: want new display name and unchanged email, got %s", me)
	}
	// Unauthenticated update is 401.
	if code, _ := send(http.MethodPatch, "/api/v1/auth/me", false,
		map[string]string{"display_name": "Nope"}); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated update: want 401, got %d", code)
	}
}
