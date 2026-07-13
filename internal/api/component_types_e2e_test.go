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

func TestComponentTypesAPI(t *testing.T) {
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
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The seeded official types list under type:read.
	out := c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
	var body struct {
		ComponentTypes []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"component_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.ComponentTypes) == 0 {
		t.Fatalf("component_types empty, want seeded rows")
	}

	// CRUD a custom type; in-use delete refused.
	c.do(ownerTok, http.MethodPost, "/component-types",
		map[string]any{"id": "relay", "display_name": "Relay", "rank": 15}, http.StatusCreated)
	// Duplicate id is a 409, exercising the shared mapTypeErr ErrTypeExists branch.
	c.do(ownerTok, http.MethodPost, "/component-types",
		map[string]any{"id": "relay", "display_name": "Dup"}, http.StatusConflict)
	c.do(ownerTok, http.MethodPatch, "/component-types/relay",
		map[string]any{"display_name": "Relay Switch"}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/components",
		map[string]any{"name": "r1", "component_type": "relay"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/component-types/relay", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/components/r1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/component-types/relay", nil, http.StatusNoContent)

	// Official rows are read-only (422 on update and delete).
	c.do(ownerTok, http.MethodPatch, "/component-types/display",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/component-types/display", nil, http.StatusUnprocessableEntity)

	// Unknown id is a 404.
	c.do(ownerTok, http.MethodDelete, "/component-types/nope", nil, http.StatusNotFound)
}
