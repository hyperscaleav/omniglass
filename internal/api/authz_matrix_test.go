package api_test

import (
	"context"
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

// grant is a (role x scope) pair for building test principals.
type grant struct {
	role      string
	scopeKind string
	scopeID   string // "" for the all scope
}

// TestAuthzScopeMatrix is the foundation security test: it drives the live API
// with realistically-granted principals and asserts the full authorization
// matrix end to end (grant -> role index -> per-action visible_set -> gateway
// 3-way split), the property every scoped surface depends on:
//
//   - capability fast-reject (403) when the action is in no grant;
//   - the over-permit fix: read-everywhere + write-narrow yields a SCOPE 403 on a
//     readable-but-out-of-write-scope target (not a 404, not a silent success);
//   - non-disclosing 404 when the target is outside the read scope entirely;
//   - success only inside the action scope;
//   - the read/act asymmetry (can read what you cannot write, never the reverse).
func TestAuthzScopeMatrix(t *testing.T) {
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

	// A custom write-only role: location create/update/delete, no inheritance.
	// Its holder also gets location:read via the :read floor, scoped to its
	// grant; it does NOT read other resources or other locations.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into role (id, official, permissions, inherits) values ('loc-writer', false, $1, '{}')`,
		[]string{"location:create,update,delete"}); err != nil {
		t.Fatalf("insert loc-writer role: %v", err)
	}
	conn.Close(ctx)

	// Owner builds the tree before any request (so the role index, built lazily
	// on first authn, includes loc-writer). hq (root) > hq-b1; lab (root).
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	c.create(ownerTok, locReq{Name: "hq", LocationType: "campus"}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "hq-b1", LocationType: "building", Parent: ptr("hq")}, http.StatusCreated)
	c.create(ownerTok, locReq{Name: "lab", LocationType: "campus"}, http.StatusCreated)
	hqID := c.list(ownerTok)[mustIndex(t, c.list(ownerTok), "hq")].ID

	// --- principal 1: viewer@all only (read everywhere, write nothing) --------
	readerTok := principalWithGrants(t, ctx, dsn, "reader", []grant{{role: "viewer", scopeKind: "all"}})
	c.get(readerTok, "lab", http.StatusOK)                                             // reads anything
	c.patch(readerTok, "hq-b1", patchReq{DisplayName: ptr("x")}, http.StatusForbidden) // capability 403 (no update anywhere)

	// --- principal 2: write-only, scoped to hq only (no @all) -----------------
	// Read scope = hq subtree (via the floor); lab is outside it.
	hqWriterTok := principalWithGrants(t, ctx, dsn, "hq-writer", []grant{{role: "loc-writer", scopeKind: "location", scopeID: hqID}})
	c.patch(hqWriterTok, "hq-b1", patchReq{DisplayName: ptr("ok")}, http.StatusOK)    // in write scope
	c.get(hqWriterTok, "lab", http.StatusNotFound)                                    // outside read scope -> non-disclosing 404
	c.patch(hqWriterTok, "lab", patchReq{DisplayName: ptr("x")}, http.StatusNotFound) // out of read scope -> 404, not 403

	// --- principal 3: viewer@all + loc-writer@hq (the over-permit case) -------
	// Reads everywhere (viewer@all); writes only under hq (loc-writer@hq).
	mixedTok := principalWithGrants(t, ctx, dsn, "mixed", []grant{
		{role: "viewer", scopeKind: "all"},
		{role: "loc-writer", scopeKind: "location", scopeID: hqID},
	})
	c.get(mixedTok, "lab", http.StatusOK)                                        // readable (viewer@all)
	c.patch(mixedTok, "hq-b1", patchReq{DisplayName: ptr("ok2")}, http.StatusOK) // write in scope (under hq)
	// The crown jewel: lab is READABLE (viewer@all) but OUTSIDE the write scope
	// (loc-writer@hq) -> 403 scope, NOT 404, NOT a silent success. This is the
	// over-permit fix: the read grant must not widen the write set.
	c.patch(mixedTok, "lab", patchReq{DisplayName: ptr("nope")}, http.StatusForbidden)
}

// bootstrapOwnerTok mints an owner and returns its bearer token.
func bootstrapOwnerTok(t *testing.T, ctx context.Context, gw storage.Gateway) string {
	t.Helper()
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return tok
}

// principalWithGrants creates a service principal with a bearer credential and
// the given grants, returning its token.
func principalWithGrants(t *testing.T, ctx context.Context, dsn, label string, grants []grant) string {
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
	for _, g := range grants {
		var scopeID any
		if g.scopeID != "" {
			scopeID = g.scopeID
		}
		if _, err := conn.Exec(ctx,
			`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, $2, $3, $4)`,
			pid, g.role, g.scopeKind, scopeID); err != nil {
			t.Fatalf("insert grant %+v: %v", g, err)
		}
	}
	return tok
}
