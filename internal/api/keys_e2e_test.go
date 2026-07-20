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
)

// TestKeyAPI drives the /keys catalog over HTTP: an owner registers, reads,
// updates, and deletes a custom key; an official (seeded) key is read-only; a
// malformed name and a duplicate are rejected; and an ungranted principal is
// forbidden to create.
func TestKeyAPI(t *testing.T) {
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

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Register a custom key.
	created := c.do(ownerTok, http.MethodPost, "/keys", map[string]any{
		"name": "rack_unit", "data_type": "int", "display_name": "Rack unit",
		"validation": map[string]any{"minimum": 1, "maximum": 48},
	}, http.StatusCreated)
	var k struct {
		Name     string `json:"name"`
		DataType string `json:"data_type"`
		Official bool   `json:"official"`
	}
	json.Unmarshal(created, &k)
	if k.Name != "rack_unit" || k.DataType != "int" || k.Official {
		t.Fatalf("created = %+v", k)
	}

	// Get it back.
	c.do(ownerTok, http.MethodGet, "/keys/rack_unit", nil, http.StatusOK)

	// List includes the custom key and the seeded official ones.
	var listed struct {
		Keys []struct {
			Name string `json:"name"`
		} `json:"keys"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/keys", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, kk := range listed.Keys {
		names[kk.Name] = true
	}
	if !names["rack_unit"] || !names["serial_number"] || !names["icmp.reachable"] {
		t.Fatalf("list missing keys: %v", names)
	}

	// Update a mutable field.
	c.do(ownerTok, http.MethodPatch, "/keys/rack_unit", map[string]any{"display_name": "Rack Unit (U)"}, http.StatusOK)

	// A malformed name is a 422.
	c.do(ownerTok, http.MethodPost, "/keys", map[string]any{"name": "Bad-Name", "data_type": "string"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/keys", map[string]any{"name": "rack_unit", "data_type": "int"}, http.StatusConflict)

	// An official (seeded) key is read-only (409).
	c.do(ownerTok, http.MethodPatch, "/keys/serial_number", map[string]any{"display_name": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/keys/serial_number", nil, http.StatusConflict)

	// An unknown key is a 404.
	c.do(ownerTok, http.MethodGet, "/keys/nope", nil, http.StatusNotFound)

	// Delete the custom key.
	c.do(ownerTok, http.MethodDelete, "/keys/rack_unit", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "nokeys", nil)
	c.do(noneTok, http.MethodPost, "/keys", map[string]any{"name": "nope_key", "data_type": "string"}, http.StatusForbidden)
}
