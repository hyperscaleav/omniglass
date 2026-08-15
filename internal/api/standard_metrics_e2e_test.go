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

// standardMetricWire is the decoded contract line: the metric the standard
// declares, its optional default, and whether a conforming system must carry it.
type standardMetricWire struct {
	MetricTypeName string          `json:"metric_type_name"`
	DefaultValue   json.RawMessage `json:"default_value"`
	Required       bool            `json:"required"`
}

// standardMetricsWire is the decoded list body.
type standardMetricsWire struct {
	Metrics []standardMetricWire `json:"metrics"`
}

// TestStandardMetricsAPI drives the standard declared-metric contract over
// HTTP, the metric twin of TestStandardPropertiesAPI: a PUT declares a metric
// on a standard, the GET lists it, a second PUT revises the same line in place
// (the upsert, not a duplicate), the DELETE withdraws it and a second DELETE is
// a 404. A metric the catalog does not know and a standard that does not exist
// are request faults. Skipped under -short.
func TestStandardMetricsAPI(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{
		"name": "acme-room", "label": "Acme Room",
	}, http.StatusCreated)

	// PUT declares the line. The metric must already exist in the catalog
	// (tcp-open is seeded); the contract only names it.
	var set standardMetricWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/standards/acme-room/metrics/tcp-open",
		map[string]any{"default_value": 1, "required": true}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode set: %v", err)
	}
	if set.MetricTypeName != "tcp-open" || !set.Required || string(set.DefaultValue) != `1` {
		t.Fatalf("set = %+v, want tcp-open required with default 1", set)
	}

	// GET lists the contract.
	var listed standardMetricsWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/acme-room/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Metrics) != 1 || listed.Metrics[0].MetricTypeName != "tcp-open" {
		t.Fatalf("contract = %+v, want one tcp-open line", listed.Metrics)
	}

	// A second PUT revises the same line rather than adding another.
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPut, "/standards/acme-room/metrics/tcp-open",
		map[string]any{"default_value": 0}, http.StatusOK), &set); err != nil {
		t.Fatalf("decode revise: %v", err)
	}
	if set.Required || string(set.DefaultValue) != `0` {
		t.Fatalf("revised = %+v, want required=false with default 0", set)
	}

	// DELETE withdraws the line; withdrawing it twice is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/standards/acme-room/metrics/tcp-open", nil, http.StatusNoContent)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/acme-room/metrics", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed.Metrics) != 0 {
		t.Fatalf("contract after delete = %+v, want empty", listed.Metrics)
	}
	c.do(ownerTok, http.MethodDelete, "/standards/acme-room/metrics/tcp-open", nil, http.StatusNotFound)

	// A metric the catalog does not know, and a standard that does not exist,
	// are request faults rather than 500s.
	c.do(ownerTok, http.MethodPut, "/standards/acme-room/metrics/not-a-metric",
		map[string]any{"required": true}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPut, "/standards/no-such-standard/metrics/tcp-open",
		map[string]any{"required": true}, http.StatusNotFound)
}
