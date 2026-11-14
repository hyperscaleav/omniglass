package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestPropertyAPI drives the /property-types catalog over HTTP: an owner registers, reads,
// updates, and deletes a custom property; an official (seeded) property is read-only; a
// malformed name and a duplicate are rejected; and an ungranted principal is
// forbidden to create.
func TestPropertyAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

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

	// Register a custom property. Numbers are the metric lane (#587), so the
	// property fixture is a string.
	created := c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{
		"name": "rack-unit", "data_type": "string", "label": "Rack unit",
		"validation": map[string]any{"pattern": "^u[0-9]+$"},
	}, http.StatusCreated)
	var p struct {
		Name     string `json:"name"`
		DataType string `json:"data_type"`
		Official bool   `json:"official"`
	}
	json.Unmarshal(created, &p)
	if p.Name != "rack-unit" || p.DataType != "string" || p.Official {
		t.Fatalf("created = %+v", p)
	}

	// Get it back.
	c.do(ownerTok, http.MethodGet, "/property-types/rack-unit", nil, http.StatusOK)

	// A numeric data type is refused by the enum: numbers register as metric types.
	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": "fan-rpm", "data_type": "int"}, http.StatusUnprocessableEntity)

	// List includes the custom property and the seeded official ones.
	var listed struct {
		Properties []struct {
			Name string `json:"name"`
		} `json:"properties"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/property-types", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, pp := range listed.Properties {
		names[pp.Name] = true
	}
	if !names["rack-unit"] || !names["serial-number"] || !names["endpoint-reachable"] {
		t.Fatalf("list missing properties: %v", names)
	}
	if !names["icmp-reachable"] || !names["tcp-open"] {
		t.Fatalf("probe results missing from the property lane (#1592): %v", names)
	}
	if names["icmp-rtt-avg"] {
		t.Fatalf("icmp-rtt-avg listed on the property lane; numbers are metric types")
	}

	// Update a mutable field.
	c.do(ownerTok, http.MethodPatch, "/property-types/rack-unit", map[string]any{"label": "Rack Unit (U)"}, http.StatusOK)

	// A malformed name is a 422.
	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": "Bad-Name", "data_type": "string"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": "rack-unit", "data_type": "string"}, http.StatusConflict)

	// An official (seeded) property is read-only (409).
	c.do(ownerTok, http.MethodPatch, "/property-types/serial-number", map[string]any{"label": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/property-types/serial-number", nil, http.StatusConflict)

	// An unknown property is a 404.
	c.do(ownerTok, http.MethodGet, "/property-types/nope", nil, http.StatusNotFound)

	// Delete the custom property.
	c.do(ownerTok, http.MethodDelete, "/property-types/rack-unit", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "noprops", nil)
	c.do(noneTok, http.MethodPost, "/property-types", map[string]any{"name": "nope_prop", "data_type": "string"}, http.StatusForbidden)
}

// A nullable JSON Schema object has no empty value to overload, so validation
// clears by being named in update_mask with no value (#1189, ADR-0106's rule
// for a nullable object). Omitting it leaves it, a value replaces it, and a
// mask naming other fields leaves it alone.
func TestPropertyTypeValidationClearsUnderUpdateMask(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{
		"name": "rack-temp", "data_type": "string",
		"validation": map[string]any{"type": "object"},
	}, http.StatusCreated)

	patch := func(body map[string]any) map[string]json.RawMessage {
		t.Helper()
		var got map[string]json.RawMessage
		if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/property-types/rack-temp", body, http.StatusOK), &got); err != nil {
			t.Fatalf("decode patch: %v", err)
		}
		return got
	}
	hasSchema := func(step string, got map[string]json.RawMessage, wantSchema string) {
		t.Helper()
		raw, ok := got["validation"]
		if wantSchema == "" {
			if ok && string(raw) != "null" {
				t.Fatalf("%s: validation = %s, want none", step, raw)
			}
			return
		}
		if !ok || !strings.Contains(string(raw), wantSchema) {
			t.Fatalf("%s: validation = %s, want it to carry %s", step, raw, wantSchema)
		}
	}

	hasSchema("omitted", patch(map[string]any{"label": "A label"}), `"object"`)
	hasSchema("replaced", patch(map[string]any{"validation": map[string]any{"type": "string"}}), `"string"`)
	// A mask naming another field leaves the schema, even though it is not in the body.
	hasSchema("mask names another field", patch(map[string]any{"label": "Again", "update_mask": []string{"label"}}), `"string"`)
	// Naming the field with no value clears it, on the response and on a re-read.
	hasSchema("cleared", patch(map[string]any{"update_mask": []string{"validation"}}), "")
	var reread map[string]json.RawMessage
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/property-types/rack-temp", nil, http.StatusOK), &reread)
	hasSchema("reread", reread, "")
	// A mask naming a field the route does not patch is a 422, never a no-op.
	c.do(ownerTok, http.MethodPatch, "/property-types/rack-temp",
		map[string]any{"update_mask": []string{"name"}}, http.StatusUnprocessableEntity)
}
