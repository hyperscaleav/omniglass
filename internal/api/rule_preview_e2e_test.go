package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type previewOut struct {
	InForceNow []struct {
		Component   string     `json:"component"`
		ComponentID string     `json:"component_id"`
		Instance    string     `json:"instance"`
		Since       *time.Time `json:"since"`
	} `json:"in_force_now"`
	Edges []struct {
		Component string    `json:"component"`
		Instance  string    `json:"instance"`
		At        time.Time `json:"at"`
		Kind      string    `json:"kind"`
	} `json:"edges"`
	Window          string    `json:"window"`
	WindowSince     time.Time `json:"window_since"`
	RowCap          int       `json:"row_cap"`
	Truncated       bool      `json:"truncated"`
	SustainsIgnored bool      `json:"sustains_ignored"`
}

// TestPreviewEventRuleAPI drives POST /event-rules:preview (#959): a
// candidate rule's edges over the last 24h and the series it holds in force
// now, judged within the caller's component read scope, sustains ignored
// and said so, writing nothing. Gated by rule:read; a bad criterion is a 422
// at its position.
func TestPreviewEventRuleAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	all := scope.Set{All: true}
	for _, name := range []string{"pa-1", "pa-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for _, s := range []struct {
		comp, value string
		ago         time.Duration
	}{{"pa-1", "down", 3 * time.Hour}, {"pa-1", "up", 2 * time.Hour}, {"pa-2", "down", time.Hour}} {
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: s.comp,
			Key: "endpoint-reachable", Instance: "tcp", Value: s.value, TS: now.Add(-s.ago)}}); err != nil {
			t.Fatal(err)
		}
	}
	viewer := setupAllViewer(t, ctx, dsn, "preview-viewer")
	pa1, err := gw.GetComponent(ctx, "pa-1", all)
	if err != nil {
		t.Fatal(err)
	}
	compViewer := setupScopedPrincipal(t, ctx, dsn, "pa1-viewer", grantSpec{role: "viewer", scopeKind: "component", scopeID: pa1.ID})
	south := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "south", "location_type": "campus"}, http.StatusCreated)
	var loc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(south, &loc); err != nil {
		t.Fatal(err)
	}
	placeViewer := setupScopedPrincipal(t, ctx, dsn, "south-viewer", grantSpec{role: "viewer", scopeKind: "location", scopeID: loc.ID})
	limited := setupLimitedPrincipal(t, ctx, dsn)

	body := map[string]any{"sample_key": "endpoint-reachable", "fire_criteria": map[string]any{"expr": `value == "down"`},
		"selector": map[string]any{"components": []string{"pa-1", "pa-2"}}, "fire_sustain": "15s"}
	preview := func(tok string) previewOut {
		t.Helper()
		var out previewOut
		if err := json.Unmarshal(c.do(tok, http.MethodPost, "/event-rules:preview", body, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := preview(viewer)
	if !got.SustainsIgnored || got.Truncated || got.RowCap != storage.RulePreviewRowCap || got.Window != "24h0m0s" {
		t.Fatalf("bounds %+v", got)
	}
	if len(got.Edges) != 3 || got.Edges[0].Component != "pa-1" || got.Edges[0].Kind != "fire" || got.Edges[1].Kind != "clear" ||
		got.Edges[2].Component != "pa-2" || got.Edges[2].Kind != "fire" {
		t.Fatalf("edges %+v, want pa-1 fire, pa-1 clear, pa-2 fire", got.Edges)
	}
	if len(got.InForceNow) != 1 || got.InForceNow[0].Component != "pa-2" || got.InForceNow[0].Since == nil {
		t.Fatalf("in force now %+v, want pa-2 since its fire", got.InForceNow)
	}

	// The caller's component read scope fences the preview.
	if mine := preview(compViewer); len(mine.Edges) != 2 || mine.Edges[0].Component != "pa-1" || len(mine.InForceNow) != 0 {
		t.Fatalf("a viewer of pa-1 sees %+v, want only pa-1's edges", mine)
	}
	if none := preview(placeViewer); len(none.Edges) != 0 || len(none.InForceNow) != 0 {
		t.Fatalf("a viewer of an empty place sees %+v, want nothing", none)
	}

	// A bad criterion is a 422 at its position; rule:read gates it.
	var p struct {
		Errors []struct {
			Location string         `json:"location"`
			Value    map[string]any `json:"value"`
		} `json:"errors"`
	}
	bad := map[string]any{"sample_key": "endpoint-reachable", "fire_criteria": map[string]any{"expr": "value ==\n  nope"}}
	if err := json.Unmarshal(c.do(viewer, http.MethodPost, "/event-rules:preview", bad, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Errors) != 1 || p.Errors[0].Location != "body.fire_criteria.expr" || p.Errors[0].Value["line"] != 2.0 {
		t.Fatalf("a bad criterion's 422 %+v, want body.fire_criteria.expr at line 2", p.Errors)
	}
	c.do(limited, http.MethodPost, "/event-rules:preview", body, http.StatusForbidden)
}
