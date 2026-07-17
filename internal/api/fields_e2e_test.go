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

	// Define a field on the "display" type, with an optional human label.
	c.do(ownerTok, http.MethodPost, "/field-definitions",
		map[string]any{"component_type": "display", "name": "asset_tag", "display_name": "Asset tag", "data_type": "string"},
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
			DisplayName   string `json:"display_name"`
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
	if listed.FieldDefinitions[0].DisplayName != "Asset tag" {
		t.Fatalf("display_name = %q, want \"Asset tag\"", listed.FieldDefinitions[0].DisplayName)
	}
}

type effectiveFieldResp struct {
	FieldID      string `json:"field_id"`
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	Value        any    `json:"value"`
	SetValue     any    `json:"set_value"`
	DefaultValue any    `json:"default_value"`
	IsSet        bool   `json:"is_set"`
	ValueID      string `json:"value_id"`
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

	// Effective read before any override: the default, unset, with no value_id (the
	// surface has nothing to clear). default_value carries the type default so the
	// drill-in can render the type-default step of the chain.
	if f := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 50 || f.IsSet || f.ValueID != "" || f.DefaultValue.(float64) != 50 {
		t.Fatalf("want default 50 unset with empty value_id and default_value 50, got %+v", f)
	}

	// Set an override; the effective read now reports the set value and carries the
	// field_value id, so the surface can clear the override back to the default.
	var setBody struct {
		ID    string `json:"id"`
		Value any    `json:"value"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "diagonal_inches", "value": 80}, http.StatusCreated), &setBody)
	if setBody.ID == "" || setBody.Value.(float64) != 80 {
		t.Fatalf("set response = %+v, want value 80 with id", setBody)
	}
	// The effective value is now the override, but default_value still reports the
	// type default (the drill-in shows it as the shadowed type-default step).
	if f := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 80 || !f.IsSet || f.ValueID != setBody.ID || f.DefaultValue.(float64) != 50 {
		t.Fatalf("want set 80 with value_id %q and default_value 50, got %+v", setBody.ID, f)
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

	// Clear the override by the value_id the effective read carries (the UI clear
	// path, which sees the effective row, not the create response). The field
	// reverts to its default, unset.
	clearID := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches").ValueID
	if clearID == "" {
		t.Fatal("effective read carried no value_id to clear")
	}
	c.do(ownerTok, http.MethodDelete, "/field-values/"+clearID, nil, http.StatusNoContent)
	if f := effectiveField(t, c, ownerTok, "lobby-display", "diagonal_inches"); f.Value.(float64) != 50 || f.IsSet || f.ValueID != "" {
		t.Fatalf("after delete want default 50 unset with empty value_id, got %+v", f)
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

	// Create-path scope split. A principal that HOLDS field:create but is scoped to a
	// different component may not set a value on lobby-display: the component exists
	// but is outside the create scope, so the create path forbids (403). This differs
	// from the viewer above (a role-gate 403) and from the non-disclosing read path
	// (404 for an out-of-scope component), and mirrors the variable create path.
	otherRaw := c.do(ownerTok, http.MethodPost, "/components",
		map[string]any{"name": "other-display", "component_type": "display"}, http.StatusCreated)
	var other struct {
		ID string `json:"id"`
	}
	json.Unmarshal(otherRaw, &other)
	otherOpTok := setupScopedViewer(t, ctx, dsn, "operator-other", "operator", "component", other.ID)
	c.do(otherOpTok, http.MethodPost, "/components/lobby-display/fields",
		map[string]any{"field": "diagonal_inches", "value": 42}, http.StatusForbidden)
}

// TestFieldValueScopeSplit drives the read-then-action scope split on the field
// value mutation routes (PATCH, DELETE). A value owned by a component OUTSIDE the
// caller's read scope is the non-disclosing 404 (the caller cannot tell it from a
// nonexistent id); a value the caller can READ but not ACT on (a broader read grant,
// a narrower action grant) is the disclosing 403. This mirrors variableRowForAction.
func TestFieldValueScopeSplit(t *testing.T) {
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

	// A field on the display type and two display components: one in the caller's
	// subtree (in-scope), one outside it (out-scope), which owns the value under test.
	c.do(ownerTok, http.MethodPost, "/field-definitions",
		map[string]any{"component_type": "display", "name": "diagonal_inches", "data_type": "int", "default_value": 50},
		http.StatusCreated)
	var inComp, outComp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components",
		map[string]any{"name": "in-scope", "component_type": "display"}, http.StatusCreated), &inComp)
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components",
		map[string]any{"name": "out-scope", "component_type": "display"}, http.StatusCreated), &outComp)

	// The owner sets a value on the out-of-scope component; capture its field-value id.
	var fvID struct {
		ID string `json:"id"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/out-scope/fields",
		map[string]any{"field": "diagonal_inches", "value": 80}, http.StatusCreated), &fvID)

	// Out-of-read leg: an operator scoped to in-scope holds field:update, but the
	// value's owner (out-scope) is outside its read scope, so PATCH is the
	// non-disclosing 404, indistinguishable from a value that does not exist.
	opTok := setupScopedViewer(t, ctx, dsn, "operator-in", "operator", "component", inComp.ID)
	c.do(opTok, http.MethodPatch, "/field-values/"+fvID.ID,
		map[string]any{"value": 60}, http.StatusNotFound)

	// Same leg for DELETE. field:delete is admin/owner-only (operator lacks it), so
	// scope an admin to in-scope: it holds field:delete but out-scope is still outside
	// its read scope, so DELETE is also the non-disclosing 404 (not a role-gate 403).
	adminTok := setupScopedViewer(t, ctx, dsn, "admin-in", "admin", "component", inComp.ID)
	c.do(adminTok, http.MethodDelete, "/field-values/"+fvID.ID, nil, http.StatusNotFound)

	// In-read-not-action leg: a principal that READS out-scope (a viewer grant there)
	// but may only UPDATE in-scope (an operator grant there). The value's owner is now
	// readable, so the split discloses a 403 rather than hiding it as a 404.
	splitTok := setupScopedPrincipal(t, ctx, dsn, "reader-not-writer",
		grantSpec{role: "operator", scopeKind: "component", scopeID: inComp.ID},
		grantSpec{role: "viewer", scopeKind: "component", scopeID: outComp.ID})
	c.do(splitTok, http.MethodPatch, "/field-values/"+fvID.ID,
		map[string]any{"value": 60}, http.StatusForbidden)
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
