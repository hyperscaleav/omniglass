package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestUserDirectoryAPI drives the admin directory against the real binary:
// an all-scope owner lists, gets, and creates users; a location-scoped admin
// is refused (a user is not a scope-tree entity); duplicate and short-password
// creates are rejected; secrets never appear in a response; and a created person can
// sign in with the initial password. Skipped under -short.
func TestUserDirectoryAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	// An all-scope owner (user:* at all scope) and a location-scoped admin
	// (has user:read via the admin role, but only at location scope).
	ownerTok := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := userWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner lists the two machine users we seeded.
	if code, body := c.send(ownerTok, "GET", "/users", nil); code != 200 ||
		!bytes.Contains(body, []byte(`"owner-all"`)) || !bytes.Contains(body, []byte(`"hq-admin"`)) {
		t.Fatalf("owner list: code %d body %s", code, body)
	}

	// Owner creates a person with an initial password. The response carries no secret.
	created := c.do(ownerTok, "POST", "/users", map[string]string{
		"username": "alice", "password": "orange-boat-42x", "email": "alice@example.test", "label": "Alice",
	}, http.StatusCreated)
	if !bytes.Contains(created, []byte(`"username":"alice"`)) {
		t.Fatalf("create body missing username: %s", created)
	}
	assertNoSecret(t, created)
	var made struct {
		ID     string `json:"id"`
		Person struct {
			Username string `json:"username"`
		} `json:"person"`
	}
	if err := json.Unmarshal(created, &made); err != nil || made.ID == "" {
		t.Fatalf("parse created: %v (%s)", err, created)
	}

	// The kind filter returns only the person, not the seeded machine users.
	if code, body := c.send(ownerTok, "GET", "/users?kind=person", nil); code != 200 ||
		!bytes.Contains(body, []byte(`"username":"alice"`)) || bytes.Contains(body, []byte(`"owner-all"`)) {
		t.Fatalf("kind=person filter: code %d body %s", code, body)
	}

	// Get by id, and an unknown id is a 404.
	if code, body := c.send(ownerTok, "GET", "/users/"+made.ID, nil); code != 200 || !bytes.Contains(body, []byte(`"username":"alice"`)) {
		t.Fatalf("get alice: code %d body %s", code, body)
	}
	assertNoSecret(t, c.do(ownerTok, "GET", "/users/"+made.ID, nil, 200))
	if code, _ := c.send(ownerTok, "GET", "/users/00000000-0000-0000-0000-000000000000", nil); code != http.StatusNotFound {
		t.Fatalf("get unknown: want 404, got %d", code)
	}

	// A duplicate username is a 409; a too-short password is a 422.
	if code, _ := c.send(ownerTok, "POST", "/users", map[string]string{"username": "alice", "password": "orange-boat-99z"}); code != http.StatusConflict {
		t.Fatalf("duplicate username: want 409, got %d", code)
	}
	if code, _ := c.send(ownerTok, "POST", "/users", map[string]string{"username": "bob", "password": "short"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("short password: want 422, got %d", code)
	}

	// The all-scope invariant: a location-scoped admin is refused list, get, and create.
	if code, _ := c.send(scopedTok, "GET", "/users", nil); code != http.StatusForbidden {
		t.Fatalf("scoped list: want 403, got %d", code)
	}
	if code, _ := c.send(scopedTok, "GET", "/users/"+made.ID, nil); code != http.StatusForbidden {
		t.Fatalf("scoped get: want 403, got %d", code)
	}
	if code, _ := c.send(scopedTok, "POST", "/users", map[string]string{"username": "eve", "password": "purple-canyon-7"}); code != http.StatusForbidden {
		t.Fatalf("scoped create: want 403, got %d", code)
	}

	// The created person can sign in with the initial password.
	loginBody, _ := json.Marshal(map[string]string{"username": "alice", "password": "orange-boat-42x"})
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("created person login: want 204, got %d", resp.StatusCode)
	}
}

// TestUpdateUserAPI drives the admin update against the real binary: an
// all-scope admin edits a person's label, email, and username; the rename
// re-homes the login (new username works, old fails); a location-scoped admin is
// refused; a clash is 409, an unknown id 404, and a non-person target 422. Skipped
// under -short.
func TestUpdateUserAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := userWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Create a person with a password, capture its id.
	created := c.do(ownerTok, "POST", "/users", map[string]string{"username": "alice", "password": "orange-boat-42x", "label": "Alice"}, http.StatusCreated)
	var made struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(created, &made)

	// Update all three admin-owned fields, including a rename.
	upd := c.do(ownerTok, "PATCH", "/users/"+made.ID, map[string]string{
		"label": "Alice Cooper", "email": "ac@example.test", "username": "alice-2",
	}, http.StatusOK)
	if !bytes.Contains(upd, []byte(`"username":"alice-2"`)) || !bytes.Contains(upd, []byte(`"label":"Alice Cooper"`)) {
		t.Fatalf("update body: %s", upd)
	}

	// The rename re-homes the login: the new username works, the old does not.
	login := func(u string) int {
		body, _ := json.Marshal(map[string]string{"username": u, "password": "orange-boat-42x"})
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
	if code, _ := c.send(scopedTok, "PATCH", "/users/"+made.ID, map[string]string{"label": "no"}); code != http.StatusForbidden {
		t.Fatalf("scoped update: want 403, got %d", code)
	}
	if code, _ := c.send(ownerTok, "PATCH", "/users/00000000-0000-0000-0000-000000000000", map[string]string{"label": "no"}); code != http.StatusNotFound {
		t.Fatalf("unknown update: want 404, got %d", code)
	}

	// A username clash is 409.
	c.do(ownerTok, "POST", "/users", map[string]string{"username": "bob"}, http.StatusCreated)
	var bobID string
	for _, p := range listIDs(t, c, ownerTok) {
		if p.username == "bob" {
			bobID = p.id
		}
	}
	if code, _ := c.send(ownerTok, "PATCH", "/users/"+bobID, map[string]string{"username": "alice-2"}); code != http.StatusConflict {
		t.Fatalf("clash update: want 409, got %d", code)
	}

	// A non-person target (a machine user) is 422.
	var machineID string
	for _, p := range listIDs(t, c, ownerTok) {
		if p.kind == "machine" {
			machineID = p.id
		}
	}
	if machineID == "" {
		t.Fatal("expected a machine user in the directory")
	}
	if code, _ := c.send(ownerTok, "PATCH", "/users/"+machineID, map[string]string{"label": "no"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("non-person update: want 422, got %d", code)
	}
}

// TestGrantAPI drives role assignment against the real binary: an admin grants a
// role, sees it on the user, revokes it, cannot strip the last owner, and a
// location-scoped admin is refused. Skipped under -short.
func TestGrantAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := userWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	created := c.do(ownerTok, "POST", "/users", map[string]string{"username": "alice"}, http.StatusCreated)
	var alice struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(created, &alice)

	// Grant viewer@all, capture the grant id from the response.
	g := c.do(ownerTok, "POST", "/users/"+alice.ID+"/grants", map[string]string{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)
	var grantResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(g, &grantResp)
	if grantResp.ID == "" {
		t.Fatalf("create grant returned no id: %s", g)
	}
	if _, body := c.send(ownerTok, "GET", "/users/"+alice.ID, nil); !bytes.Contains(body, []byte(`"role":"viewer"`)) {
		t.Fatalf("grant not on user: %s", body)
	}

	// Bad inputs.
	if code, _ := c.send(ownerTok, "POST", "/users/"+alice.ID+"/grants", map[string]string{"role": "nope", "scope_kind": "all"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown role: want 422, got %d", code)
	}
	if code, _ := c.send(ownerTok, "POST", "/users/"+alice.ID+"/grants", map[string]string{"role": "viewer", "scope_kind": "location"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("scoped grant without id: want 422, got %d", code)
	}
	if code, _ := c.send(scopedTok, "POST", "/users/"+alice.ID+"/grants", map[string]string{"role": "viewer", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Fatalf("scoped create grant: want 403, got %d", code)
	}

	// Revoke it; then it is gone.
	if code, _ := c.send(ownerTok, "DELETE", "/users/"+alice.ID+"/grants/"+grantResp.ID, nil); code != http.StatusNoContent {
		t.Fatalf("revoke: want 204, got %d", code)
	}
	if code, _ := c.send(ownerTok, "DELETE", "/users/"+alice.ID+"/grants/"+grantResp.ID, nil); code != http.StatusNotFound {
		t.Fatalf("revoke again: want 404, got %d", code)
	}

	// The owner invariant: the owner cannot revoke its own last owner grant.
	_, me := c.send(ownerTok, "GET", "/auth/me", nil)
	var meDoc struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(me, &meDoc)
	_, ownerDetail := c.send(ownerTok, "GET", "/users/"+meDoc.User.ID, nil)
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
	if code, _ := c.send(ownerTok, "DELETE", "/users/"+meDoc.User.ID+"/grants/"+ownerGrantID, nil); code != http.StatusConflict {
		t.Fatalf("revoke last owner: want 409, got %d", code)
	}
}

// TestDisableAPI drives soft-disable against the real binary: an admin disables a
// user (who can then no longer sign in), re-enables them, cannot disable the last
// owner, and a location-scoped admin is refused. Skipped under -short.
func TestDisableAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	scopedTok := userWithGrants(t, ctx, dsn, "hq-admin", []grant{{role: "admin", scopeKind: "location", scopeID: "HQ"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	created := c.do(ownerTok, "POST", "/users", map[string]string{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated)
	var alice struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(created, &alice)

	login := func() int {
		body, _ := json.Marshal(map[string]string{"username": "alice", "password": "orange-boat-42x"})
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if login() != http.StatusNoContent {
		t.Fatal("alice should sign in before disable")
	}
	// Disable: sign-in is refused and the directory shows inactive.
	if code, _ := c.send(ownerTok, "POST", "/users/"+alice.ID+":disable", nil); code != http.StatusNoContent {
		t.Fatalf("disable: want 204, got %d", code)
	}
	// A disabled account with the CORRECT password gets a distinct 403 ("account
	// disabled"), not the generic 401, so the sign-in screen can explain it.
	if login() != http.StatusForbidden {
		t.Fatal("a disabled user with the right password should get 403 account-disabled")
	}
	// A WRONG password against the disabled account stays a generic 401: no
	// account-state oracle at the HTTP layer for someone without the password.
	badLogin := func() int {
		body, _ := json.Marshal(map[string]string{"username": "alice", "password": "nope-wrong"})
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if badLogin() != http.StatusUnauthorized {
		t.Fatal("a disabled user with a wrong password should get a generic 401")
	}
	if _, body := c.send(ownerTok, "GET", "/users/"+alice.ID, nil); !bytes.Contains(body, []byte(`"active":false`)) {
		t.Fatalf("alice should read inactive: %s", body)
	}
	// Enable restores access.
	if code, _ := c.send(ownerTok, "POST", "/users/"+alice.ID+":enable", nil); code != http.StatusNoContent {
		t.Fatalf("enable: want 204, got %d", code)
	}
	if login() != http.StatusNoContent {
		t.Fatal("a re-enabled user should sign in")
	}

	// The last active owner cannot be disabled.
	_, me := c.send(ownerTok, "GET", "/auth/me", nil)
	var meDoc struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(me, &meDoc)
	if code, _ := c.send(ownerTok, "POST", "/users/"+meDoc.User.ID+":disable", nil); code != http.StatusConflict {
		t.Fatalf("disable last owner: want 409, got %d", code)
	}
	// A location-scoped admin is refused.
	if code, _ := c.send(scopedTok, "POST", "/users/"+alice.ID+":disable", nil); code != http.StatusForbidden {
		t.Fatalf("scoped disable: want 403, got %d", code)
	}
}

type dirRow struct{ id, kind, username string }

// listIDs pulls the user directory as a flat id/kind/username list.
func listIDs(t *testing.T, c *apiClient, tok string) []dirRow {
	t.Helper()
	_, body := c.send(tok, "GET", "/users", nil)
	var doc struct {
		Users []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Person *struct {
				Username string `json:"username"`
			} `json:"person"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	rows := make([]dirRow, 0, len(doc.Users))
	for _, p := range doc.Users {
		r := dirRow{id: p.ID, kind: p.Kind}
		if p.Person != nil {
			r.username = p.Person.Username
		}
		rows = append(rows, r)
	}
	return rows
}

// assertNoSecret fails if a response body carries anything that looks like a
// credential secret: the hash column, or the cleartext password we sent.
func assertNoSecret(t *testing.T, body []byte) {
	t.Helper()
	for _, banned := range []string{"secret_hash", "secret", "orange-boat-42x", "$argon2"} {
		if bytes.Contains(body, []byte(banned)) {
			t.Fatalf("response leaked %q: %s", banned, body)
		}
	}
}
