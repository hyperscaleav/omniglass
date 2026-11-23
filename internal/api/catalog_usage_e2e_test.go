package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestCatalogUsageFilters drives the "used by" reads the catalog drawers lean
// on (#2430): the fleet lists narrow by the catalog row that classifies them
// and report the whole match count beside a capped page, so a drawer can say
// "12 components" and show the first few without fetching the fleet; the
// product list narrows to the products that declare a metric or a property.
// Scope is untouched: the filters run over what the caller may already read.
// Skipped under -short.
func TestCatalogUsageFilters(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, seededDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	tok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	type comps struct {
		Components []struct {
			Name    string `json:"name"`
			Product string `json:"product"`
		} `json:"components"`
		Total int `json:"total"`
	}
	get := func(path string) comps {
		t.Helper()
		var out comps
		if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), &out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return out
	}

	// A product with a driver, found rather than assumed, so the seed may move.
	var prods struct {
		Products []struct {
			Name          string `json:"name"`
			Driver        string `json:"driver"`
			ComponentType string `json:"component_type"`
		} `json:"products"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/products", nil, http.StatusOK), &prods); err != nil {
		t.Fatalf("decode products: %v", err)
	}
	var driven, plain struct{ Name, Driver, Type string }
	for _, p := range prods.Products {
		if p.Driver != "" && driven.Name == "" {
			driven.Name, driven.Driver, driven.Type = p.Name, p.Driver, p.ComponentType
		}
		if p.Driver == "" && plain.Name == "" {
			plain.Name, plain.Type = p.Name, p.ComponentType
		}
	}
	if driven.Name == "" || plain.Name == "" {
		t.Fatalf("the seed needs one driven and one driverless product, got %+v", prods.Products)
	}

	for _, n := range []string{"use-a", "use-b", "use-c"} {
		c.do(tok, http.MethodPost, "/components", map[string]any{"name": n, "product": driven.Name}, http.StatusCreated)
	}
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "use-plain", "product": plain.Name}, http.StatusCreated)

	// By product: the three of the driven product, and total counts them all.
	got := get("/components?product=" + driven.Name)
	if got.Total != 3 || len(got.Components) != 3 {
		t.Fatalf("product filter = total %d, %d rows; want 3 and 3", got.Total, len(got.Components))
	}
	// A cap shortens the page and leaves the total whole.
	got = get("/components?product=" + driven.Name + "&limit=2")
	if got.Total != 3 || len(got.Components) != 2 {
		t.Fatalf("limit=2 = total %d, %d rows; want 3 and 2", got.Total, len(got.Components))
	}
	// By driver: every component of every product that driver serves.
	got = get("/components?driver=" + driven.Driver)
	if got.Total < 3 {
		t.Fatalf("driver filter total = %d, want at least the 3 made here", got.Total)
	}
	for _, r := range got.Components {
		if r.Name == "use-plain" {
			t.Fatalf("driver filter returned a component of a driverless product: %+v", got.Components)
		}
	}
	// By component type: the products classified under it.
	got = get("/components?component_type=" + plain.Type)
	found := false
	for _, r := range got.Components {
		found = found || r.Name == "use-plain"
	}
	if !found || got.Total < 1 {
		t.Fatalf("component_type filter missed use-plain: %+v", got)
	}
	// A reference that names nothing matches nothing, and says so as zero.
	if got = get("/components?product=no-such-product"); got.Total != 0 || len(got.Components) != 0 {
		t.Fatalf("unknown product = %+v, want empty", got)
	}
	// No filter: the whole scoped list, total included.
	if got = get("/components"); got.Total != len(got.Components) || got.Total < 4 {
		t.Fatalf("unfiltered = total %d, %d rows", got.Total, len(got.Components))
	}

	// Systems by standard.
	c.do(tok, http.MethodPost, "/standards", map[string]any{"name": "usage-std", "label": "Usage"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/systems", map[string]any{"name": "usage-sys", "standard_id": "usage-std"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/systems", map[string]any{"name": "usage-other"}, http.StatusCreated)
	var sys struct {
		Systems []struct {
			Name string `json:"name"`
		} `json:"systems"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/systems?standard=usage-std", nil, http.StatusOK), &sys); err != nil {
		t.Fatalf("decode systems: %v", err)
	}
	if sys.Total != 1 || len(sys.Systems) != 1 || sys.Systems[0].Name != "usage-sys" {
		t.Fatalf("systems by standard = %+v, want only usage-sys", sys)
	}

	// Products by what they declare.
	c.do(tok, http.MethodPost, "/products", map[string]any{
		"name": "usage-panel", "label": "Usage Panel", "kind": "device", "component_type": "generic-device",
	}, http.StatusCreated)
	c.do(tok, http.MethodPut, "/products/usage-panel/metrics/room-temperature", map[string]any{"required": false}, http.StatusOK)
	var declared struct {
		Products []struct {
			Name string `json:"name"`
		} `json:"products"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/products?declares_metric=room-temperature", nil, http.StatusOK), &declared); err != nil {
		t.Fatalf("decode declared: %v", err)
	}
	if len(declared.Products) != 1 || declared.Products[0].Name != "usage-panel" {
		t.Fatalf("declares_metric = %+v, want only usage-panel", declared.Products)
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/products?declares_property=no-such-property", nil, http.StatusOK), &declared); err != nil {
		t.Fatalf("decode declared property: %v", err)
	}
	if len(declared.Products) != 0 {
		t.Fatalf("declares_property of nothing = %+v, want empty", declared.Products)
	}
}
