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
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestForceChangePasswordGate drives the force-change gate through the real binary
// (issue #159): after an admin reset, the flagged user can log in and read their
// own principal, but every other route is refused with a distinct "password change
// required" 403 until they change the password, after which the gate releases.
// Skipped under -short.
func TestForceChangePasswordGate(t *testing.T) {
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
	_, bh, bp, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "ops", SecretHash: bh, Prefix: bp}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, bh)
	all := scope.Set{All: true}

	initHash, _ := auth.HashPassword("orange-boat-42x")
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: initHash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	// Admin reset to a known value: this flags must_change_password for alice.
	resetHash, _ := auth.HashPassword("purple-canyon-7")
	if err := gw.SetPrincipalPassword(ctx, root.ID, alice.ID, resetHash, all); err != nil {
		t.Fatalf("admin reset: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	// Log in as alice with the admin-set password; she is flagged but login still works.
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "purple-canyon-7"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "og_session" {
			session = c
		}
	}
	resp.Body.Close()
	if session == nil {
		t.Fatal("expected a session cookie for the flagged user")
	}

	get := func(path string) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
		r.AddCookie(session)
		rr, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer rr.Body.Close()
		b, _ := io.ReadAll(rr.Body)
		return rr.StatusCode, b
	}

	// /auth/me is allowed and carries the flag so the console can render the gate.
	if code, b := get("/api/v1/auth/me"); code != http.StatusOK || !bytes.Contains(b, []byte(`"must_change_password":true`)) {
		t.Fatalf("me while flagged: want 200 with the flag, got %d (%s)", code, b)
	}
	// Any other route is gated with the distinct message (not the generic forbidden).
	if code, b := get("/api/v1/principals"); code != http.StatusForbidden || !bytes.Contains(b, []byte("password change required")) {
		t.Fatalf("principals while flagged: want 403 'password change required', got %d (%s)", code, b)
	}

	// Change the password: the current is the admin-set value, the new is policy-valid.
	cp, _ := json.Marshal(map[string]string{"current_password": "purple-canyon-7", "new_password": "silver-meadow-9"})
	cr, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/auth/me:changePassword", bytes.NewReader(cp))
	cr.Header.Set("Content-Type", "application/json")
	cr.AddCookie(session)
	cresp, err := http.DefaultClient.Do(cr)
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password: want 204, got %d", cresp.StatusCode)
	}

	// The gate has released: the same route is now the ordinary permission 403 (alice
	// holds no grants), NOT the force-change message. The flag is cleared on /auth/me.
	if code, b := get("/api/v1/principals"); code != http.StatusForbidden || bytes.Contains(b, []byte("password change required")) {
		t.Fatalf("principals after change: want a generic 403, got %d (%s)", code, b)
	}
	if code, b := get("/api/v1/auth/me"); code != http.StatusOK || bytes.Contains(b, []byte(`"must_change_password":true`)) {
		t.Fatalf("me after change: flag should be cleared, got %d (%s)", code, b)
	}
}
