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

type eventRuleBody struct {
	Name         string `json:"name"`
	SampleKey    string `json:"sample_key"`
	SampleLane   string `json:"sample_lane"`
	FireCriteria struct {
		Op    string `json:"op"`
		Value any    `json:"value"`
	} `json:"fire_criteria"`
	ClearCriteria *struct {
		Op string `json:"op"`
	} `json:"clear_criteria"`
	FireSustain string `json:"fire_sustain"`
	Severity    string `json:"severity"`
	Enabled     bool   `json:"enabled"`
	Version     int    `json:"version"`
	Official    bool   `json:"official"`
}

// TestEventRuleAPI drives rule authoring over HTTP: rules are listed by
// any all-scope reader and authored with rule:*; a rule refers to a
// registered sample key, criteria its lane can evaluate and a level that
// exists; an update bumps its version.
func TestEventRuleAPI(t *testing.T) {
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
	viewer := setupAllViewer(t, ctx, dsn, "rule-viewer")

	rule := map[string]any{"name": "reach-lost", "label": "Reachability lost", "sample_key": "endpoint-reachable",
		"fire_criteria": map[string]any{"op": "eq", "value": "down"}, "fire_sustain": "30s", "severity": "high"}
	c.do(viewer, http.MethodPost, "/event-rules", rule, http.StatusForbidden)
	var made eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", rule, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if made.SampleLane != "property" || made.FireSustain != "30s" || made.Version != 1 || !made.Enabled || made.ClearCriteria != nil ||
		made.FireCriteria.Op != "eq" || made.FireCriteria.Value != "down" {
		t.Fatalf("made = %+v", made)
	}
	c.do(ownerTok, http.MethodPost, "/event-rules", rule, http.StatusConflict)
	for _, bad := range []map[string]any{
		{"name": "r1", "sample_key": "nope", "fire_criteria": map[string]any{"op": "eq", "value": "x"}, "severity": "high"},
		{"name": "r2", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": "hot"}, "severity": "high"},
		{"name": "r3", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30}, "severity": "critical"},
		{"name": "r4", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30}, "fire_sustain": "soon", "severity": "high"},
	} {
		c.do(ownerTok, http.MethodPost, "/event-rules", bad, http.StatusUnprocessableEntity)
	}

	var list struct {
		EventRules []eventRuleBody `json:"event_rules"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/event-rules", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.EventRules) != 2 {
		t.Fatalf("listed %d rules, want 2 (the authored one and the official reachability rule)", len(list.EventRules))
	}

	var upd eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/event-rules/reach-lost",
		map[string]any{"enabled": false, "clear_criteria": map[string]any{"op": "eq", "value": "up"}}, http.StatusOK), &upd); err != nil {
		t.Fatal(err)
	}
	if upd.Version != 2 || upd.Enabled || upd.ClearCriteria == nil {
		t.Fatalf("updated = %+v, want version 2, disabled, with a clear criterion", upd)
	}
	c.do(viewer, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/event-rules/reach-lost", nil, http.StatusNotFound)
}
