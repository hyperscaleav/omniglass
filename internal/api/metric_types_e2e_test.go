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

// TestMetricTypeAPI drives the /metric-types catalog over HTTP, the metric-lane
// mirror of TestPropertyAPI: an owner registers, reads, updates, and deletes a
// custom metric type; an official (seeded) one is read-only; a malformed name, a
// non-numeric data type, and a duplicate are rejected; and an ungranted principal
// is forbidden to create.
func TestMetricTypeAPI(t *testing.T) {
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

	// Register a custom metric type with its numeric facts.
	created := c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{
		"name": "fan-speed", "data_type": "int", "display_name": "Fan speed",
		"unit": "rpm", "precision": 0,
	}, http.StatusCreated)
	var mt struct {
		Name      string `json:"name"`
		DataType  string `json:"data_type"`
		Unit      string `json:"unit"`
		Precision *int   `json:"precision"`
		Official  bool   `json:"official"`
	}
	json.Unmarshal(created, &mt)
	if mt.Name != "fan-speed" || mt.DataType != "int" || mt.Unit != "rpm" || mt.Precision == nil || *mt.Precision != 0 || mt.Official {
		t.Fatalf("created = %+v", mt)
	}

	// Get it back.
	c.do(ownerTok, http.MethodGet, "/metric-types/fan-speed", nil, http.StatusOK)

	// List includes the custom type and the seeded official ones.
	var listed struct {
		MetricTypes []struct {
			Name string `json:"name"`
		} `json:"metric_types"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/metric-types", nil, http.StatusOK), &listed)
	names := map[string]bool{}
	for _, m := range listed.MetricTypes {
		names[m.Name] = true
	}
	if !names["fan-speed"] || !names["icmp-rtt-avg"] || !names["tcp-open"] {
		t.Fatalf("list missing metric types: %v", names)
	}

	// Update a mutable field.
	c.do(ownerTok, http.MethodPatch, "/metric-types/fan-speed", map[string]any{"display_name": "Fan Speed (RPM)"}, http.StatusOK)

	// A malformed name is a 422, and so is a non-numeric data type (the enum).
	c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{"name": "Bad-Name", "data_type": "int"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{"name": "power-state", "data_type": "string"}, http.StatusUnprocessableEntity)

	// A duplicate name is a 409.
	c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{"name": "fan-speed", "data_type": "int"}, http.StatusConflict)

	// An official (seeded) metric type is read-only (409).
	c.do(ownerTok, http.MethodPatch, "/metric-types/tcp-open", map[string]any{"display_name": "x"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/metric-types/tcp-open", nil, http.StatusConflict)

	// An unknown metric type is a 404.
	c.do(ownerTok, http.MethodGet, "/metric-types/nope", nil, http.StatusNotFound)

	// Delete the custom type.
	c.do(ownerTok, http.MethodDelete, "/metric-types/fan-speed", nil, http.StatusNoContent)

	// An ungranted principal is forbidden to create.
	noneTok := principalWithGrants(t, ctx, dsn, "nometrics", nil)
	c.do(noneTok, http.MethodPost, "/metric-types", map[string]any{"name": "nope-metric", "data_type": "int"}, http.StatusForbidden)
}
