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

// TestPrincipalLifecycleAPI drives the deactivate / reactivate / purge endpoints
// against the real binary: the capability split (an operator can do neither, an
// admin can do both, purge is admin-sensitive), the deactivate-before-purge gate,
// and the soft-delete visibility (deactivated principals leave the directory but
// are still gettable by id). Skipped under -short.
func TestPrincipalLifecycleAPI(t *testing.T) {
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
	adminTok := principalWithGrants(t, ctx, dsn, "admin-all", []grant{{role: "admin", scopeKind: "all"}})
	opTok := principalWithGrants(t, ctx, dsn, "op-all", []grant{{role: "operator", scopeKind: "all"}})
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	newID := func(tok, username string) string {
		body := c.do(tok, "POST", "/principals", map[string]string{"username": username}, http.StatusCreated)
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

	// Capability: an operator (no principal:deactivate, no principal:purge:admin)
	// is refused both.
	c.do(opTok, "POST", "/principals/"+alice+":deactivate", nil, http.StatusForbidden)
	c.do(opTok, "POST", "/principals/"+bob+":purge", nil, http.StatusForbidden)

	// Purge is gated on deactivation: bob is live, so an admin's purge is a 409.
	c.do(adminTok, "POST", "/principals/"+bob+":purge", nil, http.StatusConflict)

	// Admin deactivates alice: she leaves the directory but is still gettable by id,
	// carrying deactivated_at.
	c.do(adminTok, "POST", "/principals/"+alice+":deactivate", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/principals", nil); bytes.Contains(body, []byte(`"alice"`)) {
		t.Fatalf("deactivated alice should be hidden from the directory: %s", body)
	}
	if code, body := c.send(ownerTok, "GET", "/principals/"+alice, nil); code != 200 || !bytes.Contains(body, []byte(`"deactivated_at"`)) {
		t.Fatalf("get deactivated alice: code %d body %s", code, body)
	}

	// Reactivate restores her to the directory.
	c.do(adminTok, "POST", "/principals/"+alice+":reactivate", nil, http.StatusNoContent)
	if _, body := c.send(ownerTok, "GET", "/principals", nil); !bytes.Contains(body, []byte(`"alice"`)) {
		t.Fatalf("reactivated alice should be back in the directory: %s", body)
	}

	// Admin deactivates then purges alice: gone, a get is a 404.
	c.do(adminTok, "POST", "/principals/"+alice+":deactivate", nil, http.StatusNoContent)
	c.do(adminTok, "POST", "/principals/"+alice+":purge", nil, http.StatusNoContent)
	if code, _ := c.send(ownerTok, "GET", "/principals/"+alice, nil); code != http.StatusNotFound {
		t.Fatalf("purged alice: want 404, got %d", code)
	}
}
