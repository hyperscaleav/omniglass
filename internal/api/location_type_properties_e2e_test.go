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

// locationTypePropertyWire is the decoded contract line: the property the
// location type declares, its optional default, and whether a location of the
// type must set it.
type locationTypePropertyWire struct {
	PropertyName string          `json:"property_name"`
	DefaultValue json.RawMessage `json:"default_value"`
	Required     bool            `json:"required"`
}

// locationTypePropertiesWire is the decoded list body.
type locationTypePropertiesWire struct {
	Properties []locationTypePropertyWire `json:"properties"`
}

// TestLocationTypePropertiesAPI drives the location type declared-property
// contract over HTTP: a PUT declares a property on a type, the GET lists it, a
// second PUT revises the same line in place (the upsert, not a duplicate), the
// DELETE withdraws it and a second DELETE is a 404. A property the catalog does
// not know and a type that does not exist are request faults. Skipped under
// -short.
func TestLocationTypePropertiesAPI(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/location-types", map[string]any{
		"name": "annex", "display_name": "Annex", "allowed_parent_types": []string{"campus"},
	}, http.StatusCreated)

	// PUT declares the line. The property must already exist in the catalog
	// (model_number is seeded); the contract only names it.
	var set locationTypePropertyWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/location-types/annex/properties/model_number",
		map[string]any{"default_value": "MN-UNSET", "required": true}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode set: %v", err)
	}
	if set.PropertyName != "model_number" || !set.Required || string(set.DefaultValue) != `"MN-UNSET"` {
		t.Fatalf("set = %+v, want model_number required with default \"MN-UNSET\"", set)
	}

	// GET lists the contract.
	var listed locationTypePropertiesWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/location-types/annex/properties", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Properties) != 1 || listed.Properties[0].PropertyName != "model_number" {
		t.Fatalf("contract = %+v, want one model_number line", listed.Properties)
	}

	// A second PUT revises the same line rather than adding another.
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/location-types/annex/properties/model_number",
		map[string]any{"default_value": "MN-REVISED"}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode revise: %v", err)
	}
	if set.Required || string(set.DefaultValue) != `"MN-REVISED"` {
		t.Fatalf("revised = %+v, want required=false with default \"MN-REVISED\"", set)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/location-types/annex/properties", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after revise: %v", err)
	}
	if len(listed.Properties) != 1 || string(listed.Properties[0].DefaultValue) != `"MN-REVISED"` {
		t.Fatalf("contract after revise = %+v, want one line with default \"MN-REVISED\"", listed.Properties)
	}

	// DELETE withdraws the line; withdrawing it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/location-types/annex/properties/model_number", nil, http.StatusNoContent)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/location-types/annex/properties", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed.Properties) != 0 {
		t.Fatalf("contract after delete = %+v, want empty", listed.Properties)
	}
	c.do(ownerTok, http.MethodDelete, "/location-types/annex/properties/model_number", nil, http.StatusNotFound)

	// A property the catalog does not know, and a type that does not exist, are
	// request faults rather than 500s.
	c.do(ownerTok, http.MethodPut, "/location-types/annex/properties/not_a_property",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPut, "/location-types/no-such-type/properties/model_number",
		map[string]any{"required": true}, http.StatusNotFound)
}
