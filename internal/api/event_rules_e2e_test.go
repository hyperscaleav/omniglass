package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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
		Expr  string `json:"expr"`
		Op    string `json:"op"`
		Value any    `json:"value"`
	} `json:"fire_criteria"`
	ClearCriteria *struct {
		Expr string `json:"expr"`
		Op   string `json:"op"`
	} `json:"clear_criteria"`
	FireSustain    string  `json:"fire_sustain"`
	Severity       *string `json:"severity"`
	FireEventType  string  `json:"fire_event_type"`
	ClearEventType *string `json:"clear_event_type"`
	Enabled        bool    `json:"enabled"`
	Version        int     `json:"version"`
	Official       bool    `json:"official"`
	OpenAlarms     *int    `json:"open_alarms"`
	Pending        *int    `json:"pending_streaks"`
	// LastJudged is when the rule last judged an ingested sample (#929).
	LastJudged *time.Time `json:"last_judged_at"`
}

// TestEventRuleAPI drives rule authoring over HTTP: rules are listed by
// any rule:read holder, like a registry, and authored with rule:* on an
// all-scope grant; a rule refers to a
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
		"fire_criteria": map[string]any{"op": "eq", "value": "down"}, "fire_sustain": "30s", "severity": "high",
		"fire_event_type": "condition-started"}
	c.do(viewer, http.MethodPost, "/event-rules", rule, http.StatusForbidden)
	var made eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", rule, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if made.SampleLane != "property" || made.FireSustain != "30s" || made.Version != 1 || !made.Enabled || made.ClearCriteria != nil ||
		made.FireCriteria.Expr != `value == "down"` || made.FireCriteria.Op != "" || made.FireCriteria.Value != nil {
		t.Fatalf("made = %+v", made)
	}
	c.do(ownerTok, http.MethodPost, "/event-rules", rule, http.StatusConflict)
	var state struct {
		Open       []any      `json:"open"`
		Pending    []any      `json:"pending"`
		LastJudged *time.Time `json:"last_judged_at"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/event-rules/reach-lost/state", nil, http.StatusOK), &state); err != nil {
		t.Fatal(err)
	}
	if state.Open == nil || state.Pending == nil || len(state.Open) != 0 || len(state.Pending) != 0 || state.LastJudged != nil {
		t.Fatalf("a fresh rule's state = %+v, want empty lists and never judged", state)
	}
	for _, bad := range []map[string]any{
		{"name": "r1", "sample_key": "nope", "fire_criteria": map[string]any{"op": "eq", "value": "x"}, "severity": "high", "fire_event_type": "condition-started"},
		{"name": "r2", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": "hot"}, "severity": "high", "fire_event_type": "condition-started"},
		{"name": "r3", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30}, "severity": "critical", "fire_event_type": "condition-started"},
		{"name": "r4", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30}, "fire_sustain": "soon", "severity": "high", "fire_event_type": "condition-started"},
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
	// Judging the down stamped the rule, on the get, the list and the state.
	if one.LastJudged == nil || time.Since(*one.LastJudged) > time.Minute {
		t.Fatalf("got reach-lost last judged %v, want just now", one.LastJudged)
	}
	for _, r := range list.EventRules {
		if r.Name == "reach-lost" && (r.LastJudged == nil || !r.LastJudged.Equal(*one.LastJudged)) {
			t.Fatalf("listed reach-lost last judged %v, want %v", r.LastJudged, one.LastJudged)
		}
	}
	state.LastJudged = nil
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/event-rules/reach-lost/state", nil, http.StatusOK), &state); err != nil {
		t.Fatal(err)
	}
	if state.LastJudged == nil || !state.LastJudged.Equal(*one.LastJudged) {
		t.Fatalf("the state says last judged %v, want %v", state.LastJudged, one.LastJudged)
	}

	// Rules read like a registry: a viewer scoped to one place lists and gets
	// every rule. The live state names components, so it carries only the
	// streaks on components the caller's component scope admits: a place
	// scope reaches none today, a component scope reaches its own.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rule-other"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "rule-other",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	south := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "south", "location_type": "campus"}, http.StatusCreated)
	var southLoc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(south, &southLoc); err != nil || southLoc.ID == "" {
		t.Fatalf("created location %s (%v)", south, err)
	}
	placeViewer := setupScopedPrincipal(t, ctx, dsn, "south-viewer", grantSpec{role: "viewer", scopeKind: "location", scopeID: southLoc.ID})
	target, err := gw.GetComponent(ctx, "rule-target", all)
	if err != nil {
		t.Fatal(err)
	}
	compViewer := setupScopedPrincipal(t, ctx, dsn, "target-viewer", grantSpec{role: "viewer", scopeKind: "component", scopeID: target.ID})
	ruleState := func(tok string) []struct {
		Component string `json:"component"`
	} {
		t.Helper()
		var st struct {
			Pending []struct {
				Component string `json:"component"`
			} `json:"pending"`
		}
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/event-rules/reach-lost/state", nil, http.StatusOK), &st); err != nil {
			t.Fatal(err)
		}
		return st.Pending
	}
	if got := ruleState(viewer); len(got) != 2 {
		t.Fatalf("the fleet viewer sees %d pending streaks, want 2", len(got))
	}
	for tok, want := range map[string][]string{placeViewer: {}, compViewer: {"rule-target"}} {
		var list struct {
			EventRules []eventRuleBody `json:"event_rules"`
		}
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/event-rules", nil, http.StatusOK), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.EventRules) != 2 {
			t.Fatalf("a scoped viewer lists %d rules, want both", len(list.EventRules))
		}
		for _, r := range list.EventRules {
			if r.Name == "reach-lost" && *r.Pending != len(want) {
				t.Fatalf("a scoped viewer counts %d pending on reach-lost, want %d (its visible components only)", *r.Pending, len(want))
			}
		}
		var one eventRuleBody
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/event-rules/reach-lost", nil, http.StatusOK), &one); err != nil {
			t.Fatal(err)
		}
		if one.Name != "reach-lost" || *one.Pending != len(want) {
			t.Fatalf("a scoped viewer gets %+v, want reach-lost with %d pending", one, len(want))
		}
		got := ruleState(tok)
		if len(got) != len(want) || (len(want) == 1 && got[0].Component != want[0]) {
			t.Fatalf("a scoped viewer sees streaks %+v, want %v", got, want)
		}
	}

	var upd eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/event-rules/reach-lost",
		map[string]any{"enabled": false, "clear_criteria": map[string]any{"op": "eq", "value": "up"}}, http.StatusOK), &upd); err != nil {
		t.Fatal(err)
	}
	if upd.Version != 2 || upd.Enabled || upd.ClearCriteria == nil || upd.ClearCriteria.Expr != `value == "up"` {
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
		"fire_criteria": map[string]any{"op": "gt", "value": 30}, "severity": "high", "fire_event_type": "condition-started"}, http.StatusForbidden)

	// A holder of every rule permission on one place alone still writes no
	// rule: a rule's selector reaches the whole fleet.
	placeOnly := setupScopedPrincipal(t, ctx, dsn, "north-only-owner", grantSpec{role: "owner", scopeKind: "location", scopeID: loc.ID})
	c.do(placeOnly, http.MethodGet, "/event-rules/reach-lost", nil, http.StatusOK)
	c.do(placeOnly, http.MethodPost, "/event-rules", map[string]any{"name": "north-only", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"op": "gt", "value": 30}, "severity": "high", "fire_event_type": "condition-started"}, http.StatusForbidden)
	c.do(placeOnly, http.MethodPatch, "/event-rules/reach-lost", map[string]any{"label": "mine"}, http.StatusForbidden)
	c.do(placeOnly, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusForbidden)

	c.do(viewer, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/event-rules/reach-lost", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/event-rules/reach-lost", nil, http.StatusNotFound)
}

// TestEventRuleTypesAPI drives #948 over HTTP: a rule saves with no severity
// (informational, the field absent on read), a fire event type is required
// and must exist, a clear type must exist when given, a PATCH with an empty
// severity makes a rule informational, and the official rule carries the
// generic pair.
func TestEventRuleTypesAPI(t *testing.T) {
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

	var info eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "hot-note",
		"sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 30},
		"fire_event_type": "condition-started"}, http.StatusCreated), &info); err != nil {
		t.Fatal(err)
	}
	if info.Severity != nil || info.FireEventType != "condition-started" || info.ClearEventType != nil {
		t.Fatalf("an informational rule reads %+v, want no severity, fire condition-started, no clear type", info)
	}
	for name, bad := range map[string]map[string]any{
		"no fire type":      {"name": "t1", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 1}},
		"unknown fire type": {"name": "t2", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 1}, "fire_event_type": "nope"},
		"unknown clear":     {"name": "t3", "sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 1}, "fire_event_type": "condition-started", "clear_event_type": "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			c.t = t
			c.do(ownerTok, http.MethodPost, "/event-rules", bad, http.StatusUnprocessableEntity)
		})
	}
	c.t = t

	var paged eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "hot-page",
		"sample_key": "room-temperature", "fire_criteria": map[string]any{"op": "gt", "value": 40}, "severity": "high",
		"fire_event_type": "condition-started", "clear_event_type": "condition-ended"}, http.StatusCreated), &paged); err != nil {
		t.Fatal(err)
	}
	if paged.Severity == nil || *paged.Severity != "high" || paged.ClearEventType == nil || *paged.ClearEventType != "condition-ended" {
		t.Fatalf("an alarm-raising rule reads %+v", paged)
	}
	var upd eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/event-rules/hot-page", map[string]any{"severity": "",
		"clear_event_type": ""}, http.StatusOK), &upd); err != nil {
		t.Fatal(err)
	}
	if upd.Severity != nil || upd.ClearEventType != nil || upd.Version != 2 {
		t.Fatalf("patched %+v, want informational with no clear type, version 2", upd)
	}
	c.do(ownerTok, http.MethodPatch, "/event-rules/hot-page", map[string]any{"fire_event_type": ""}, http.StatusUnprocessableEntity)

	var official eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-rules/reachability-lost", nil, http.StatusOK), &official); err != nil {
		t.Fatal(err)
	}
	if official.FireEventType != "condition-started" || official.ClearEventType == nil || *official.ClearEventType != "condition-ended" {
		t.Fatalf("the official rule reads %+v, want the generic pair", official)
	}
}

// TestRuleEventsOnTheEventsRead proves a rule's derived events read back on
// the component's and its system's event lists naming the rule and carrying
// the alarm they opened (#949).
func TestRuleEventsOnTheEventsRead(t *testing.T) {
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
	target, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "ev-target"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "ev-sys"}, http.StatusCreated)
	if st, b := c.send(ownerTok, http.MethodPut, "/systems/ev-sys/members/ev-target", nil); st >= 300 {
		t.Fatalf("add member = %d %s", st, b)
	}
	c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "ev-watch", "sample_key": "endpoint-reachable",
		"fire_criteria": map[string]any{"op": "eq", "value": "down"}, "severity": "high", "fire_event_type": "condition-started",
		"selector": map[string]any{"components": []string{"ev-target"}}}, http.StatusCreated)
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "ev-target",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	var alarms struct {
		Alarms []struct {
			ID           string `json:"id"`
			RaisedByRule string `json:"raised_by_rule"`
		} `json:"alarms"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/ev-target/alarms", nil, http.StatusOK), &alarms); err != nil {
		t.Fatal(err)
	}
	alarm := ""
	for _, a := range alarms.Alarms {
		if a.RaisedByRule == "ev-watch" {
			alarm = a.ID
		}
	}
	if alarm == "" {
		t.Fatalf("alarms = %+v, want one raised by ev-watch", alarms)
	}
	// The rule reads its condition in force as a count and in its state (#951).
	var one struct {
		ConditionsInForce *int `json:"conditions_in_force"`
		OpenAlarms        int  `json:"open_alarms"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-rules/ev-watch", nil, http.StatusOK), &one); err != nil {
		t.Fatal(err)
	}
	if one.ConditionsInForce == nil || *one.ConditionsInForce != 1 || one.OpenAlarms != 1 {
		t.Fatalf("ev-watch reads %+v, want 1 condition in force and 1 open alarm", one)
	}
	var st struct {
		Conditions []struct {
			Component   string    `json:"component"`
			ComponentID string    `json:"component_id"`
			Instance    string    `json:"instance"`
			Since       time.Time `json:"since"`
		} `json:"conditions"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-rules/ev-watch/state", nil, http.StatusOK), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Conditions) != 1 || st.Conditions[0].ComponentID != target.ID || st.Conditions[0].Instance != "tcp" {
		t.Fatalf("ev-watch state conditions = %+v, want ev-target on tcp", st.Conditions)
	}
	type ev struct {
		Key             string `json:"key"`
		Origin          string `json:"origin"`
		DerivedByRule   string `json:"derived_by_rule"`
		DerivedByRuleID string `json:"derived_by_rule_id"`
		AlarmID         string `json:"alarm_id"`
		OwnerID         string `json:"owner_id"`
	}
	for _, path := range []string{"/components/ev-target/events", "/systems/ev-sys/events"} {
		var out struct {
			Events []ev `json:"events"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, path, nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range out.Events {
			if e.Key == "condition-started" && e.DerivedByRule == "ev-watch" {
				// The system list names each row's owner by uuid too, so a row
				// can open its alarm on the right component (names repeat).
				found = e.Origin == "derived" && e.AlarmID == alarm && (path == "/components/ev-target/events" || e.OwnerID == target.ID)
			}
		}
		if !found {
			t.Fatalf("%s = %+v, want ev-watch's derived condition-started carrying alarm %s", path, out.Events, alarm)
		}
	}

	// An event names its rule by id while the rule exists, so a console links
	// it; once the rule is deleted the id goes and the recorded name stays.
	ruleID := func() (string, string) {
		var out struct {
			Events []ev `json:"events"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/ev-target/events", nil, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		for _, e := range out.Events {
			if e.Key == "condition-started" {
				return e.DerivedByRule, e.DerivedByRuleID
			}
		}
		return "", ""
	}
	if name, id := ruleID(); name != "ev-watch" || id == "" {
		t.Fatalf("a live rule's event names %q by id %q, want ev-watch with its id", name, id)
	}
	c.do(ownerTok, http.MethodDelete, "/event-rules/ev-watch", nil, http.StatusNoContent)
	if name, id := ruleID(); name != "ev-watch" || id != "" {
		t.Fatalf("a deleted rule's event names %q by id %q, want ev-watch with no id", name, id)
	}
}

// TestAlarmTimelineAPI drives GET /components/{name}/alarms/{id}:timeline
// (#950): the alarm's events and audit rows in time order, gated alarm:read
// with the component's read scope injected, so a caller who cannot read the
// component gets the same 404 an unknown one does.
func TestAlarmTimelineAPI(t *testing.T) {
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
	viewer := setupAllViewer(t, ctx, dsn, "timeline-viewer")
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "tl-target"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "tl-watch", "sample_key": "endpoint-reachable",
		"fire_criteria": map[string]any{"op": "eq", "value": "down"}, "clear_criteria": map[string]any{"op": "eq", "value": "up"},
		"severity": "high", "fire_event_type": "condition-started", "clear_event_type": "condition-ended",
		"selector": map[string]any{"components": []string{"tl-target"}}}, http.StatusCreated)
	sample := func(v string) {
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "tl-target",
			Key: "endpoint-reachable", Instance: "tcp", Value: v, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	sample("down")
	var alarms struct {
		Alarms []struct {
			ID           string `json:"id"`
			RaisedByRule string `json:"raised_by_rule"`
		} `json:"alarms"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/tl-target/alarms", nil, http.StatusOK), &alarms); err != nil {
		t.Fatal(err)
	}
	alarm := ""
	for _, a := range alarms.Alarms {
		if a.RaisedByRule == "tl-watch" {
			alarm = a.ID
		}
	}
	if alarm == "" {
		t.Fatalf("alarms = %+v, want one raised by tl-watch", alarms)
	}
	c.do(ownerTok, http.MethodPost, "/components/tl-target/alarms/"+alarm+":acknowledge", nil, http.StatusOK)
	sample("up")

	var tl struct {
		AlarmID string `json:"alarm_id"`
		Entries []struct {
			At    time.Time `json:"at"`
			Kind  string    `json:"kind"`
			What  string    `json:"what"`
			Actor string    `json:"actor"`
			Rule  string    `json:"rule"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/components/tl-target/alarms/"+alarm+":timeline", nil, http.StatusOK), &tl); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range tl.Entries {
		who := e.Actor
		if who == "" {
			who = "rule " + e.Rule
		}
		got = append(got, e.Kind+" "+e.What+" by "+who)
	}
	want := []string{"event condition-started by rule tl-watch", "audit raise by rule tl-watch", "audit acknowledge by root",
		"event condition-ended by rule tl-watch", "audit clear by rule tl-watch"}
	if tl.AlarmID != alarm || strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("timeline of %s = %q, want %q", tl.AlarmID, got, want)
	}

	// Any form of the id reads the same history, audit rows included.
	var upper struct {
		Entries []struct{} `json:"entries"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/components/tl-target/alarms/"+strings.ToUpper(alarm)+":timeline", nil, http.StatusOK), &upper); err != nil {
		t.Fatal(err)
	}
	if len(upper.Entries) != len(want) {
		t.Fatalf("the timeline by an uppercase id has %d entries, want %d", len(upper.Entries), len(want))
	}

	// A caller who cannot read the component is told it is not there.
	created := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "tl-north", "location_type": "campus"}, http.StatusCreated)
	var loc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &loc); err != nil || loc.ID == "" {
		t.Fatalf("created location %s (%v)", created, err)
	}
	north := setupScopedPrincipal(t, ctx, dsn, "tl-north-viewer", grantSpec{role: "viewer", scopeKind: "location", scopeID: loc.ID})
	c.do(north, http.MethodGet, "/components/tl-target/alarms/"+alarm+":timeline", nil, http.StatusNotFound)
	c.do(viewer, http.MethodGet, "/components/tl-target/alarms/01a15700-0000-7000-8000-000000000000:timeline", nil, http.StatusNotFound)
}

// TestEventRuleExpressionAPI proves a rule's criteria are expressions over
// HTTP (#957): a body may give {expr} or the short {op, value}, the answer is
// always {expr}, and an expression that does not compile is a 422 naming the
// criteria, line and column, on create and on update.
func TestEventRuleExpressionAPI(t *testing.T) {
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

	var made eventRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "hot", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"expr": `value > 40 && instance != "aux"`}, "clear_criteria": map[string]any{"op": "lt", "value": 35},
		"fire_event_type": "condition-started"}, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if made.FireCriteria.Expr != `value > 40 && instance != "aux"` || made.ClearCriteria == nil || made.ClearCriteria.Expr != "value < 35.0" {
		t.Fatalf("made = %+v / %+v, want both as expressions", made.FireCriteria, made.ClearCriteria)
	}

	type problem struct {
		Errors []struct {
			Location string         `json:"location"`
			Message  string         `json:"message"`
			Value    map[string]any `json:"value"`
		} `json:"errors"`
	}
	check := func(body []byte, location string, line, column float64) {
		t.Helper()
		var p problem
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Errors) != 1 || p.Errors[0].Location != location || p.Errors[0].Value["line"] != line || p.Errors[0].Value["column"] != column {
			t.Fatalf("422 body %s, want one error at %s, line %v, column %v", body, location, line, column)
		}
	}
	check(c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "bad", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"expr": "value > 40 &&\n  humidity > 2"}, "fire_event_type": "condition-started"},
		http.StatusUnprocessableEntity), "body.fire_criteria.expr", 2, 3)
	check(c.do(ownerTok, http.MethodPatch, "/event-rules/hot", map[string]any{"clear_criteria": map[string]any{"expr": "value + 1"}},
		http.StatusUnprocessableEntity), "body.clear_criteria.expr", 1, 1)
	// A short form is located at the criteria, not at an expr it never had.
	big := strings.Repeat("x", 4090)
	var sp problem
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "long", "sample_key": "endpoint-reachable",
		"fire_criteria": map[string]any{"op": "eq", "value": big}, "fire_event_type": "condition-started"},
		http.StatusUnprocessableEntity), &sp); err != nil {
		t.Fatal(err)
	}
	if len(sp.Errors) != 1 || sp.Errors[0].Location != "body.fire_criteria" {
		t.Fatalf("a short form's 422 errors %+v, want one at body.fire_criteria", sp.Errors)
	}

	// A stored rule whose criteria stop compiling (its key's data type changed
	// under it) says so on the list, a get and its state.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `update property_type set data_type = 'bool' where name = 'endpoint-reachable'`); err != nil {
		t.Fatal(err)
	}
	type withErr struct {
		Name          string `json:"name"`
		CriteriaError *struct {
			Field   string `json:"field"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
			Message string `json:"message"`
		} `json:"criteria_error"`
	}
	var got withErr
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-rules/reachability-lost", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if got.CriteriaError == nil || got.CriteriaError.Field != "fire_criteria" || got.CriteriaError.Line != 1 || got.CriteriaError.Message == "" {
		t.Fatalf("get: criteria_error %+v, want fire_criteria at line 1 with a message", got.CriteriaError)
	}
	var list struct {
		EventRules []withErr `json:"event_rules"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-rules", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	for _, r := range list.EventRules {
		if (r.Name == "reachability-lost") != (r.CriteriaError != nil) {
			t.Errorf("list: %s criteria_error %+v", r.Name, r.CriteriaError)
		}
	}
	var st withErr
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/event-rules/reachability-lost/state", nil, http.StatusOK), &st); err != nil {
		t.Fatal(err)
	}
	if st.CriteriaError == nil || *st.CriteriaError != *got.CriteriaError {
		t.Fatalf("state: criteria_error %+v, want %+v", st.CriteriaError, got.CriteriaError)
	}

	c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "both", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"expr": "value > 1", "op": "gt", "value": 1}, "fire_event_type": "condition-started"},
		http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/event-rules", map[string]any{"name": "clock", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"expr": "now() != nil"}, "fire_event_type": "condition-started"},
		http.StatusUnprocessableEntity)
}
