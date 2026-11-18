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

// TestUserLifecycleAPI drives the archive / restore / purge endpoints
// against the real binary: the capability split (an operator can do neither, an
// admin can do both, purge is admin-sensitive), the archive-before-purge gate,
// and the soft-delete visibility (archived users leave the directory but
// are still gettable by id). Skipped under -short.
func TestUserLifecycleAPI(t *testing.T) {
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
	adminTok := userWithGrants(t, ctx, dsn, "admin-all", []grant{{role: "admin", scopeKind: "all"}})
	opTok := userWithGrants(t, ctx, dsn, "op-all", []grant{{role: "operator", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	newID := func(tok, username string) string {
		body := c.do(tok, "POST", "/users", map[string]string{"username": username}, http.StatusCreated)
		var made struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &made); err != nil || made.ID == "" {
			t.Fatalf("create %s: %v (%s)", username, err, body)
		}
		return made.ID
	}
	alice := newID(ownerTok, "alice")
	bob := newID(ownerTok, "bob")

	// Capability: an operator (no user:archive, no user:purge:admin)
	// is refused both.
	c.do(opTok, "POST", "/users/"+alice+":archive", nil, http.StatusForbidden)
	c.do(opTok, "POST", "/users/"+bob+":purge", nil, http.StatusForbidden)

	// Purge is gated on archival: bob is live, so an admin's purge is a 409.
	c.do(adminTok, "POST", "/users/"+bob+":purge", nil, http.StatusConflict)

	// Admin archives alice: she leaves the directory but is still gettable by id,
	// carrying archived_at.
	c.do(adminTok, "POST", "/users/"+alice+":archive", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/users", nil); bytes.Contains(body, []byte(`"alice"`)) {
		t.Fatalf("archived alice should be hidden from the directory: %s", body)
	}
	if code, body := c.send(ownerTok, "GET", "/users/"+alice, nil); code != 200 || !bytes.Contains(body, []byte(`"archived_at"`)) {
		t.Fatalf("get archived alice: code %d body %s", code, body)
	}

	// Restore returns her to the directory.
	c.do(adminTok, "POST", "/users/"+alice+":restore", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/users", nil); !bytes.Contains(body, []byte(`"alice"`)) {
		t.Fatalf("restored alice should be back in the directory: %s", body)
	}

	// Admin archives then purges alice: gone, a get is a 404.
	c.do(adminTok, "POST", "/users/"+alice+":archive", nil, http.StatusNoContent)
	c.do(adminTok, "POST", "/users/"+alice+":purge", nil, http.StatusNoContent)
	if code, _ := c.send(ownerTok, "GET", "/users/"+alice, nil); code != http.StatusNotFound {
		t.Fatalf("purged alice: want 404, got %d", code)
	}
}
