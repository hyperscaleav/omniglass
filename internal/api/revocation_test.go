package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// meID returns the principal id behind a live token via /auth/me.
func meID(t *testing.T, c *apiClient, tok string) string {
	t.Helper()
	_, body := c.send(tok, "GET", "/auth/me", nil)
	var doc struct {
		Principal struct {
			ID string `json:"id"`
		} `json:"principal"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Principal.ID == "" {
		t.Fatalf("resolve /auth/me id: err=%v body=%s", err, body)
	}
	return doc.Principal.ID
}

// TestDisableRevokesLiveSessionAPI proves disable is instant, not just refused at
// next sign-in: a principal with a live bearer that is already authenticating is
// rejected on its very next request the moment it is disabled, because authn
// re-reads the principal (and its `active` flag) from Postgres on every call
// rather than trusting a cached session. Re-enabling restores the same token.
// Skipped under -short.
func TestDisableRevokesLiveSessionAPI(t *testing.T) {
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
	subjTok := principalWithGrants(t, ctx, dsn, "subject", []grant{{role: "viewer", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	subjID := meID(t, c, subjTok)

	// The live token works: a viewer@all lists the directory.
	if code, _ := c.send(subjTok, "GET", "/principals", nil); code != http.StatusOK {
		t.Fatalf("live token before disable: want 200, got %d", code)
	}
	// Disable the subject with a different admin's token.
	if code, _ := c.send(ownerTok, "POST", "/principals/"+subjID+":disable", nil); code != http.StatusNoContent {
		t.Fatalf("disable: want 204, got %d", code)
	}
	// The very next request on the same live token is rejected (401), not served
	// from a cached session.
	if code, _ := c.send(subjTok, "GET", "/principals", nil); code != http.StatusUnauthorized {
		t.Fatalf("disabled token next request: want 401, got %d", code)
	}
	// Re-enabling restores the same token immediately.
	if code, _ := c.send(ownerTok, "POST", "/principals/"+subjID+":enable", nil); code != http.StatusNoContent {
		t.Fatalf("enable: want 204, got %d", code)
	}
	if code, _ := c.send(subjTok, "GET", "/principals", nil); code != http.StatusOK {
		t.Fatalf("re-enabled token: want 200, got %d", code)
	}
}

// TestRevokeCutsScopeLiveSessionAPI proves de-scoping is instant: a principal that
// can write in scope loses that write on its very next request the moment the
// granting role is revoked, because scope is resolved from the principal's grants
// on every request. The read grant (viewer@all) is left intact, so the target is
// still readable: the revoke removes exactly the write, and the previously
// in-scope write becomes a capability 403, not a silent success. Skipped under
// -short.
func TestRevokeCutsScopeLiveSessionAPI(t *testing.T) {
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

	// A write-only location role, inserted before the first request builds the
	// lazy role index.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into role (id, official, permissions, inherits) values ('location-writer', false, $1, '{}')`,
		[]string{"location:create,update,delete"}); err != nil {
		conn.Close(ctx)
		t.Fatalf("insert location-writer role: %v", err)
	}
	conn.Close(ctx)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner builds root > child (the first owner request builds the role index).
	body := func(name, parent string) map[string]any {
		b := map[string]any{"name": name, "location_type": "campus"}
		if parent != "" {
			b["parent"] = parent
		}
		return b
	}
	c.do(ownerTok, "POST", "/locations", body("az-root", ""), http.StatusCreated)
	c.do(ownerTok, "POST", "/locations", body("az-child", "az-root"), http.StatusCreated)
	rootID := entityID(t, c, ownerTok, "/locations", "az-root")

	// Subject: viewer@all (read everywhere) + writer@root (write under root).
	subjTok := principalWithGrants(t, ctx, dsn, "subject", []grant{
		{role: "viewer", scopeKind: "all"},
		{role: "location-writer", scopeKind: "location", scopeID: rootID},
	})
	patch := map[string]any{"display_name": "x"}

	// The live token can write in scope.
	if code, _ := c.send(subjTok, "PATCH", "/locations/az-child", patch); code != http.StatusOK {
		t.Fatalf("in-scope write before revoke: want 200, got %d", code)
	}

	// Find and revoke the writer grant (leaving viewer@all).
	subjID := meID(t, c, subjTok)
	_, detail := c.send(ownerTok, "GET", "/principals/"+subjID, nil)
	var doc struct {
		Grants []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(detail, &doc); err != nil {
		t.Fatalf("decode subject grants: %v (body %s)", err, detail)
	}
	var writerGrantID string
	for _, g := range doc.Grants {
		if g.Role == "location-writer" {
			writerGrantID = g.ID
		}
	}
	if writerGrantID == "" {
		t.Fatalf("subject should hold a location-writer grant: %s", detail)
	}
	if code, _ := c.send(ownerTok, "DELETE", "/principals/"+subjID+"/grants/"+writerGrantID, nil); code != http.StatusNoContent {
		t.Fatalf("revoke writer grant: want 204, got %d", code)
	}

	// The very next write on the same live token is refused: the write capability
	// is gone (403), resolved from the now-smaller grant set, not a cache.
	if code, _ := c.send(subjTok, "PATCH", "/locations/az-child", patch); code != http.StatusForbidden {
		t.Fatalf("de-scoped write next request: want 403, got %d", code)
	}
	// The retained read grant still works: the revoke cut exactly the write.
	if code, _ := c.send(subjTok, "GET", "/locations/az-child", nil); code != http.StatusOK {
		t.Fatalf("read after revoke (viewer@all retained): want 200, got %d", code)
	}
}
