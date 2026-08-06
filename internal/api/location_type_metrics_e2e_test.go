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

// locationTypeMetricWire is the decoded contract line: the metric the location
// type declares, its optional default, and whether a location of the type must
// carry it.
type locationTypeMetricWire struct {
	MetricTypeName string          `json:"metric_type_name"`
	DefaultValue   json.RawMessage `json:"default_value"`
	Required       bool            `json:"required"`
}

// locationTypeMetricsWire is the decoded list body.
type locationTypeMetricsWire struct {
	Metrics []locationTypeMetricWire `json:"metrics"`
}

// TestLocationTypeMetricsAPI drives the location type declared-metric contract
// over HTTP, the metric twin of TestLocationTypePropertiesAPI: a PUT declares a
// metric on a type, the GET lists it, a second PUT revises the same line in
// place (the upsert, not a duplicate), the DELETE withdraws it and a second
// DELETE is a 404. A metric the catalog does not know and a type that does not
// exist are request faults. Skipped under -short.
func TestLocationTypeMetricsAPI(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/location-types", map[string]any{
		"name": "annex", "display_name": "Annex", "allowed_parent_types": []string{"campus"},
	}, http.StatusCreated)

	// PUT declares the line. The metric must already exist in the catalog
	// (icmp-rtt-avg is seeded); the contract only names it.
	var set locationTypeMetricWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/location-types/annex/metrics/icmp-rtt-avg",
		map[string]any{"default_value": 5, "required": true}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode set: %v", err)
	}
	if set.MetricTypeName != "icmp-rtt-avg" || !set.Required || string(set.DefaultValue) != `5` {
		t.Fatalf("set = %+v, want icmp-rtt-avg required with default 5", set)
	}

	// GET lists the contract.
	var listed locationTypeMetricsWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/location-types/annex/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Metrics) != 1 || listed.Metrics[0].MetricTypeName != "icmp-rtt-avg" {
		t.Fatalf("contract = %+v, want one icmp-rtt-avg line", listed.Metrics)
	}

	// A second PUT revises the same line rather than adding another.
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/location-types/annex/metrics/icmp-rtt-avg",
		map[string]any{"default_value": 7.5}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode revise: %v", err)
	}
	if set.Required || string(set.DefaultValue) != `7.5` {
		t.Fatalf("revised = %+v, want required=false with default 7.5", set)
	}

	// DELETE withdraws the line; withdrawing it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/location-types/annex/metrics/icmp-rtt-avg", nil, http.StatusNoContent)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/location-types/annex/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed.Metrics) != 0 {
		t.Fatalf("contract after delete = %+v, want empty", listed.Metrics)
	}
	c.do(ownerTok, http.MethodDelete, "/location-types/annex/metrics/icmp-rtt-avg", nil, http.StatusNotFound)

	// A metric the catalog does not know, and a type that does not exist, are
	// request faults rather than 500s.
	c.do(ownerTok, http.MethodPut, "/location-types/annex/metrics/not-a-metric",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPut, "/location-types/no-such-type/metrics/icmp-rtt-avg",
		map[string]any{"required": true}, http.StatusNotFound)
}
