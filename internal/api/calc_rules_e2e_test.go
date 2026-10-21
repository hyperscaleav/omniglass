package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type calcRuleBody struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerKind string `json:"owner_kind"`
	Inputs    []struct {
		As  string `json:"as"`
		Key string `json:"key"`
	} `json:"inputs"`
	Reduce struct {
		Reducer string `json:"reducer"`
		Expr    string `json:"expr"`
	} `json:"reduce"`
	OutputKey  string `json:"output_key"`
	OutputLane string `json:"output_lane"`
	Enabled    bool   `json:"enabled"`
	Official   bool   `json:"official"`
	Version    int    `json:"version"`
}

type problem struct {
	Detail string `json:"detail"`
	Errors []struct {
		Location string         `json:"location"`
		Message  string         `json:"message"`
		Value    map[string]any `json:"value"`
	} `json:"errors"`
}

// TestCalcRuleAPI drives calc rule authoring over HTTP (#995): rules read
// like a registry for any rule:read holder and are written with rule:* on an
// all-scope grant; a write returns the rule's uuid; a reduce that does not
// compile is a 422 at its line and column, a rule closing a cycle a 422
// naming the chain, health as an input or output and an output a driver
// emits a 422; a rename keeps the uuid and version.
func TestCalcRuleAPI(t *testing.T) {
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
	viewer := setupAllViewer(t, ctx, dsn, "calc-viewer")
	for _, m := range []string{"avg-temp", "temp-twice"} {
		c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{"name": m, "data_type": "float"}, http.StatusCreated)
	}

	rule := map[string]any{"name": "room-avg", "owner_kind": "component",
		"inputs": []map[string]any{{"as": "t", "key": "room-temperature"}}, "reduce": map[string]any{"reducer": "average"},
		"output_key": "avg-temp"}
	c.do(viewer, http.MethodPost, "/calc-rules", rule, http.StatusForbidden)
	var made calcRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", rule, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if made.ID == "" || made.OutputLane != "metric" || made.Version != 1 || !made.Enabled || made.Reduce.Reducer != "average" ||
		len(made.Inputs) != 1 || made.Inputs[0].As != "t" {
		t.Fatalf("made = %+v", made)
	}
	c.do(ownerTok, http.MethodPost, "/calc-rules", rule, http.StatusConflict)

	var list struct {
		CalcRules []calcRuleBody `json:"calc_rules"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/calc-rules", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.CalcRules) != 1 || list.CalcRules[0].ID != made.ID {
		t.Fatalf("list = %+v", list)
	}
	c.do(viewer, http.MethodGet, "/calc-rules/"+made.ID, nil, http.StatusOK)

	// A reduce that does not compile is placed at its expression, line and
	// column.
	var p problem
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "bad-expr", "owner_kind": "component",
		"inputs": []map[string]any{{"as": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "t +\n nope"},
		"output_key": "temp-twice"}, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Errors) != 1 || p.Errors[0].Location != "body.reduce.expr" || p.Errors[0].Value["line"] != float64(2) {
		t.Fatalf("a bad reduce: %+v, want body.reduce.expr at line 2", p)
	}

	// A rule closing a loop names the chain.
	c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "doubled", "owner_kind": "component",
		"inputs": []map[string]any{{"as": "a", "key": "avg-temp"}}, "reduce": map[string]any{"expr": "a * 2"},
		"output_key": "temp-twice"}, http.StatusCreated)
	p = problem{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "closing", "owner_kind": "component",
		"inputs": []map[string]any{{"as": "a", "key": "temp-twice"}}, "reduce": map[string]any{"expr": "a / 2"},
		"output_key": "avg-temp"}, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Detail, "temp-twice -> avg-temp -> temp-twice") {
		t.Fatalf("a closing rule: %+v, want the chain temp-twice -> avg-temp -> temp-twice", p)
	}

	for name, bad := range map[string]map[string]any{
		"health input": {"name": "h-in", "owner_kind": "component", "inputs": []map[string]any{{"as": "h", "key": "health"}},
			"reduce": map[string]any{"expr": `h == "outage" ? 1.0 : 0.0`}, "output_key": "temp-twice"},
		"health output": {"name": "h-out", "owner_kind": "component", "inputs": []map[string]any{{"as": "v", "key": "video-input"}},
			"reduce": map[string]any{"reducer": "majority"}, "output_key": "health"},
		"driver output": {"name": "d-out", "owner_kind": "component", "inputs": []map[string]any{{"as": "t", "key": "room-temperature"}},
			"reduce": map[string]any{"reducer": "max"}, "output_key": "uptime"},
		"bad as": {"name": "bad-as", "owner_kind": "component", "inputs": []map[string]any{{"as": "not", "key": "room-temperature"}},
			"reduce": map[string]any{"reducer": "max"}, "output_key": "temp-twice"},
		"bad owner": {"name": "bad-owner", "owner_kind": "location", "inputs": []map[string]any{{"as": "t", "key": "room-temperature"}},
			"reduce": map[string]any{"reducer": "max"}, "output_key": "temp-twice"},
	} {
		t.Run(name, func(t *testing.T) {
			c.t = t
			c.do(ownerTok, http.MethodPost, "/calc-rules", bad, http.StatusUnprocessableEntity)
		})
	}
	c.t = t

	// The validate route checks a calc reduce against the inputs it reads.
	var v struct {
		OK          bool   `json:"ok"`
		ValueType   string `json:"value_type"`
		ResultType  string `json:"result_type"`
		Diagnostics []struct {
			Line int `json:"line"`
		} `json:"diagnostics"`
		Bindings []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodPost, "/expressions:validate", map[string]any{"site": "calc", "source": "len(temps) > 0 ? temps[0] : 0.0",
		"key": "temp-twice", "owner_kind": "system", "inputs": []map[string]any{{"as": "temps", "key": "room-temperature"}}}, http.StatusOK), &v); err != nil {
		t.Fatal(err)
	}
	if !v.OK || v.ValueType != "number" || v.ResultType != "float64" || len(v.Bindings) != 1 || v.Bindings[0].Name != "temps" || v.Bindings[0].Type != "[]float64" {
		t.Fatalf("validate = %+v, want ok with temps a []float64", v)
	}
	c.do(viewer, http.MethodPost, "/expressions:validate", map[string]any{"site": "calc", "source": "1.0", "key": "temp-twice"}, http.StatusUnprocessableEntity)

	// Writes take an all-scope grant.
	south := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "calc-south", "location_type": "campus"}, http.StatusCreated)
	var loc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(south, &loc); err != nil || loc.ID == "" {
		t.Fatalf("created location %s (%v)", south, err)
	}
	placeOwner := setupScopedPrincipal(t, ctx, dsn, "calc-south-owner", grantSpec{role: "owner", scopeKind: "location", scopeID: loc.ID})
	c.do(placeOwner, http.MethodGet, "/calc-rules/room-avg", nil, http.StatusOK)
	c.do(placeOwner, http.MethodPost, "/calc-rules", map[string]any{"name": "mine", "owner_kind": "component",
		"inputs": []map[string]any{{"as": "t", "key": "room-temperature"}}, "reduce": map[string]any{"reducer": "max"}, "output_key": "temp-twice"}, http.StatusForbidden)
	c.do(placeOwner, http.MethodPatch, "/calc-rules/room-avg", map[string]any{"label": "mine"}, http.StatusForbidden)
	c.do(placeOwner, http.MethodPost, "/calc-rules/room-avg:rename", map[string]any{"name": "mine"}, http.StatusForbidden)
	c.do(placeOwner, http.MethodDelete, "/calc-rules/room-avg", nil, http.StatusForbidden)

	var up calcRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/calc-rules/room-avg", map[string]any{"reduce": map[string]any{"reducer": "max"}}, http.StatusOK), &up); err != nil {
		t.Fatal(err)
	}
	if up.Version != 2 || up.Reduce.Reducer != "max" || up.ID != made.ID {
		t.Fatalf("updated = %+v, want version 2 with max", up)
	}

	var renamed calcRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules/room-avg:rename", map[string]any{"name": "room-max"}, http.StatusOK), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.ID != made.ID || renamed.Name != "room-max" || renamed.Version != 2 {
		t.Fatalf("renamed = %+v, want the same uuid and version under room-max", renamed)
	}
	c.do(ownerTok, http.MethodGet, "/calc-rules/room-avg", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodPost, "/calc-rules/room-max:rename", map[string]any{"name": "doubled"}, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/calc-rules/room-max:rename", map[string]any{"name": "Not Kebab"}, http.StatusUnprocessableEntity)

	c.do(viewer, http.MethodDelete, "/calc-rules/room-max", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/calc-rules/room-max", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/calc-rules/room-max", nil, http.StatusNotFound)
}
