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

// TestPrincipalDirectoryAPI drives the admin directory against the real binary:
// an all-scope owner lists, gets, and creates principals; a location-scoped admin
// is refused (a principal is not a scope-tree entity); duplicate and short-password
// creates are rejected; secrets never appear in a response; and a created human can
// sign in with the initial password. Skipped under -short.
func TestPrincipalDirectoryAPI(t *testing.T) {
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

	// An all-scope owner (principal:* at all scope) and a location-scoped admin
	// (has principal:read via the admin role, but only at location scope).
	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := principalWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner lists the two service principals we seeded.
	if code, body := c.send(ownerTok, "GET", "/principals", nil); code != 200 ||
		!bytes.Contains(body, []byte(`"owner-all"`)) || !bytes.Contains(body, []byte(`"hq-admin"`)) {
		t.Fatalf("owner list: code %d body %s", code, body)
	}

	// Owner creates a human with an initial password. The response carries no secret.
	created := c.do(ownerTok, "POST", "/principals", map[string]string{
		"username": "alice", "password": "alice-s3cret", "email": "alice@example.test", "display_name": "Alice",
	}, http.StatusCreated)
	if !bytes.Contains(created, []byte(`"username":"alice"`)) {
		t.Fatalf("create body missing username: %s", created)
	}
	assertNoSecret(t, created)
	var made struct {
		ID    string `json:"id"`
		Human struct {
			Username string `json:"username"`
		} `json:"human"`
	}
	if err := json.Unmarshal(created, &made); err != nil || made.ID == "" {
		t.Fatalf("parse created: %v (%s)", err, created)
	}

	// The kind filter returns only the human, not the seeded services.
	if code, body := c.send(ownerTok, "GET", "/principals?kind=human", nil); code != 200 ||
		!bytes.Contains(body, []byte(`"username":"alice"`)) || bytes.Contains(body, []byte(`"owner-all"`)) {
		t.Fatalf("kind=human filter: code %d body %s", code, body)
	}

	// Get by id, and an unknown id is a 404.
	if code, body := c.send(ownerTok, "GET", "/principals/"+made.ID, nil); code != 200 || !bytes.Contains(body, []byte(`"username":"alice"`)) {
		t.Fatalf("get alice: code %d body %s", code, body)
	}
	assertNoSecret(t, c.do(ownerTok, "GET", "/principals/"+made.ID, nil, 200))
	if code, _ := c.send(ownerTok, "GET", "/principals/00000000-0000-0000-0000-000000000000", nil); code != http.StatusNotFound {
		t.Fatalf("get unknown: want 404, got %d", code)
	}

	// A duplicate username is a 409; a too-short password is a 422.
	if code, _ := c.send(ownerTok, "POST", "/principals", map[string]string{"username": "alice", "password": "another-pw"}); code != http.StatusConflict {
		t.Fatalf("duplicate username: want 409, got %d", code)
	}
	if code, _ := c.send(ownerTok, "POST", "/principals", map[string]string{"username": "bob", "password": "short"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("short password: want 422, got %d", code)
	}

	// The all-scope invariant: a location-scoped admin is refused list and create.
	if code, _ := c.send(scopedTok, "GET", "/principals", nil); code != http.StatusForbidden {
		t.Fatalf("scoped list: want 403, got %d", code)
	}
	if code, _ := c.send(scopedTok, "POST", "/principals", map[string]string{"username": "eve", "password": "eve-s3cret"}); code != http.StatusForbidden {
		t.Fatalf("scoped create: want 403, got %d", code)
	}

	// The created human can sign in with the initial password.
	loginBody, _ := json.Marshal(map[string]string{"username": "alice", "password": "alice-s3cret"})
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("created human login: want 204, got %d", resp.StatusCode)
	}
}

// assertNoSecret fails if a response body carries anything that looks like a
// credential secret: the hash column, or the cleartext password we sent.
func assertNoSecret(t *testing.T, body []byte) {
	t.Helper()
	for _, banned := range []string{"secret_hash", "secret", "alice-s3cret", "$argon2"} {
		if bytes.Contains(body, []byte(banned)) {
			t.Fatalf("response leaked %q: %s", banned, body)
		}
	}
}
