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

// productMetricWire is the decoded contract line: the metric the product
// declares, its optional default, and whether an instance must carry it.
type productMetricWire struct {
	MetricTypeName string          `json:"metric_type_name"`
	DefaultValue   json.RawMessage `json:"default_value"`
	Required       bool            `json:"required"`
}

// productMetricsWire is the decoded list body.
type productMetricsWire struct {
	Metrics []productMetricWire `json:"metrics"`
}

// TestProductMetricsAPI drives the product declared-metric contract over HTTP,
// the metric twin of TestProductPropertiesAPI: a PUT declares a metric on a
// custom product, the GET lists it, a second PUT revises the same line in place
// (the upsert, not a duplicate), the DELETE withdraws it and a second DELETE is
// a 404. The seeded official product's contract is seed-owned and refuses both
// writes with a 422. Skipped under -short.
func TestProductMetricsAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
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

	// Only a custom product's contract is operator-owned, so the surface under test
	// needs one of its own.
	c.do(ownerTok, http.MethodPost, "/products", map[string]any{
		"name": "acme-panel", "label": "Acme Panel", "kind": "device", "component_type": "generic-device",
	}, http.StatusCreated)

	// PUT declares the line. The metric must already exist in the catalog
	// (icmp-rtt-avg is seeded); the contract only names it.
	var set productMetricWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/products/acme-panel/metrics/icmp-rtt-avg",
		map[string]any{"default_value": 10.5, "required": true}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode set: %v", err)
	}
	if set.MetricTypeName != "icmp-rtt-avg" || !set.Required || string(set.DefaultValue) != `10.5` {
		t.Fatalf("set = %+v, want icmp-rtt-avg required with default 10.5", set)
	}

	// GET lists the contract.
	var listed productMetricsWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-panel/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Metrics) != 1 || listed.Metrics[0].MetricTypeName != "icmp-rtt-avg" {
		t.Fatalf("contract = %+v, want one icmp-rtt-avg line", listed.Metrics)
	}

	// A second PUT revises the same line rather than adding another.
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/products/acme-panel/metrics/icmp-rtt-avg",
		map[string]any{"default_value": 20}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode revise: %v", err)
	}
	if set.Required || string(set.DefaultValue) != `20` {
		t.Fatalf("revised = %+v, want required=false with default 20", set)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-panel/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after revise: %v", err)
	}
	if len(listed.Metrics) != 1 || string(listed.Metrics[0].DefaultValue) != `20` {
		t.Fatalf("contract after revise = %+v, want one line with default 20", listed.Metrics)
	}

	// DELETE withdraws the line; withdrawing it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/products/acme-panel/metrics/icmp-rtt-avg", nil, http.StatusNoContent)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-panel/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed.Metrics) != 0 {
		t.Fatalf("contract after delete = %+v, want empty", listed.Metrics)
	}
	c.do(ownerTok, http.MethodDelete, "/products/acme-panel/metrics/icmp-rtt-avg", nil, http.StatusNotFound)

	// The seeded official product ships its contract with the release, so both
	// writes are refused (the official read-only rule, not a permission fault).
	c.do(ownerTok, http.MethodPut, "/products/cisco-room-bar/metrics/icmp-rtt-avg",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/products/cisco-room-bar/metrics/icmp-rtt-avg", nil, http.StatusUnprocessableEntity)

	// A metric the catalog does not know is a request fault, not a 500, and an
	// unknown product is a 404.
	c.do(ownerTok, http.MethodPut, "/products/acme-panel/metrics/not-a-metric",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPut, "/products/no-such-product/metrics/icmp-rtt-avg",
		map[string]any{"required": true}, http.StatusNotFound)
}
