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
	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
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
	OpenAlarms  *int   `json:"open_alarms"`
	Pending     *int   `json:"pending_streaks"`
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
	var state struct {
		Open    []any `json:"open"`
		Pending []any `json:"pending"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/event-rules/reach-lost/state", nil, http.StatusOK), &state); err != nil {
		t.Fatal(err)
	}
	if state.Open == nil || state.Pending == nil || len(state.Open) != 0 || len(state.Pending) != 0 {
		t.Fatalf("a fresh rule's state = %+v, want empty lists", state)
	}
	for _, bad := range []map[string]any{
		{"name": "r1", "sample_key": "nope", "fire_criteria": map[string]any{"op": "eq", "value": "x"}, "severity": "high"},
		{"name": "r2", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": "hot"}, "severity": "high"},
		{"name": "r3", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30}, "severity": "critical"},
		{"name": "r4", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30}, "fire_sustain": "soon", "severity": "high"},
	} {
		c.do(ownerTok, http.MethodPost, "/event-rules", bad, http.StatusUnprocessableEntity)
	}

	// A component reporting down starts the rule's 30s streak: the list and a
	// get carry it as a count, so the rules page reads every rule's live state
	// from the one list call.
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rule-target"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "rule-target",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now()}}); err != nil {
		t.Fatal(err)
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
	for _, r := range list.EventRules {
		if r.OpenAlarms == nil || r.Pending == nil {
			t.Fatalf("listed %s without its counts: %+v", r.Name, r)
		}
		if r.Name == "reach-lost" && (*r.OpenAlarms != 0 || *r.Pending != 1) {
			t.Fatalf("listed reach-lost with %d open, %d pending; want 0 open, 1 pending", *r.OpenAlarms, *r.Pending)
		}
	}
	var one eventRuleBody
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/event-rules/reach-lost", nil, http.StatusOK), &one); err != nil {
		t.Fatal(err)
	}
	if one.OpenAlarms == nil || one.Pending == nil || *one.OpenAlarms != 0 || *one.Pending != 1 {
		t.Fatalf("got reach-lost %+v, want 0 open and 1 pending", one)
	}

	var upd eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/event-rules/reach-lost",
		map[string]any{"enabled": false, "clear_criteria": map[string]any{"op": "eq", "value": "up"}}, http.StatusOK), &upd); err != nil {
		t.Fatal(err)
	}
	if upd.Version != 2 || upd.Enabled || upd.ClearCriteria == nil {
		t.Fatalf("updated = %+v, want version 2, disabled, with a clear criterion", upd)
	}
	// A caller who owns one place but only views the fleet holds rule:create in
	// its flattened permissions, yet the gateway refuses it a fleet-wide rule.
	// /auth/me says which permissions ride an all-scope grant, so the console
	// offers New rule only to a caller the server would let author one.
	created := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "north", "location_type": "campus"}, http.StatusCreated)
	var loc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &loc); err != nil || loc.ID == "" {
		t.Fatalf("created location %s (%v)", created, err)
	}
	placeOwner := setupScopedPrincipal(t, ctx, dsn, "north-owner",
		grantSpec{role: "owner", scopeKind: "location", scopeID: loc.ID},
		grantSpec{role: "viewer", scopeKind: "all"})
	var me struct {
		Permissions      []string `json:"permissions"`
		FleetPermissions []string `json:"fleet_permissions"`
	}
	if err := json.Unmarshal(c.do(placeOwner, http.MethodGet, "/auth/me", nil, http.StatusOK), &me); err != nil {
		t.Fatal(err)
	}
	if rbac.NewSet(me.FleetPermissions).Allows("rule", "create") || !rbac.NewSet(me.Permissions).Allows("rule", "create") {
		t.Fatalf("me = %+v, want rule:create among its permissions but not its fleet permissions", me)
	}
	if !rbac.NewSet(me.FleetPermissions).Allows("rule", "read") {
		t.Fatalf("fleet permissions %v, want the all-scope viewer's rule:read", me.FleetPermissions)
	}
	c.do(placeOwner, http.MethodPost, "/event-rules", map[string]any{"name": "north-only", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"op": "gt", "value": 30}, "severity": "high"}, http.StatusForbidden)

	c.do(viewer, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/event-rules/reach-lost", nil, http.StatusNotFound)
}
