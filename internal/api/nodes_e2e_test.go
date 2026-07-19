package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
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

	// N1: create carries display_name + location, and the create response mints no token.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, scope.Set{All: true}); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	created := c.do(ownerTok, http.MethodPost, "/nodes", map[string]any{"name": "site-c", "display_name": "Site C", "location": "hq"}, http.StatusCreated)
	if strings.Contains(string(created), `"token"`) {
		t.Fatalf("create must not mint a token: %s", created)
	}
	var cbody struct {
		DisplayName string  `json:"display_name"`
		Location    *string `json:"location"`
	}
	json.Unmarshal(created, &cbody)
	if cbody.DisplayName != "Site C" || cbody.Location == nil || *cbody.Location != "hq" {
		t.Fatalf("create identity: got %+v", cbody)
	}

	// N1: PATCH the display name and clear the location; the name stays immutable.
	var patched struct {
		Name        string  `json:"name"`
		DisplayName string  `json:"display_name"`
		Location    *string `json:"location"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/nodes/site-c", map[string]any{"display_name": "Site C prod", "location": ""}, http.StatusOK), &patched)
	if patched.Name != "site-c" || patched.DisplayName != "Site C prod" || patched.Location != nil {
		t.Fatalf("patch: got %+v", patched)
	}
	// An unknown location is a 422, not a silent apply.
	c.do(ownerTok, http.MethodPatch, "/nodes/site-c", map[string]any{"location": "ghost"}, http.StatusUnprocessableEntity)
	// node:update is gated: the viewer cannot patch.
	c.do(viewerTok, http.MethodPatch, "/nodes/site-c", map[string]any{"display_name": "x"}, http.StatusForbidden)

	// N2: node is a taggable owner kind. A key that applies to nodes binds via
	// :setTag (node:update), reads back on :listTags and in the node's effective_tags,
	// and unbinds via :removeTag; the viewer is gated out of the write.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "environment", Propagates: true}, scope.Set{All: true}); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/nodes/site-a:setTag", map[string]any{"key": "environment", "value": "prod"}, http.StatusOK)
	var tagsList struct {
		Tags []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-a:listTags", nil, http.StatusOK), &tagsList)
	if len(tagsList.Tags) != 1 || tagsList.Tags[0].Key != "environment" {
		t.Fatalf("node direct tags = %+v, want environment", tagsList.Tags)
	}
	var withTags struct {
		EffectiveTags map[string]string `json:"effective_tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-a", nil, http.StatusOK), &withTags)
	if withTags.EffectiveTags["environment"] != "prod" {
		t.Fatalf("node effective_tags = %+v, want environment=prod", withTags.EffectiveTags)
	}
	// node:update gates the tag write: the viewer is forbidden.
	c.do(viewerTok, http.MethodPost, "/nodes/site-a:setTag", map[string]any{"key": "environment", "value": "x"}, http.StatusForbidden)
	// Unbind clears it (a fresh var: effective_tags is omitempty, so an empty map is
	// absent from the JSON and would not overwrite a reused struct's stale map).
	c.do(ownerTok, http.MethodPost, "/nodes/site-a:removeTag", map[string]any{"key": "environment"}, http.StatusNoContent)
	var afterUnbind struct {
		EffectiveTags map[string]string `json:"effective_tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-a", nil, http.StatusOK), &afterUnbind)
	if _, ok := afterUnbind.EffectiveTags["environment"]; ok {
		t.Fatalf("environment still effective after unbind")
	}
}
