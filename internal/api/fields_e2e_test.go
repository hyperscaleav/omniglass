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

type effectiveFieldResp struct {
	FieldID  string `json:"field_id"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Value    any    `json:"value"`
	SetValue any    `json:"set_value"`
	IsSet    bool   `json:"is_set"`
}

// TestFieldValueAPI drives the field-value surface over HTTP: a component reads
// its effective fields (default until set), an override flips the value and the
// is_set flag, update and delete round-trip the literal, and the ABAC split holds.
// Unlike the definition catalog these routes are scoped to the component: a
// component-scoped operator may set, a viewer may read the effective fields but is
// forbidden to set (403 at the field:create gate).
func TestFieldValueAPI(t *testing.T) {
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

	// A display component and a diagonal_inches field with a type-level default of 50.
	compRaw := c.do(ownerTok, http.MethodPost, "/components",
		map[string]any{"name": "lobby-display", "component_type": "display"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(compRaw, &comp)
	c.do(ownerTok, http.MethodPost, "/field-definitions",
		map[string]any{"component_type": "display", "name": "diagonal_inches", "data_type": "int", "default_value": 50},
		http.StatusCreated)

	// Effective read before any override: the default, unset.
	if f := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 50 || f.IsSet {
		t.Fatalf("want default 50 unset, got %+v", f)
	}

	// Set an override; the effective read now reports the set value.
	var setBody struct {
		ID    string `json:"id"`
		Value any    `json:"value"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "diagonal_inches", "value": 80}, http.StatusCreated), &setBody)
	if setBody.ID == "" || setBody.Value.(float64) != 80 {
		t.Fatalf("set response = %+v, want value 80 with id", setBody)
	}
	if f := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 80 || !f.IsSet {
		t.Fatalf("want set 80, got %+v", f)
	}

	// A second value for the same field on the same component is a conflict.
	c.do(ownerTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "diagonal_inches", "value": 90}, http.StatusConflict)
	// A value for a field not defined on the type is a request fault.
	c.do(ownerTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "nope", "value": 1}, http.StatusUnprocessableEntity)

	// Update the literal, revalidated against the fixed int type.
	var updated struct {
		Value any `json:"value"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/field-values/"+setBody.ID,
		map[string]any{"value": 60}, http.StatusOK), &updated)
	if updated.Value.(float64) != 60 {
		t.Fatalf("updated value = %v, want 60", updated.Value)
	}
	// An update that breaks the type is a 422.
	c.do(ownerTok, http.MethodPatch, "/field-values/"+setBody.ID,
		map[string]any{"value": "bright"}, http.StatusUnprocessableEntity)

	// Delete the override; the field reverts to its default, unset.
	c.do(ownerTok, http.MethodDelete, "/field-values/"+setBody.ID, nil, http.StatusNoContent)
	if f := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 50 || f.IsSet {
		t.Fatalf("after delete want default 50 unset, got %+v", f)
	}

	// The ABAC split. A component-scoped operator may set (its subtree contains the
	// component); a component-scoped viewer may read the effective fields but is
	// forbidden to set (403 at the field:create gate, before any handler runs).
	opTok := setupScopedViewer(t, ctx, dsn, "operator-display", "operator", "component", comp.ID)
	c.do(opTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "diagonal_inches", "value": 70}, http.StatusCreated)
	if f := effectiveField(t, c, opTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 70 || !f.IsSet {
		t.Fatalf("operator set = %+v, want 70 set", f)
	}

	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-display", "viewer", "component", comp.ID)
	c.do(viewerTok, http.MethodGet, "/components/lobby-display/fields", nil, http.StatusOK)
	c.do(viewerTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "diagonal_inches", "value": 10}, http.StatusForbidden)
}

// effectiveField reads a component's effective fields and returns the one named,
// failing the test if it is absent.
func effectiveField(t *testing.T, c *apiClient, tok, comp, name string) effectiveFieldResp {
	t.Helper()
	raw := c.do(tok, http.MethodGet, "/components/"+comp+"/fields", nil, http.StatusOK)
	var out struct {
		Fields []effectiveFieldResp `json:"fields"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode effective fields: %v", err)
	}
	for _, f := range out.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not in effective fields", name)
	return effectiveFieldResp{}
}
