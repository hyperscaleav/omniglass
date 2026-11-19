package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
)

// setupAllViewer inserts a service user with a single viewer@all grant and
// returns its bearer token: an all-scope reader that still lacks outpost:create /
// outpost:enroll, so it exercises the capability fast-reject independent of scope.
func setupAllViewer(t *testing.T, ctx context.Context, dsn, label string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into machine (user_id, name) values ($1, $2)`, pid, label); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, hash, prefix); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'viewer'), 'all', null)`,
		pid); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	return tok
}

// TestOutpostAPI drives the outpost enrollment surface over HTTP: an owner creates a
// outpost, mints its enrollment token, and the outpost claims its NATS credential; a
// bad name is rejected, a wrong token is a 401, and a user without outpost
// capability is forbidden. Skipped under -short.
func TestOutpostAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

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
	c.do(ownerTok, http.MethodPost, "/outposts", map[string]any{"name": "site-a", "description": "lab outpost"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/outposts", map[string]any{"name": "bad.name"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/outposts", map[string]any{"name": "site-a"}, http.StatusConflict)

	// Enroll: the token is returned once.
	var enroll struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/outposts/site-a:enroll", nil, http.StatusOK), &enroll)
	if enroll.Token == "" {
		t.Fatalf("enroll returned no token")
	}

	// Claim: a wrong token is a 401; the right token returns the NATS credential.
	c.do("", http.MethodPost, "/outposts:claim", map[string]any{"name": "site-a", "token": "nope"}, http.StatusUnauthorized)
	var claim struct {
		NatsURL  string `json:"nats_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.Unmarshal(c.do("", http.MethodPost, "/outposts:claim", map[string]any{"name": "site-a", "token": enroll.Token}, http.StatusOK), &claim)
	if claim.NatsURL != "nats://bus.example:4222" || claim.Username != "site-a" || claim.Password != enroll.Token {
		t.Fatalf("claim credential = %+v, want the advertised url + name + token", claim)
	}

	// The claim marked the outpost enrolled.
	var outpost struct {
		Name     string `json:"name"`
		Enrolled bool   `json:"enrolled"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a", nil, http.StatusOK), &outpost)
	if !outpost.Enrolled {
		t.Fatalf("outpost not marked enrolled after claim")
	}

	// The outpost read serves the count of its batches the confinement fence
	// orphaned (#1200), zero and absent-time until one is, then both.
	var orphans struct {
		OrphanedBatches int64      `json:"orphaned_batches"`
		LastOrphanedAt  *time.Time `json:"last_orphaned_at"`
	}
	raw := c.do(ownerTok, http.MethodGet, "/outposts/site-a", nil, http.StatusOK)
	if !strings.Contains(string(raw), `"orphaned_batches":0`) {
		t.Fatalf("a fresh outpost's read carries no orphaned_batches 0: %s", raw)
	}
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if err := gw.RecordOrphanedBatches(ctx, "site-a", 7, at); err != nil {
		t.Fatalf("record orphans: %v", err)
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a", nil, http.StatusOK), &orphans)
	if orphans.OrphanedBatches != 7 || orphans.LastOrphanedAt == nil || !orphans.LastOrphanedAt.Equal(at) {
		t.Fatalf("outpost read after 7 orphans = %+v, want 7 at %s", orphans, at)
	}

	// A viewer (no outpost:create / outpost:enroll) is capability-forbidden.
	viewerTok := setupAllViewer(t, ctx, dsn, "viewer-1")
	c.do(viewerTok, http.MethodPost, "/outposts", map[string]any{"name": "site-b"}, http.StatusForbidden)
	c.do(viewerTok, http.MethodPost, "/outposts/site-a:enroll", nil, http.StatusForbidden)

	// N1: create carries label + location, and the create response mints no token.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, scope.Set{All: true}); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	created := c.do(ownerTok, http.MethodPost, "/outposts", map[string]any{"name": "site-c", "label": "Site C", "location": "hq"}, http.StatusCreated)
	if strings.Contains(string(created), `"token"`) {
		t.Fatalf("create must not mint a token: %s", created)
	}
	var cbody struct {
		Label    string  `json:"label"`
		Location *string `json:"location"`
	}
	json.Unmarshal(created, &cbody)
	if cbody.Label != "Site C" || cbody.Location == nil || *cbody.Location != "hq" {
		t.Fatalf("create identity: got %+v", cbody)
	}

	// N1: PATCH the label and clear the location; the name stays immutable.
	var patched struct {
		Name     string  `json:"name"`
		Label    string  `json:"label"`
		Location *string `json:"location"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/outposts/site-c", map[string]any{"label": "Site C prod", "location": ""}, http.StatusOK), &patched)
	if patched.Name != "site-c" || patched.Label != "Site C prod" || patched.Location != nil {
		t.Fatalf("patch: got %+v", patched)
	}
	// An unknown location is a 422, not a silent apply.
	c.do(ownerTok, http.MethodPatch, "/outposts/site-c", map[string]any{"location": "ghost"}, http.StatusUnprocessableEntity)
	// outpost:update is gated: the viewer cannot patch.
	c.do(viewerTok, http.MethodPatch, "/outposts/site-c", map[string]any{"label": "x"}, http.StatusForbidden)

	// N2: outpost is a taggable owner kind. A key that applies to outposts binds via
	// :setTag (outpost:update), reads back on :listTags and in the outpost's effective_tags,
	// and unbinds via :removeTag; the viewer is gated out of the write.
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "environment", Propagates: true}, scope.Set{All: true}); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/outposts/site-a:setTag", map[string]any{"key": "environment", "value": "prod"}, http.StatusOK)
	var tagsList struct {
		Tags []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a:listTags", nil, http.StatusOK), &tagsList)
	if len(tagsList.Tags) != 1 || tagsList.Tags[0].Key != "environment" {
		t.Fatalf("outpost direct tags = %+v, want environment", tagsList.Tags)
	}
	var withTags struct {
		EffectiveTags map[string]string `json:"effective_tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a", nil, http.StatusOK), &withTags)
	if withTags.EffectiveTags["environment"] != "prod" {
		t.Fatalf("outpost effective_tags = %+v, want environment=prod", withTags.EffectiveTags)
	}
	// outpost:update gates the tag write: the viewer is forbidden.
	c.do(viewerTok, http.MethodPost, "/outposts/site-a:setTag", map[string]any{"key": "environment", "value": "x"}, http.StatusForbidden)
	// Unbind clears it (a fresh var: effective_tags is omitempty, so an empty map is
	// absent from the JSON and would not overwrite a reused struct's stale map).
	c.do(ownerTok, http.MethodPost, "/outposts/site-a:removeTag", map[string]any{"key": "environment"}, http.StatusNoContent)
	var afterUnbind struct {
		EffectiveTags map[string]string `json:"effective_tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a", nil, http.StatusOK), &afterUnbind)
	if _, ok := afterUnbind.EffectiveTags["environment"]; ok {
		t.Fatalf("environment still effective after unbind")
	}

	// N3: outpost decommission. outpost:delete gates it (the viewer is forbidden); the
	// owner deletes and the outpost is then a 404.
	c.do(viewerTok, http.MethodDelete, "/outposts/site-a", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/outposts/site-a", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/outposts/site-a", nil, http.StatusNotFound)
	// Deleting an unknown outpost is a 404.
	c.do(ownerTok, http.MethodDelete, "/outposts/ghost", nil, http.StatusNotFound)
}

// TestOutpostCreateWithAmbiguousLocationIs404 closes I1: outpostLocationID resolves
// the create/patch "location" field with the scope-blind scopedByName (correct
// here, since outpost writes require an all scope by design, so there is no
// narrower caller scope to filter against), and on ambiguity that returns the
// raw *storage.ErrAmbiguousName, untranslated, up through CreateOutpost and
// UpdateOutpost. Before mapOutpostErr grew its mapRefErr check that error fell
// through to the handler's own default, an unmapped 500, exactly like
// mapInterfaceErr's component field before its own fix. Two locations sharing
// a name is legal under #627 (one root, one nested), so this is a routine
// state, not a corrupted one, and must 404 (#1134), not 500. Skipped under -short.
func TestOutpostCreateWithAmbiguousLocationIs404(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

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

	all := scope.Set{All: true}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "holder", LocationType: "campus"}, all); err != nil {
		t.Fatalf("seed holder location: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "dup-loc", LocationType: "campus"}, all); err != nil {
		t.Fatalf("seed root dup-loc: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "dup-loc", LocationType: "room", ParentName: ptr("holder")}, all); err != nil {
		t.Fatalf("seed nested dup-loc: %v", err)
	}

	status, body := c.send(ownerTok, http.MethodPost, "/outposts", map[string]any{"name": "site-d", "location": "dup-loc"})
	if status != http.StatusNotFound {
		t.Fatalf("create outpost with ambiguous location status = %d, want 404\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "dup-loc") {
		t.Fatalf("404 body = %s, want it to name the ambiguous reference", body)
	}

	// Same refusal on the patch path (UpdateOutpost calls the same resolver).
	c.do(ownerTok, http.MethodPost, "/outposts", map[string]any{"name": "site-e"}, http.StatusCreated)
	status, body = c.send(ownerTok, http.MethodPatch, "/outposts/site-e", map[string]any{"location": "dup-loc"})
	if status != http.StatusNotFound {
		t.Fatalf("patch outpost with ambiguous location status = %d, want 404\nbody: %s", status, body)
	}
}
