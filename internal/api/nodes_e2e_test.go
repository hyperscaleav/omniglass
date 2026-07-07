package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// setupAllViewer inserts a service principal with a single viewer@all grant and
// returns its bearer token: an all-scope reader that still lacks node:create /
// node:enroll, so it exercises the capability fast-reject independent of scope.
func setupAllViewer(t *testing.T, ctx context.Context, dsn, label string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into service (principal_id, label) values ($1, $2)`, pid, label); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, hash, prefix); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, 'viewer', 'all', null)`,
		pid); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	return tok
}

// TestNodeAPI drives the node enrollment surface over HTTP: an owner creates a
// node, mints its enrollment token, and the node claims its NATS credential; a
// bad name is rejected, a wrong token is a 401, and a principal without node
// capability is forbidden. Skipped under -short.
func TestNodeAPI(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL("nats://bus.example:4222")))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Create, with a bad-name rejection.
	c.do(ownerTok, http.MethodPost, "/nodes", map[string]any{"name": "site-a", "description": "lab node"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/nodes", map[string]any{"name": "bad.name"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/nodes", map[string]any{"name": "site-a"}, http.StatusConflict)

	// Enroll: the token is returned once.
	var enroll struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/nodes/site-a:enroll", nil, http.StatusOK), &enroll)
	if enroll.Token == "" {
		t.Fatalf("enroll returned no token")
	}

	// Claim: a wrong token is a 401; the right token returns the NATS credential.
	c.do("", http.MethodPost, "/nodes:claim", map[string]any{"name": "site-a", "token": "nope"}, http.StatusUnauthorized)
	var claim struct {
		NatsURL  string `json:"nats_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.Unmarshal(c.do("", http.MethodPost, "/nodes:claim", map[string]any{"name": "site-a", "token": enroll.Token}, http.StatusOK), &claim)
	if claim.NatsURL != "nats://bus.example:4222" || claim.Username != "site-a" || claim.Password != enroll.Token {
		t.Fatalf("claim credential = %+v, want the advertised url + name + token", claim)
	}

	// The claim marked the node enrolled.
	var node struct {
		Name     string `json:"name"`
		Enrolled bool   `json:"enrolled"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-a", nil, http.StatusOK), &node)
	if !node.Enrolled {
		t.Fatalf("node not marked enrolled after claim")
	}

	// A viewer (no node:create / node:enroll) is capability-forbidden.
	viewerTok := setupAllViewer(t, ctx, dsn, "viewer-1")
	c.do(viewerTok, http.MethodPost, "/nodes", map[string]any{"name": "site-b"}, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/nodes/site-a:enroll", nil, http.StatusForbidden)
}
