package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type calcStateBody struct {
	Owners []struct {
		OwnerKind   string          `json:"owner_kind"`
		Owner       string          `json:"owner"`
		OwnerID     string          `json:"owner_id"`
		Value       json.RawMessage `json:"value"`
		RuleVersion int             `json:"rule_version"`
		Missing     []string        `json:"missing"`
		Skipped     []struct {
			Key       string `json:"key"`
			Component string `json:"component"`
		} `json:"skipped"`
		SkippedHidden int  `json:"skipped_hidden"`
		Pending       bool `json:"pending"`
	} `json:"owners"`
	Total        int `json:"total"`
	PendingDepth int `json:"pending_depth"`
}

// TestCalcRuleStateAndPreviewAPI drives a calc rule's state and preview over
// HTTP (#998): both read with rule:read, fenced to what the caller may read;
// a viewer scoped to one system and one of its members sees only that
// system and never names its other member; the preview writes nothing and refuses what a save
// would.
func TestCalcRuleStateAndPreviewAPI(t *testing.T) {
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
	c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{"name": "avg-temp", "data_type": "float"}, http.StatusCreated)
	var places = map[string]string{}
	for _, name := range []string{"east", "west"} {
		var loc struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": name, "location_type": "campus"}, http.StatusCreated), &loc); err != nil {
			t.Fatal(err)
		}
		places[name] = loc.ID
	}
	av := "av"
	for sys, place := range map[string]string{"sys-e": "east", "sys-w": "west"} {
		p := place
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: sys, SystemTypeID: &av, LocationName: &p}, all, all); err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"left", "right"} {
			if _, err := gw.SetSystemRole(ctx, "", "system", sys, storage.SystemRoleSpec{Name: role, Label: role, Quorum: 1, Impact: "none"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for comp, place := range map[string]string{"e1": "east", "w1": "west", "w2": "west"} {
		p := place
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp, LocationName: &p}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	// sys-e staffed by e1 and by w1 (a member placed in the other place).
	for _, a := range [][3]string{{"sys-e", "left", "e1"}, {"sys-e", "right", "w1"}, {"sys-w", "left", "w2"}} {
		if err := gw.AssignRole(ctx, "", a[0], a[1], a[2], all, all); err != nil {
			t.Fatal(err)
		}
	}
	for comp, v := range map[string]float64{"e1": 20, "w2": 40} {
		if err := gw.InsertMetricSamples(ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp, Key: "room-temperature", Value: v, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	rule := map[string]any{"owner_kind": "system", "selector": map[string]any{"system_types": []string{"av"}},
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "avg(t)"}, "output_key": "avg-temp"}
	// A viewer of e1 and of sys-e alone (the scope grants the event rule
	// preview's tests use: a component and a system).
	e1, err := gw.GetComponent(ctx, "e1", all)
	if err != nil {
		t.Fatal(err)
	}
	sysE, err := gw.GetSystem(ctx, "sys-e", all)
	if err != nil {
		t.Fatal(err)
	}
	eastViewer := setupScopedPrincipal(t, ctx, dsn, "east-viewer",
		grantSpec{role: "viewer", scopeKind: "component", scopeID: e1.ID}, grantSpec{role: "viewer", scopeKind: "system", scopeID: sysE.ID})

	// The preview, as the fleet and as the east viewer, writing nothing.
	var pv struct {
		Owners []struct {
			Owner         string          `json:"owner"`
			Value         json.RawMessage `json:"value"`
			Differs       bool            `json:"differs"`
			SkippedHidden int             `json:"skipped_hidden"`
			Skipped       []struct {
				Component string `json:"component"`
			} `json:"skipped"`
		} `json:"owners"`
		OwnersJudged int  `json:"owners_judged"`
		OwnersTotal  int  `json:"owners_total"`
		Truncated    bool `json:"truncated"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules:preview", rule, http.StatusOK), &pv); err != nil {
		t.Fatal(err)
	}
	if len(pv.Owners) != 2 || pv.OwnersTotal != 2 || pv.Truncated {
		t.Fatalf("the fleet preview %+v, want sys-e and sys-w", pv)
	}
	pv.Owners = nil
	if err := json.Unmarshal(c.do(eastViewer, http.MethodPost, "/calc-rules:preview", rule, http.StatusOK), &pv); err != nil {
		t.Fatal(err)
	}
	if len(pv.Owners) != 1 || pv.Owners[0].Owner != "sys-e" || string(pv.Owners[0].Value) != "20" || !pv.Owners[0].Differs {
		t.Fatalf("the east preview %+v, want sys-e alone at 20", pv)
	}
	if len(pv.Owners[0].Skipped) != 0 || pv.Owners[0].SkippedHidden != 1 {
		t.Fatalf("the east preview names %+v, want w1 a hidden count", pv.Owners[0])
	}

	// An expression can address one member (members[1].value.t, .name,
	// .id), so over a system with a member the caller may not read the
	// preview withholds the value rather than reveal that member's own (w1
	// holds no temperature here, so its value would read as the zero).
	probe := map[string]any{"owner_kind": "system", "selector": map[string]any{"systems": []string{"sys-e"}},
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "members[len(members) - 1].value.t"}, "output_key": "avg-temp"}
	var leak struct {
		Owners []struct {
			Owner string          `json:"owner"`
			Value json.RawMessage `json:"value"`
			Error string          `json:"error"`
		} `json:"owners"`
	}
	if err := json.Unmarshal(c.do(eastViewer, http.MethodPost, "/calc-rules:preview", probe, http.StatusOK), &leak); err != nil {
		t.Fatal(err)
	}
	if len(leak.Owners) != 1 || len(leak.Owners[0].Value) != 0 || !strings.Contains(leak.Owners[0].Error, "outside") {
		t.Fatalf("the east expression preview %+v, want sys-e withheld (a member is outside the read scope)", leak.Owners)
	}
	leak.Owners = nil
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules:preview", probe, http.StatusOK), &leak); err != nil {
		t.Fatal(err)
	}
	if len(leak.Owners) != 1 || string(leak.Owners[0].Value) == "" {
		t.Fatalf("the fleet expression preview %+v, want a value (every member in scope)", leak.Owners)
	}

	// Saved and computed, its state reads the same way.
	rule["name"] = "av-mean"
	c.do(ownerTok, http.MethodPost, "/calc-rules", rule, http.StatusCreated)
	if _, err := gw.DrainCalcPending(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var st calcStateBody
	if err := json.Unmarshal(c.do(eastViewer, http.MethodGet, "/calc-rules/av-mean/state", nil, http.StatusOK), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Owners) != 1 || st.Owners[0].Owner != "sys-e" || string(st.Owners[0].Value) != "20" || st.Owners[0].RuleVersion != 1 ||
		len(st.Owners[0].Skipped) != 0 || st.Owners[0].SkippedHidden != 1 {
		t.Fatalf("the east state %+v, want sys-e alone at 20 with w1 hidden", st)
	}
	st = calcStateBody{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/calc-rules/av-mean/state", nil, http.StatusOK), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Owners) != 2 || st.Total != 2 {
		t.Fatalf("the fleet state %+v, want two owners of two", st)
	}
	// The route lists at most limit owners and says how many there are.
	one := calcStateBody{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/calc-rules/av-mean/state?limit=1", nil, http.StatusOK), &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Owners) != 1 || one.Total != 2 {
		t.Fatalf("the state at limit=1 %+v, want one owner of two", one)
	}
	c.do(ownerTok, http.MethodGet, "/calc-rules/av-mean/state?limit=0", nil, http.StatusUnprocessableEntity)
	for _, o := range st.Owners {
		if o.Owner == "sys-e" && (len(o.Skipped) != 1 || o.Skipped[0].Component != "w1") {
			t.Fatalf("the fleet names w1 as skipped: %+v", o)
		}
	}
	// A second writer is refused by the preview as by a save.
	rule["name"] = "another"
	var p problem
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules:preview", rule, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Detail+string(errorsJSON(p.Errors)), "av-mean") {
		t.Fatalf("a second writer's preview: %+v, want av-mean named", p)
	}
	c.do(ownerTok, http.MethodGet, "/calc-rules/nobody/state", nil, http.StatusNotFound)
}

func errorsJSON(v any) []byte { b, _ := json.Marshal(v); return b }
