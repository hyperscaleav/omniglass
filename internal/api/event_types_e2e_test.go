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

// TestEventTypeAPI drives the /event-types catalog over HTTP: an owner registers,
// reads, updates, and deletes a custom event type; an official (seeded) event type is
// read-only; a malformed name and a duplicate are rejected; an ungranted principal is
// forbidden to create. Mirrors the property_type catalog e2e.
func TestEventTypeAPI(t *testing.T) {
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

	// Register a custom event type.
	created := c.do(ownerTok, http.MethodPost, "/event-types", map[string]any{
		"name": "cable.unplugged", "display_name": "Cable unplugged",
		"payload_schema": map[string]any{"type": "object"},
	}, http.StatusCreated)
	var et struct {
		Name     string `json:"name"`
		Official bool   `json:"official"`
	}
	json.Unmarshal(created, &et)
	if et.Name != "cable.unplugged" || et.Official {
		t.Fatalf("created = %+v", et)
	}

	// Get it back.
	c.do(ownerTok, http.MethodGet, "/event-types/cable.unplugged", nil, http.StatusOK)

	// List includes the custom event type and the seeded official ones.
	var listed struct {
		EventTypes []struct {
			Name string `json:"name"`
		} `json:"event_types"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-types", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, e := range listed.EventTypes {
		names[e.Name] = true
	}
	if !names["cable.unplugged"] || !names["call.started"] {
		t.Fatalf("list missing event types: %v", names)
	}

	// Update a mutable field.
	c.do(ownerTok, http.MethodPatch, "/event-types/cable.unplugged", map[string]any{"display_name": "Cable Unplugged"}, http.StatusOK)

	// A malformed name is a 422.
	c.do(ownerTok, http.MethodPost, "/event-types", map[string]any{"name": "Bad-Name"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/event-types", map[string]any{"name": "cable.unplugged"}, http.StatusConflict)

	// An official (seeded) event type is read-only (409).
	c.do(ownerTok, http.MethodPatch, "/event-types/call.started", map[string]any{"display_name": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/event-types/call.started", nil, http.StatusConflict)

	// An unknown event type is a 404.
	c.do(ownerTok, http.MethodGet, "/event-types/nope", nil, http.StatusNotFound)

	// Delete the custom event type.
	c.do(ownerTok, http.MethodDelete, "/event-types/cable.unplugged", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "noevents", nil)
	c.do(noneTok, http.MethodPost, "/event-types", map[string]any{"name": "nope.event"}, http.StatusForbidden)
}
