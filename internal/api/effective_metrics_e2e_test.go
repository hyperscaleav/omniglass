package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// effectiveMetricWire is the decoded effective-metric line: the contract facts
// beside the resolved value, the metric twin of effectivePropertyWire with the
// sample side read from the metric series.
type effectiveMetricWire struct {
	MetricTypeName string          `json:"metric_type_name"`
	DataType       string          `json:"data_type"`
	Required       bool            `json:"required"`
	IsSampled      bool            `json:"is_sampled"`
	FromContract   bool            `json:"from_contract"`
	DefaultValue   json.RawMessage `json:"default_value"`
	SampleValue    json.RawMessage `json:"sample_value"`
	Value          json.RawMessage `json:"value"`
	Instance       *string         `json:"instance"`
	InstanceCount  int             `json:"instance_count"`
	Instances      []struct {
		Instance string          `json:"instance"`
		Value    json.RawMessage `json:"value"`
		TS       time.Time       `json:"ts"`
	} `json:"instances"`
}

type componentMetricsWire struct {
	Component string                `json:"component"`
	Metrics   []effectiveMetricWire `json:"metrics"`
}

// TestComponentMetricsAPI drives the component effective-metric read over HTTP:
// the read resolves the product contract's default until a sample arrives, then
// the latest sample; an off-contract metric the series holds lands beside the
// contract ones marked from_contract=false; an unknown component is a
// non-disclosing 404. The system and location twins ride the same owner-generic
// resolver, smoke-checked here. Skipped under -short.
func TestComponentMetricsAPI(t *testing.T) {
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

	// A custom product declaring one metric with a default, and a component of it.
	c.do(ownerTok, http.MethodPost, "/products", map[string]any{
		"name": "acme-dsp", "label": "Acme DSP", "kind": "device", "component_type": "generic-device",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/products/acme-dsp/metrics/room-temperature",
		map[string]any{"default_value": 10.5, "required": true}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{
		"name": "dsp-1", "product": "acme-dsp",
	}, http.StatusCreated)

	// Unsampled: the contract default resolves.
	var got componentMetricsWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/dsp-1/metrics", nil, http.StatusOK), &got); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if got.Component != "dsp-1" || len(got.Metrics) != 1 {
		t.Fatalf("effective read = %+v, want dsp-1 with one metric", got)
	}
	m := got.Metrics[0]
	if m.MetricTypeName != "room-temperature" || !m.FromContract || m.IsSampled || string(m.Value) != `10.5` || !m.Required {
		t.Fatalf("unsampled line = %+v, want the contract default 10.5", m)
	}

	// A sample lands (through the gateway sink, the ingest path's write): the
	// series wins over the default, and an off-contract series shows beside it.
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "room-temperature", Instance: "", Value: 3.25, Source: "test"},
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "occupancy-count", Instance: "", Value: 42, Source: "test"},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/dsp-1/metrics", nil, http.StatusOK), &got); err != nil {
		t.Fatalf("decode metrics after samples: %v", err)
	}
	if len(got.Metrics) != 2 {
		t.Fatalf("effective read after samples = %+v, want 2 lines", got.Metrics)
	}
	for _, m := range got.Metrics {
		switch m.MetricTypeName {
		case "room-temperature":
			if !m.IsSampled || string(m.Value) != `3.25` || string(m.DefaultValue) != `10.5` {
				t.Errorf("sampled line = %+v, want 3.25 over default 10.5", m)
			}
		case "occupancy-count":
			if !m.IsSampled || m.FromContract || string(m.Value) != `42` {
				t.Errorf("ad-hoc line = %+v, want off-contract 42", m)
			}
		default:
			t.Errorf("unexpected line %+v", m)
		}
	}

	// An unknown component is a non-disclosing 404.
	c.do(ownerTok, http.MethodGet, "/components/ghost-x/metrics", nil, http.StatusNotFound)

	// The system and location twins answer on the same resolver.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "hq-huddle"}, http.StatusCreated)
	var sys struct {
		System  string                `json:"system"`
		Metrics []effectiveMetricWire `json:"metrics"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/hq-huddle/metrics", nil, http.StatusOK), &sys); err != nil {
		t.Fatalf("decode system metrics: %v", err)
	}
	if sys.System != "hq-huddle" || len(sys.Metrics) != 0 {
		t.Fatalf("standardless system = %+v, want an empty resolved set", sys)
	}
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq-campus", "location_type": "campus"}, http.StatusCreated)
	var loc struct {
		Location string                `json:"location"`
		Metrics  []effectiveMetricWire `json:"metrics"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/locations/hq-campus/metrics", nil, http.StatusOK), &loc); err != nil {
		t.Fatalf("decode location metrics: %v", err)
	}
	if loc.Location != "hq-campus" {
		t.Fatalf("location read = %+v, want hq-campus", loc)
	}
}

// TestComponentMetricsNameTheShownEndpoint drives the read a component's
// vitals row renders (#1020) over HTTP: a metric two endpoints collect shows
// the newest sample, names the endpoint it came from, counts both, and lists
// each endpoint's latest; a metric one endpoint collects names it and carries
// no list. Skipped under -short.
func TestComponentMetricsNameTheShownEndpoint(t *testing.T) {
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

	all := scope.Set{All: true}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	node := "edge"
	for _, ep := range []struct{ transport, target string }{{"http", "10.0.0.1:80"}, {"tcp", "10.0.0.1:1710"}} {
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
			Transport: ep.transport, Component: &comp.ID, Node: &node,
			Params: []byte(`{"target":"` + ep.target + `"}`),
		}, all); err != nil {
			t.Fatalf("create %s endpoint: %v", ep.transport, err)
		}
	}
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	for _, s := range []struct {
		ep, key string
		v       float64
		at      time.Duration
	}{
		{"http", "tcp-connect-time", 2.2, time.Second},
		{"tcp", "tcp-connect-time", 4.8, 2 * time.Second},
		{"tcp", "tcp-open", 1, time.Second},
	} {
		if err := gw.InsertMetricSamples(ctx, storage.NodeWriter(comp.ID, s.ep), []storage.MetricSampleWrite{{
			OwnerKind: "component", OwnerID: comp.ID, Key: s.key, Instance: s.ep, Value: s.v, Source: "test", TS: base.Add(s.at),
		}}); err != nil {
			t.Fatalf("observe %s on %s: %v", s.key, s.ep, err)
		}
	}

	var got componentMetricsWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/dsp-1/metrics", nil, http.StatusOK), &got); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	byName := map[string]effectiveMetricWire{}
	for _, m := range got.Metrics {
		byName[m.MetricTypeName] = m
	}
	two := byName["tcp-connect-time"]
	if string(two.Value) != `4.8` || two.Instance == nil || *two.Instance != "tcp" || two.InstanceCount != 2 {
		t.Fatalf("two endpoints: want 4.8 from tcp of 2, got %+v", two)
	}
	if len(two.Instances) != 2 || two.Instances[0].Instance != "tcp" || two.Instances[1].Instance != "http" || string(two.Instances[1].Value) != `2.2` {
		t.Fatalf("two endpoints: want [tcp 4.8, http 2.2], got %+v", two.Instances)
	}
	one := byName["tcp-open"]
	if string(one.Value) != `1` || one.Instance == nil || *one.Instance != "tcp" || one.InstanceCount != 1 || one.Instances != nil {
		t.Fatalf("one endpoint: want 1 from tcp, count 1, no list, got %+v", one)
	}
}
