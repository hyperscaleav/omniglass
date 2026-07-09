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

// TestPrincipalAddressByUsername proves a principal route accepts a human username
// as well as a uuid (issue #163): getting a principal by username matches getting it
// by uuid, a :verb custom method (archive) works by username, and an unknown
// username is a 404, the same as an unknown id. Skipped under -short.
func TestPrincipalAddressByUsername(t *testing.T) {
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
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Create alice, capture her uuid.
	created := c.do(ownerTok, "POST", "/principals", map[string]string{
		"username": "alice", "password": "orange-boat-42x",
	}, http.StatusCreated)
	var made struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &made); err != nil || made.ID == "" {
		t.Fatalf("create alice: id missing (%v) in %s", err, created)
	}

	// Get by uuid and by username return the same principal.
	codeU, byUUID := c.send(ownerTok, "GET", "/principals/"+made.ID, nil)
	codeN, byName := c.send(ownerTok, "GET", "/principals/alice", nil)
	if codeU != 200 || codeN != 200 {
		t.Fatalf("get by uuid/username: codes %d/%d", codeU, codeN)
	}
	if !bytes.Equal(byUUID, byName) {
		t.Fatalf("get by username differs from by uuid:\n uuid: %s\n name: %s", byUUID, byName)
	}
	if !bytes.Contains(byName, []byte(`"username":"alice"`)) {
		t.Fatalf("get by username missing alice: %s", byName)
	}

	// A :verb custom method resolves the username too: archive alice by name.
	if code, body := c.send(ownerTok, "POST", "/principals/alice:archive", nil); code != http.StatusNoContent {
		t.Fatalf("archive by username: want 204, got %d (%s)", code, body)
	}
	// She now reads archived (get still resolves an archived principal by username).
	if code, body := c.send(ownerTok, "GET", "/principals/alice", nil); code != 200 || !bytes.Contains(body, []byte(`"archived_at"`)) {
		t.Fatalf("archived read by username: code %d body %s", code, body)
	}

	// An unknown username is a 404, the same as an unknown id.
	if code, _ := c.send(ownerTok, "GET", "/principals/nobody", nil); code != http.StatusNotFound {
		t.Fatalf("unknown username: want 404, got %d", code)
	}
}
