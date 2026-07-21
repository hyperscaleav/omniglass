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

// TestVariableAPI drives the variable surface over HTTP: an owner sets variables
// at several scopes and lists the all-scope directory, a scoped operator may set
// and edit but a viewer is forbidden create and the all-scope directory.
func TestVariableAPI(t *testing.T) {
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

	// Estate: a room in a building, a system, and a codec at both.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "bldg", "location_type": "building"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "room", "location_type": "room", "parent": "bldg"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys"}, http.StatusCreated)
	compRaw := c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "system": "sys", "location": "room"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(compRaw, &comp)

	// Set "poll" at the platform tier, room, and the component; distinct values.
	c.do(ownerTok, http.MethodPost, "/variables", varReq("poll", "int", "platform", "", 10), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/variables", varReq("poll", "int", "location", "room", 20), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/variables", varReq("poll", "int", "component", "codec-1", 30), http.StatusCreated)
	// A non-int value for an int variable is a 422.
	c.do(ownerTok, http.MethodPost, "/variables", varReq("bad", "int", "platform", "", "not-int"), http.StatusUnprocessableEntity)
	// An unknown value_type is a 422.
	c.do(ownerTok, http.MethodPost, "/variables", map[string]any{"name": "x", "value_type": "date", "owner_kind": "platform", "value": "y"}, http.StatusUnprocessableEntity)
	// Duplicate at the same owner is a conflict.
	c.do(ownerTok, http.MethodPost, "/variables", varReq("poll", "int", "component", "codec-1", 99), http.StatusConflict)
	// An unknown owner is a 422.
	c.do(ownerTok, http.MethodPost, "/variables", varReq("poll", "int", "location", "ghost", 1), http.StatusUnprocessableEntity)

	// Owner directory lists all three.
	var listed struct {
		Variables []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			OwnerKind string `json:"owner_kind"`
		} `json:"variables"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/variables", nil, http.StatusOK), &listed)
	if len(listed.Variables) != 3 {
		t.Fatalf("owner list = %d, want 3", len(listed.Variables))
	}
	var compPollID string
	for _, v := range listed.Variables {
		if v.Name == "poll" && v.OwnerKind == "component" {
			compPollID = v.ID
		}
	}
	if compPollID == "" {
		t.Fatal("component poll not in list")
	}

	// Update the value; the round-trip shows the new value.
	var updated struct {
		Value any `json:"value"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/variables/"+compPollID, map[string]any{"value": 45}, http.StatusOK), &updated)
	if n, ok := updated.Value.(float64); !ok || n != 45 {
		t.Errorf("updated value = %v, want 45", updated.Value)
	}
	// An update that violates the fixed type is a 422.
	c.do(ownerTok, http.MethodPatch, "/variables/"+compPollID, map[string]any{"value": "nope"}, http.StatusUnprocessableEntity)

	// A component-scoped operator: may set and edit variables in its subtree, but
	// delete stays off the role (403). Reads within scope are fine via the cascade.
	opTok := setupScopedViewer(t, ctx, dsn, "operator-codec", "operator", "component", comp.ID)
	var opCreated struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.do(opTok, http.MethodPost, "/variables", varReq("op-poll", "int", "component", "codec-1", 7), http.StatusCreated), &opCreated)
	if opCreated.ID == "" {
		t.Fatal("operator create returned no id")
	}
	c.do(opTok, http.MethodPatch, "/variables/"+opCreated.ID, map[string]any{"value": 8}, http.StatusOK)
	c.do(opTok, http.MethodDelete, "/variables/"+opCreated.ID, nil, http.StatusForbidden)

	// A component-scoped viewer: forbidden to create, to list the all-scope
	// directory, or to delete.
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-codec", "viewer", "component", comp.ID)
	c.do(viewerTok, http.MethodPost, "/variables", varReq("nope", "int", "component", "codec-1", 1), http.StatusForbidden)
	c.do(viewerTok, http.MethodGet, "/variables", nil, http.StatusForbidden)
	c.do(viewerTok, http.MethodDelete, "/variables/"+compPollID, nil, http.StatusForbidden)
}

// TestCreateVariableAtPlatformTierE2E drives the create route as an operator
// would and pins the renamed least-specific tier on the wire: "platform" is
// accepted and round-trips, and the retired "global" is refused by the enum
// before it can ever reach the check constraint.
func TestCreateVariableAtPlatformTierE2E(t *testing.T) {
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

	var created struct {
		OwnerKind string `json:"owner_kind"`
		OwnerID   string `json:"owner_id"`
	}
	raw := c.do(ownerTok, http.MethodPost, "/variables",
		varReq("snmp_community", "string", "platform", "", "public"), http.StatusCreated)
	json.Unmarshal(raw, &created)
	if created.OwnerKind != "platform" {
		t.Errorf("owner_kind = %q, want %q", created.OwnerKind, "platform")
	}
	if created.OwnerID != "" {
		t.Errorf("owner_id = %q, want empty for the platform singleton", created.OwnerID)
	}

	// The retired tier name is no longer a member of the enum.
	c.do(ownerTok, http.MethodPost, "/variables",
		varReq("legacy", "string", "global", "", "x"), http.StatusUnprocessableEntity)
}

func varReq(name, valueType, ownerKind, owner string, value any) map[string]any {
	body := map[string]any{
		"name":       name,
		"value_type": valueType,
		"owner_kind": ownerKind,
		"value":      value,
	}
	if owner != "" {
		body["owner"] = owner
	}
	return body
}
