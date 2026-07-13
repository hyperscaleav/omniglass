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

func TestSystemTypesAPI(t *testing.T) {
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
	out := c.do(ownerTok, http.MethodGet, "/system-types", nil, http.StatusOK)
	var body struct {
		SystemTypes []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"system_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.SystemTypes) == 0 {
		t.Fatalf("system_types empty, want seeded rows")
	}

	// CRUD a custom type; in-use delete refused.
	c.do(ownerTok, http.MethodPost, "/system-types",
		map[string]any{"id": "kiosk", "display_name": "Kiosk", "rank": 15}, http.StatusCreated)
	// Duplicate id is a 409, exercising the shared mapTypeErr ErrTypeExists branch.
	c.do(ownerTok, http.MethodPost, "/system-types",
		map[string]any{"id": "kiosk", "display_name": "Dup"}, http.StatusConflict)
	c.do(ownerTok, http.MethodPatch, "/system-types/kiosk",
		map[string]any{"display_name": "Info Kiosk"}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"name": "k1", "system_type": "kiosk"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/system-types/kiosk", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/systems/k1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/system-types/kiosk", nil, http.StatusNoContent)
}
