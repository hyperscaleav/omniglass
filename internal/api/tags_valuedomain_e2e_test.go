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

// TestTagValueDomainAPI drives the value-domain surface over HTTP: an enum key
// admits only its declared values (a non-member is a 422), a free key admits
// anything, and the values endpoint returns the distinct values in use.
func TestTagValueDomainAPI(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av", "system_type": "meeting-room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec", "component_type": "codec", "system": "av"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec2", "component_type": "codec", "system": "av"}, http.StatusCreated)

	// An enum key and a free-text key.
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "environment", "allowed_values": []string{"prod", "staging", "dev"}}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "note"}, http.StatusCreated)
	// A duplicate allowed value is a 422 at create.
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "bad", "allowed_values": []string{"x", "x"}}, http.StatusUnprocessableEntity)

	// The enum admits a member, rejects a non-member.
	setTag(c, ownerTok, "components", "codec", "environment", "prod", http.StatusOK)
	setTag(c, ownerTok, "components", "codec2", "environment", "staging", http.StatusOK)
	setTag(c, ownerTok, "components", "codec", "environment", "qa", http.StatusUnprocessableEntity)
	// The free key admits anything.
	setTag(c, ownerTok, "components", "codec", "note", "any free text", http.StatusOK)

	// The allowed set round-trips on the key list.
	var tags struct {
		Tags []struct {
			Name          string   `json:"name"`
			AllowedValues []string `json:"allowed_values"`
		} `json:"tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/tags", nil, http.StatusOK), &tags)
	for _, tg := range tags.Tags {
		if tg.Name == "environment" && len(tg.AllowedValues) != 3 {
			t.Errorf("environment allowed_values = %v, want 3", tg.AllowedValues)
		}
	}

	// The values endpoint returns the distinct values in use for the enum key.
	var vals struct {
		Values []string `json:"values"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/tags/environment:values", nil, http.StatusOK), &vals)
	if len(vals.Values) != 2 || vals.Values[0] != "prod" || vals.Values[1] != "staging" {
		t.Errorf("distinct values = %v, want [prod staging]", vals.Values)
	}
	// An unknown key is a 404.
	c.do(ownerTok, http.MethodGet, "/tags/ghost:values", nil, http.StatusNotFound)
}
