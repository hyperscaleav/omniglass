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

// TestCommandTypeAPI drives the /command-types catalog over HTTP: an owner registers,
// reads, updates, and deletes a custom command type (with a settle window and a target
// property); an official (seeded) type is read-only; a malformed name, a duplicate, and
// an unknown target property are rejected; an ungranted principal is forbidden to
// create. Mirrors the property_type and event_type catalog e2e.
func TestCommandTypeAPI(t *testing.T) {
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

	// Register a custom, settleable command type targeting a seeded property.
	created := c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{
		"name": "set_volume", "display_name": "Set volume",
		"settle_window_seconds": 5, "target_property_type": "video.input",
	}, http.StatusCreated)
	var ct struct {
		Name                string `json:"name"`
		SettleWindowSeconds int    `json:"settle_window_seconds"`
		TargetPropertyType  string `json:"target_property_type"`
		Official            bool   `json:"official"`
	}
	json.Unmarshal(created, &ct)
	if ct.Name != "set_volume" || ct.SettleWindowSeconds != 5 || ct.TargetPropertyType != "video.input" || ct.Official {
		t.Fatalf("created = %+v", ct)
	}

	// Get it back; list includes the custom + the seeded official ones.
	c.do(ownerTok, http.MethodGet, "/command-types/set_volume", nil, http.StatusOK)
	var listed struct {
		CommandTypes []struct {
			Name string `json:"name"`
		} `json:"command_types"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/command-types", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, e := range listed.CommandTypes {
		names[e.Name] = true
	}
	if !names["set_volume"] || !names["set_input"] || !names["reboot"] {
		t.Fatalf("list missing command types: %v", names)
	}

	// Update the settle window.
	c.do(ownerTok, http.MethodPatch, "/command-types/set_volume", map[string]any{"settle_window_seconds": 10}, http.StatusOK)

	// A malformed name is a 422; an unknown target property is a 422.
	c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{"name": "Bad-Name"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{"name": "set_thing", "target_property_type": "no.such.property"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/command-types", map[string]any{"name": "set_volume"}, http.StatusConflict)

	// An official (seeded) command type is read-only.
	c.do(ownerTok, http.MethodPatch, "/command-types/set_input", map[string]any{"display_name": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/command-types/reboot", nil, http.StatusConflict)

	// An unknown command type is a 404.
	c.do(ownerTok, http.MethodGet, "/command-types/nope", nil, http.StatusNotFound)

	// Delete the custom command type.
	c.do(ownerTok, http.MethodDelete, "/command-types/set_volume", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "nocmds", nil)
	c.do(noneTok, http.MethodPost, "/command-types", map[string]any{"name": "nope_cmd"}, http.StatusForbidden)
}
