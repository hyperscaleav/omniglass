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

// TestFieldDefinitionAPI drives the field-definition catalog over HTTP: an owner
// declares a field on a component_type (201), a duplicate name on the same type
// conflicts (409), an unknown component_type is a request fault (422), and the
// admin directory lists the one surviving definition.
func TestFieldDefinitionAPI(t *testing.T) {
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

	// Define a field on the "display" type.
	c.do(ownerTok, http.MethodPost, "/field-definitions",
		map[string]any{"component_type": "display", "name": "asset_tag", "data_type": "string"},
		http.StatusCreated)

	// A duplicate name on the same type conflicts.
	c.do(ownerTok, http.MethodPost, "/field-definitions",
		map[string]any{"component_type": "display", "name": "asset_tag", "data_type": "string"},
		http.StatusConflict)

	// An unknown component_type is a 422.
	c.do(ownerTok, http.MethodPost, "/field-definitions",
		map[string]any{"component_type": "nope", "name": "x", "data_type": "string"},
		http.StatusUnprocessableEntity)

	// The directory lists the one surviving definition.
	var listed struct {
		FieldDefinitions []struct {
			ID            string `json:"id"`
			ComponentType string `json:"component_type"`
			Name          string `json:"name"`
			DataType      string `json:"data_type"`
		} `json:"field_definitions"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/field-definitions", nil, http.StatusOK), &listed)
	if n := len(listed.FieldDefinitions); n != 1 {
		t.Fatalf("want 1 definition, got %d", n)
	}
	if fd := listed.FieldDefinitions[0]; fd.ComponentType != "display" || fd.Name != "asset_tag" || fd.DataType != "string" {
		t.Fatalf("listed definition = %+v, want display/asset_tag/string", fd)
	}
}
