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

// TestPropertyAPI drives the /property-types catalog over HTTP: an owner registers, reads,
// updates, and deletes a custom property; an official (seeded) property is read-only; a
// malformed name and a duplicate are rejected; and an ungranted principal is
// forbidden to create.
func TestPropertyAPI(t *testing.T) {
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

	// Register a custom property. Numbers are the metric lane (#587), so the
	// property fixture is a string.
	created := c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{
		"name": "rack-unit", "data_type": "string", "display_name": "Rack unit",
		"validation": map[string]any{"pattern": "^u[0-9]+$"},
	}, http.StatusCreated)
	var p struct {
		Name     string `json:"name"`
		DataType string `json:"data_type"`
		Official bool   `json:"official"`
	}
	json.Unmarshal(created, &p)
	if p.Name != "rack-unit" || p.DataType != "string" || p.Official {
		t.Fatalf("created = %+v", p)
	}

	// Get it back.
	c.do(ownerTok, http.MethodGet, "/property-types/rack-unit", nil, http.StatusOK)

	// A numeric data type is refused by the enum: numbers register as metric types.
	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": "fan-rpm", "data_type": "int"}, http.StatusUnprocessableEntity)

	// List includes the custom property and the seeded official ones.
	var listed struct {
		Properties []struct {
			Name string `json:"name"`
		} `json:"properties"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/property-types", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, pp := range listed.Properties {
		names[pp.Name] = true
	}
	if !names["rack-unit"] || !names["serial-number"] || !names["interface-reachable"] {
		t.Fatalf("list missing properties: %v", names)
	}
	if names["icmp-reachable"] {
		t.Fatalf("icmp-reachable listed on the property lane; numbers are metric types")
	}

	// Update a mutable field.
	c.do(ownerTok, http.MethodPatch, "/property-types/rack-unit", map[string]any{"display_name": "Rack Unit (U)"}, http.StatusOK)

	// A malformed name is a 422.
	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": "Bad-Name", "data_type": "string"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": "rack-unit", "data_type": "string"}, http.StatusConflict)

	// An official (seeded) property is read-only (409).
	c.do(ownerTok, http.MethodPatch, "/property-types/serial-number", map[string]any{"display_name": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/property-types/serial-number", nil, http.StatusConflict)

	// An unknown property is a 404.
	c.do(ownerTok, http.MethodGet, "/property-types/nope", nil, http.StatusNotFound)

	// Delete the custom property.
	c.do(ownerTok, http.MethodDelete, "/property-types/rack-unit", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "noprops", nil)
	c.do(noneTok, http.MethodPost, "/property-types", map[string]any{"name": "nope_prop", "data_type": "string"}, http.StatusForbidden)
}
