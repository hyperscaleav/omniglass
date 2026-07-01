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

	// The all-scope invariant: a location-scoped admin is refused list, get, and create.
	if code, _ := c.send(scopedTok, "GET", "/principals", nil); code != http.StatusForbidden {
		t.Fatalf("scoped list: want 403, got %d", code)
	}
	if code, _ := c.send(scopedTok, "GET", "/principals/"+made.ID, nil); code != http.StatusForbidden {
		t.Fatalf("scoped get: want 403, got %d", code)
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

// TestUpdatePrincipalAPI drives the admin update against the real binary: an
// all-scope admin edits a human's display name, email, and username; the rename
// re-homes the login (new username works, old fails); a location-scoped admin is
// refused; a clash is 409, an unknown id 404, and a non-human target 422. Skipped
// under -short.
func TestUpdatePrincipalAPI(t *testing.T) {
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

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := principalWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Create a human with a password, capture its id.
	created := c.do(ownerTok, "POST", "/principals", map[string]string{"username": "alice", "password": "alice-s3cret", "display_name": "Alice"}, http.StatusCreated)
	var made struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(created, &made)

	// Update all three admin-owned fields, including a rename.
	upd := c.do(ownerTok, "PATCH", "/principals/"+made.ID, map[string]string{
		"display_name": "Alice Cooper", "email": "ac@example.test", "username": "alice-2",
	}, http.StatusOK)
	if !bytes.Contains(upd, []byte(`"username":"alice-2"`)) || !bytes.Contains(upd, []byte(`"display_name":"Alice Cooper"`)) {
		t.Fatalf("update body: %s", upd)
	}

	// The rename re-homes the login: the new username works, the old does not.
	login := func(u string) int {
		body, _ := json.Marshal(map[string]string{"username": u, "password": "alice-s3cret"})
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := login("alice-2"); code != http.StatusNoContent {
		t.Fatalf("login under new username: want 204, got %d", code)
	}
	if code := login("alice"); code != http.StatusUnauthorized {
		t.Fatalf("login under old username: want 401, got %d", code)
	}

	// A location-scoped admin is refused, an unknown id is 404.
	if code, _ := c.send(scopedTok, "PATCH", "/principals/"+made.ID, map[string]string{"display_name": "no"}); code != http.StatusForbidden {
		t.Fatalf("scoped update: want 403, got %d", code)
	}
	if code, _ := c.send(ownerTok, "PATCH", "/principals/00000000-0000-0000-0000-000000000000", map[string]string{"display_name": "no"}); code != http.StatusNotFound {
		t.Fatalf("unknown update: want 404, got %d", code)
	}

	// A username clash is 409.
	c.do(ownerTok, "POST", "/principals", map[string]string{"username": "bob"}, http.StatusCreated)
	var bobID string
	for _, p := range listIDs(t, c, ownerTok) {
		if p.username == "bob" {
			bobID = p.id
		}
	}
	if code, _ := c.send(ownerTok, "PATCH", "/principals/"+bobID, map[string]string{"username": "alice-2"}); code != http.StatusConflict {
		t.Fatalf("clash update: want 409, got %d", code)
	}

	// A non-human target (a service principal) is 422.
	var svcID string
	for _, p := range listIDs(t, c, ownerTok) {
		if p.kind == "service" {
			svcID = p.id
		}
	}
	if svcID == "" {
		t.Fatal("expected a service principal in the directory")
	}
	if code, _ := c.send(ownerTok, "PATCH", "/principals/"+svcID, map[string]string{"display_name": "no"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("non-human update: want 422, got %d", code)
	}
}

// TestGrantAPI drives role assignment against the real binary: an admin grants a
// role, sees it on the principal, revokes it, cannot strip the last owner, and a
// location-scoped admin is refused. Skipped under -short.
func TestGrantAPI(t *testing.T) {
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

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := principalWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	created := c.do(ownerTok, "POST", "/principals", map[string]string{"username": "alice"}, http.StatusCreated)
	var alice struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(created, &alice)

	// Grant viewer@all, capture the grant id from the response.
	g := c.do(ownerTok, "POST", "/principals/"+alice.ID+"/grants", map[string]string{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	var grantResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(g, &grantResp)
	if grantResp.ID == "" {
		t.Fatalf("create grant returned no id: %s", g)
	}
	if _, body := c.send(ownerTok, "GET", "/principals/"+alice.ID, nil); !bytes.Contains(body, []byte(`"role":"viewer"`)) {
		t.Fatalf("grant not on principal: %s", body)
	}

	// Bad inputs.
	if code, _ := c.send(ownerTok, "POST", "/principals/"+alice.ID+"/grants", map[string]string{"role": "nope", "scope_kind": "all"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown role: want 422, got %d", code)
	}
	if code, _ := c.send(ownerTok, "POST", "/principals/"+alice.ID+"/grants", map[string]string{"role": "viewer", "scope_kind": "location"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("scoped grant without id: want 422, got %d", code)
	}
	if code, _ := c.send(scopedTok, "POST", "/principals/"+alice.ID+"/grants", map[string]string{"role": "viewer", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Fatalf("scoped create grant: want 403, got %d", code)
	}

	// Revoke it; then it is gone.
	if code, _ := c.send(ownerTok, "DELETE", "/principals/"+alice.ID+"/grants/"+grantResp.ID, nil); code != http.StatusNoContent {
		t.Fatalf("revoke: want 204, got %d", code)
	}
	if code, _ := c.send(ownerTok, "DELETE", "/principals/"+alice.ID+"/grants/"+grantResp.ID, nil); code != http.StatusNotFound {
		t.Fatalf("revoke again: want 404, got %d", code)
	}

	// The owner invariant: the owner cannot revoke its own last owner grant.
	_, me := c.send(ownerTok, "GET", "/auth/me", nil)
	var meDoc struct {
		Principal struct {
			ID string `json:"id"`
		} `json:"principal"`
	}
	_ = json.Unmarshal(me, &meDoc)
	_, ownerDetail := c.send(ownerTok, "GET", "/principals/"+meDoc.Principal.ID, nil)
	var ownerDoc struct {
		Grants []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"grants"`
	}
	_ = json.Unmarshal(ownerDetail, &ownerDoc)
	var ownerGrantID string
	for _, gr := range ownerDoc.Grants {
		if gr.Role == "owner" {
			ownerGrantID = gr.ID
		}
	}
	if ownerGrantID == "" {
		t.Fatal("owner should hold an owner grant")
	}
	if code, _ := c.send(ownerTok, "DELETE", "/principals/"+meDoc.Principal.ID+"/grants/"+ownerGrantID, nil); code != http.StatusConflict {
		t.Fatalf("revoke last owner: want 409, got %d", code)
	}
}

type dirRow struct{ id, kind, username string }

// listIDs pulls the principal directory as a flat id/kind/username list.
func listIDs(t *testing.T, c *apiClient, tok string) []dirRow {
	t.Helper()
	_, body := c.send(tok, "GET", "/principals", nil)
	var doc struct {
		Principals []struct {
			ID    string `json:"id"`
			Kind  string `json:"kind"`
			Human *struct {
				Username string `json:"username"`
			} `json:"human"`
		} `json:"principals"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	rows := make([]dirRow, 0, len(doc.Principals))
	for _, p := range doc.Principals {
		r := dirRow{id: p.ID, kind: p.Kind}
		if p.Human != nil {
			r.username = p.Human.Username
		}
		rows = append(rows, r)
	}
	return rows
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
