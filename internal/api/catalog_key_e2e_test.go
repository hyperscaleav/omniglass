package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// catalogKeyServer is a seeded server and its owner's token, for the catalog
// address tests (#1434).
func catalogKeyServer(t *testing.T) (*apiClient, string, *storage.PG) {
	t.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, seededDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	tok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	return &apiClient{t: t, ctx: ctx, base: srv.URL}, tok, gw
}

type catalogKeyRow struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Official  bool   `json:"official"`
	Label     string `json:"label"`
}

// Every catalog route addresses a key as {namespace}/{key} (#1434,
// ADR-0318): with og/x and local/x both held, each URL answers its own row,
// where the bare name reaches only the operator's; and the single-segment
// item route no longer exists.
func TestEachNamespaceOfANameIsReachableByItsOwnURL(t *testing.T) {
	t.Parallel()
	c, tok, gw := catalogKeyServer(t)
	c.do(tok, http.MethodPost, "/metric-types", map[string]any{"name": "twin-metric", "data_type": "float", "label": "Mine"}, http.StatusCreated)
	if err := gw.UpsertMetricType(c.ctx, storage.MetricType{Name: "twin-metric", DataType: "float", Label: "Shipped", Official: true}); err != nil {
		t.Fatalf("seed og/twin-metric: %v", err)
	}
	for _, tc := range []struct {
		ns, label string
		official  bool
	}{{"og", "Shipped", true}, {"local", "Mine", false}} {
		var row catalogKeyRow
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/metric-types/"+tc.ns+"/twin-metric", nil, http.StatusOK), &row); err != nil {
			t.Fatal(err)
		}
		if row.Namespace != tc.ns || row.Label != tc.label || row.Official != tc.official {
			t.Errorf("GET /metric-types/%s/twin-metric = %+v, want namespace %s label %s", tc.ns, row, tc.ns, tc.label)
		}
	}
	if status, _ := c.send(tok, http.MethodGet, "/metric-types/twin-metric", nil); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Errorf("GET /metric-types/twin-metric = %d, want 404 or 405: the single-segment item route is gone", status)
	}
	if status, body := c.send(tok, http.MethodGet, "/metric-types/acme/twin-metric", nil); status != http.StatusNotFound || !strings.Contains(string(body), "acme/twin-metric") {
		t.Errorf("GET /metric-types/acme/twin-metric = %d %s, want a 404 naming the address", status, body)
	}
}

// A uuid in the key segment resolves under its row's namespace and 404s
// under any other (#1434, ADR-0062 keeps uuid addressing).
func TestAUUIDKeyAnswersOnlyUnderItsRowsNamespace(t *testing.T) {
	t.Parallel()
	c, tok, _ := catalogKeyServer(t)
	var row catalogKeyRow
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/metric-types/og/icmp-rtt-avg", nil, http.StatusOK), &row); err != nil {
		t.Fatal(err)
	}
	var byID catalogKeyRow
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/metric-types/og/"+row.ID, nil, http.StatusOK), &byID); err != nil {
		t.Fatal(err)
	}
	if byID.ID != row.ID || byID.Name != "icmp-rtt-avg" {
		t.Errorf("GET /metric-types/og/<uuid> = %+v, want icmp-rtt-avg", byID)
	}
	c.do(tok, http.MethodGet, "/metric-types/local/"+row.ID, nil, http.StatusNotFound)
}

// A child route on a qualified parent reads the parent and the child each in
// its own namespace, and a parent whose key is a child collection's word never
// collides with the child route: the key is always the second segment.
func TestAChildRouteOnAQualifiedParentReadsItsOwnRows(t *testing.T) {
	t.Parallel()
	c, tok, _ := catalogKeyServer(t)
	for _, name := range []string{"acme-panel", "metrics"} {
		c.do(tok, http.MethodPost, "/products", map[string]any{
			"name": name, "label": "Product " + name, "kind": "device", "component_type": "generic-device",
		}, http.StatusCreated)
	}
	c.do(tok, http.MethodPut, "/products/local/acme-panel/metrics/og/room-temperature", map[string]any{"required": true}, http.StatusOK)
	var lines struct {
		Metrics []struct {
			MetricTypeName string `json:"metric_type_name"`
			Required       bool   `json:"required"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/products/local/acme-panel/metrics", nil, http.StatusOK), &lines); err != nil {
		t.Fatal(err)
	}
	if len(lines.Metrics) != 1 || lines.Metrics[0].MetricTypeName != "room-temperature" || !lines.Metrics[0].Required {
		t.Errorf("contract = %+v, want one required room-temperature line", lines.Metrics)
	}
	c.do(tok, http.MethodPut, "/products/local/acme-panel/metrics/local/room-temperature", map[string]any{"required": true}, http.StatusUnprocessableEntity)
	var product catalogKeyRow
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/products/local/metrics", nil, http.StatusOK), &product); err != nil {
		t.Fatal(err)
	}
	if product.Name != "metrics" || product.Label != "Product metrics" {
		t.Errorf("GET /products/local/metrics = %+v, want the product named metrics", product)
	}
}

// A create naming no namespace lands in local; one naming og is refused,
// since only the boot seed writes og (#1434).
func TestACreateLandsInLocalAndNeverInOg(t *testing.T) {
	t.Parallel()
	c, tok, _ := catalogKeyServer(t)
	var row catalogKeyRow
	if err := json.Unmarshal(c.do(tok, http.MethodPost, "/event-types", map[string]any{"name": "door-opened"}, http.StatusCreated), &row); err != nil {
		t.Fatal(err)
	}
	if row.Namespace != "local" {
		t.Errorf("created event type namespace = %q, want local", row.Namespace)
	}
	c.do(tok, http.MethodGet, "/event-types/local/door-opened", nil, http.StatusOK)
	if status, body := c.send(tok, http.MethodPost, "/vendors", map[string]any{"name": "shipped-acme", "label": "Acme", "namespace": "og"}); status != http.StatusUnprocessableEntity {
		t.Errorf("create naming og = %d %s, want 422", status, body)
	}
	c.do(tok, http.MethodGet, "/vendors/og/shipped-acme", nil, http.StatusNotFound)
	c.do(tok, http.MethodPost, "/vendors", map[string]any{"name": "named-local", "label": "Local", "namespace": "local"}, http.StatusCreated)
}
