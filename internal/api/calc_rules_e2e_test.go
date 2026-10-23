package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

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
		Alias string `json:"alias"`
		Key   string `json:"key"`
		KeyID string `json:"key_id"`
		Lane  string `json:"lane"`
	} `json:"inputs"`
	Reduce struct {
		Expr string `json:"expr"`
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

	rule := map[string]any{"name": "temp-mean", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "avg(t)"},
		"output_key": "avg-temp"}
	c.do(viewer, http.MethodPost, "/calc-rules", rule, http.StatusForbidden)
	var made calcRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", rule, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if made.ID == "" || made.OutputLane != "metric" || made.Version != 1 || !made.Enabled || made.Reduce.Expr != "avg(t)" ||
		len(made.Inputs) != 1 || made.Inputs[0].Alias != "t" || made.Inputs[0].KeyID == "" || made.Inputs[0].Lane != "metric" {
		t.Fatalf("made = %+v", made)
	}
	// The metric it writes names it (#1019), on the get and the list.
	var mt struct {
		WrittenBy *struct {
			Rule   string `json:"rule"`
			RuleID string `json:"rule_id"`
		} `json:"written_by"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/metric-types/avg-temp", nil, http.StatusOK), &mt); err != nil {
		t.Fatal(err)
	}
	if mt.WrittenBy == nil || mt.WrittenBy.Rule != made.Name || mt.WrittenBy.RuleID != made.ID {
		t.Fatalf("avg-temp written_by %+v, want %s (%s)", mt.WrittenBy, made.Name, made.ID)
	}
	if list := string(c.do(ownerTok, http.MethodGet, "/metric-types", nil, http.StatusOK)); !strings.Contains(list, `"rule_id":"`+made.ID+`"`) {
		t.Fatal("the metric type list does not name the writing rule")
	}
	// A second enabled writer of avg-temp is refused naming the first; a
	// disabled one under the taken name is a name conflict.
	p := problem{}
	second := map[string]any{"name": "temp-high", "owner_kind": "system",
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "max(t)"}, "output_key": "avg-temp"}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", second, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Errors) != 1 || p.Errors[0].Location != "body.output_key" || !strings.Contains(p.Errors[0].Message, "temp-mean") {
		t.Fatalf("a second writer: %+v, want body.output_key naming temp-mean", p)
	}
	taken := map[string]any{}
	for k, v := range rule {
		taken[k] = v
	}
	taken["enabled"] = false
	c.do(ownerTok, http.MethodPost, "/calc-rules", taken, http.StatusConflict)

	// The key a rule writes cannot be deleted from its registry: a 409 naming
	// the rule.
	p = problem{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodDelete, "/metric-types/avg-temp", nil, http.StatusConflict), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Detail, "temp-mean") {
		t.Fatalf("deleting a written metric type: %+v, want the rule named", p)
	}

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
	p = problem{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "bad-expr", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "t +\n nope"},
		"output_key": "temp-twice"}, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Errors) != 1 || p.Errors[0].Location != "body.reduce.expr" || p.Errors[0].Value["line"] != float64(2) {
		t.Fatalf("a bad reduce: %+v, want body.reduce.expr at line 2", p)
	}

	// A rule closing a loop names the chain.
	c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "doubled", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "a", "key": "avg-temp"}}, "reduce": map[string]any{"expr": "max(a) * 2"},
		"output_key": "temp-twice"}, http.StatusCreated)
	p = problem{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "closing", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "a", "key": "temp-twice"}}, "reduce": map[string]any{"expr": "max(a) / 2"},
		"output_key": "avg-temp"}, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Detail, "temp-twice -> avg-temp -> temp-twice") {
		t.Fatalf("a closing rule: %+v, want the chain temp-twice -> avg-temp -> temp-twice", p)
	}

	for name, bad := range map[string]map[string]any{
		"health input": {"name": "h-in", "owner_kind": "component", "inputs": []map[string]any{{"alias": "h", "key": "health"}},
			"reduce": map[string]any{"expr": `h == "outage" ? 1.0 : 0.0`}, "output_key": "temp-twice"},
		"health output": {"name": "h-out", "owner_kind": "component", "inputs": []map[string]any{{"alias": "v", "key": "video-input"}},
			"reduce": map[string]any{"expr": "majority(v)"}, "output_key": "health"},
		"driver output": {"name": "d-out", "owner_kind": "component", "inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}},
			"reduce": map[string]any{"expr": "max(t)"}, "output_key": "uptime"},
		"bad as": {"name": "bad-as", "owner_kind": "component", "inputs": []map[string]any{{"alias": "not", "key": "room-temperature"}},
			"reduce": map[string]any{"expr": "max(not)"}, "output_key": "temp-twice"},
		"bad owner": {"name": "bad-owner", "owner_kind": "location", "inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}},
			"reduce": map[string]any{"expr": "max(t)"}, "output_key": "temp-twice"},
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
	if err := json.Unmarshal(c.do(viewer, http.MethodPost, "/expressions:validate", map[string]any{"site": "calc", "source": "len(members) > 0 ? members[0].value.temps : 0.0",
		"key": "temp-twice", "owner_kind": "system", "inputs": []map[string]any{{"alias": "temps", "key": "room-temperature"}}}, http.StatusOK), &v); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, b := range v.Bindings {
		names[b.Name] = b.Type
	}
	if !v.OK || v.ValueType != "number" || v.ResultType != "float64" || names["members"] != "[]record" || names["members[].value.temps"] != "float64" {
		t.Fatalf("validate = %+v, want ok with members records holding value.temps", v)
	}
	c.do(viewer, http.MethodPost, "/expressions:validate", map[string]any{"site": "calc", "source": "1.0", "key": "temp-twice"}, http.StatusUnprocessableEntity)
	c.do(viewer, http.MethodPost, "/expressions:validate", map[string]any{"site": "calc", "source": `h == "outage"`, "key": "temp-twice",
		"inputs": []map[string]any{{"alias": "h", "key": "health"}}}, http.StatusUnprocessableEntity)

	// A stored reduce that stops compiling reads back with calc_error.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `update calc_rule set reduce = '{"expr": "a *\n nope"}', version = version + 1 where name = 'doubled'`); err != nil {
		t.Fatal(err)
	}
	var broken struct {
		CalcError *struct {
			Field   string `json:"field"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
			Message string `json:"message"`
		} `json:"calc_error"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/calc-rules/doubled", nil, http.StatusOK), &broken); err != nil {
		t.Fatal(err)
	}
	if broken.CalcError == nil || broken.CalcError.Field != "reduce.expr" || broken.CalcError.Line != 2 || broken.CalcError.Message == "" {
		t.Fatalf("a broken rule reads %+v, want calc_error at reduce.expr line 2", broken.CalcError)
	}
	c.do(ownerTok, http.MethodPatch, "/calc-rules/doubled", map[string]any{"enabled": false}, http.StatusOK)
	c.do(ownerTok, http.MethodPatch, "/calc-rules/doubled", map[string]any{"enabled": true}, http.StatusUnprocessableEntity)

	// Writes take an all-scope grant.
	south := c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "calc-south", "location_type": "campus"}, http.StatusCreated)
	var loc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(south, &loc); err != nil || loc.ID == "" {
		t.Fatalf("created location %s (%v)", south, err)
	}
	placeOwner := setupScopedPrincipal(t, ctx, dsn, "calc-south-owner", grantSpec{role: "owner", scopeKind: "location", scopeID: loc.ID})
	c.do(placeOwner, http.MethodGet, "/calc-rules/temp-mean", nil, http.StatusOK)
	c.do(placeOwner, http.MethodPost, "/calc-rules", map[string]any{"name": "mine", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "max(t)"}, "output_key": "temp-twice"}, http.StatusForbidden)
	c.do(placeOwner, http.MethodPatch, "/calc-rules/temp-mean", map[string]any{"label": "mine"}, http.StatusForbidden)
	c.do(placeOwner, http.MethodPost, "/calc-rules/temp-mean:rename", map[string]any{"name": "mine"}, http.StatusForbidden)
	c.do(placeOwner, http.MethodDelete, "/calc-rules/temp-mean", nil, http.StatusForbidden)

	var up calcRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/calc-rules/temp-mean", map[string]any{"reduce": map[string]any{"expr": "max(t)"}}, http.StatusOK), &up); err != nil {
		t.Fatal(err)
	}
	if up.Version != 2 || up.Reduce.Expr != "max(t)" || up.ID != made.ID {
		t.Fatalf("updated = %+v, want version 2 with max", up)
	}

	var renamed calcRuleBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules/temp-mean:rename", map[string]any{"name": "temp-peak"}, http.StatusOK), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.ID != made.ID || renamed.Name != "temp-peak" || renamed.Version != 2 {
		t.Fatalf("renamed = %+v, want the same uuid and version under temp-peak", renamed)
	}
	c.do(ownerTok, http.MethodGet, "/calc-rules/temp-mean", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodPost, "/calc-rules/temp-peak:rename", map[string]any{"name": "doubled"}, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/calc-rules/temp-peak:rename", map[string]any{"name": "Not Kebab"}, http.StatusUnprocessableEntity)

	c.do(viewer, http.MethodDelete, "/calc-rules/temp-peak", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/calc-rules/temp-peak", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/calc-rules/temp-peak", nil, http.StatusNotFound)
}
