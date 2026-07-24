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
)

// productPropertyWire is the decoded contract line: the property the product
// declares, its optional default, and whether an instance must set it.
type productPropertyWire struct {
	PropertyTypeName string          `json:"property_type_name"`
	DefaultValue     json.RawMessage `json:"default_value"`
	Required         bool            `json:"required"`
}

// productPropertiesWire is the decoded list body.
type productPropertiesWire struct {
	Properties []productPropertyWire `json:"properties"`
}

// TestProductPropertiesAPI drives the product declared-property contract over
// HTTP: a PUT declares a property on a custom product, the GET lists it, a second
// PUT revises the same line in place (the upsert, not a duplicate), the DELETE
// withdraws it and a second DELETE is a 404. The seeded official product's
// contract is seed-owned and refuses both writes with a 422. Skipped under -short.
func TestProductPropertiesAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Only a custom product's contract is operator-owned, so the surface under test
	// needs one of its own.
	c.do(ownerTok, http.MethodPost, "/products", map[string]any{
		"name": "acme-panel", "display_name": "Acme Panel", "kind": "device",
	}, http.StatusCreated)

	// PUT declares the line. The property must already exist in the catalog
	// (serial_number is seeded); the contract only names it.
	var set productPropertyWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/products/acme-panel/properties/serial_number",
		map[string]any{"default_value": "SN-UNSET", "required": true}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode set: %v", err)
	}
	if set.PropertyTypeName != "serial_number" || !set.Required || string(set.DefaultValue) != `"SN-UNSET"` {
		t.Fatalf("set = %+v, want serial_number required with default \"SN-UNSET\"", set)
	}

	// GET lists the contract.
	var listed productPropertiesWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-panel/properties", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Properties) != 1 || listed.Properties[0].PropertyTypeName != "serial_number" {
		t.Fatalf("contract = %+v, want one serial_number line", listed.Properties)
	}

	// A second PUT revises the same line rather than adding another.
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/products/acme-panel/properties/serial_number",
		map[string]any{"default_value": "SN-REVISED"}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode revise: %v", err)
	}
	if set.Required || string(set.DefaultValue) != `"SN-REVISED"` {
		t.Fatalf("revised = %+v, want required=false with default \"SN-REVISED\"", set)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-panel/properties", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after revise: %v", err)
	}
	if len(listed.Properties) != 1 || string(listed.Properties[0].DefaultValue) != `"SN-REVISED"` {
		t.Fatalf("contract after revise = %+v, want one line with default \"SN-REVISED\"", listed.Properties)
	}

	// DELETE withdraws the line; withdrawing it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/products/acme-panel/properties/serial_number", nil, http.StatusNoContent)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-panel/properties", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed.Properties) != 0 {
		t.Fatalf("contract after delete = %+v, want empty", listed.Properties)
	}
	c.do(ownerTok, http.MethodDelete, "/products/acme-panel/properties/serial_number", nil, http.StatusNotFound)

	// The seeded official product ships its contract with the release, so both
	// writes are refused (the official read-only rule, not a permission fault).
	c.do(ownerTok, http.MethodPut, "/products/cisco-room-bar/properties/serial_number",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/products/cisco-room-bar/properties/serial_number", nil, http.StatusUnprocessableEntity)

	// A property the catalog does not know is a request fault, not a 500.
	c.do(ownerTok, http.MethodPut, "/products/acme-panel/properties/not_a_property",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
}
