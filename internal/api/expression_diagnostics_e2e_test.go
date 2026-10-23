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

type diagFix struct {
	Title       string `json:"title"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	EndLine     int    `json:"end_line"`
	EndColumn   int    `json:"end_column"`
	Replacement string `json:"replacement"`
}

type diagOut struct {
	OK          bool `json:"ok"`
	Diagnostics []struct {
		Line     int      `json:"line"`
		Column   int      `json:"column"`
		Phase    string   `json:"phase"`
		Message  string   `json:"message"`
		Code     string   `json:"code"`
		Severity string   `json:"severity"`
		Fix      *diagFix `json:"fix"`
	} `json:"diagnostics"`
}

type ruleNote struct {
	Field   string `json:"field"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// TestExpressionDiagnosticsAPI drives the diagnostics registry over HTTP
// (#1021): validate answers each diagnostic's code, severity and fix, an
// error refusing (ok false) and a warning or hint riding along (ok true);
// a rule's create refuses an error rule's expression with its code and
// saves a warning's, which its reads carry as a note.
func TestExpressionDiagnosticsAPI(t *testing.T) {
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
	viewer := setupAllViewer(t, ctx, dsn, "diag-viewer")
	for _, p := range []string{"in-call", "busy-flag"} {
		c.do(ownerTok, http.MethodPost, "/property-types", map[string]any{"name": p, "data_type": "bool"}, http.StatusCreated)
	}
	validate := func(body map[string]any) diagOut {
		t.Helper()
		var out diagOut
		if err := json.Unmarshal(c.do(viewer, http.MethodPost, "/expressions:validate", body, http.StatusOK), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	calc := func(src string) map[string]any {
		return map[string]any{"site": "calc", "source": src, "key": "busy-flag",
			"inputs": []map[string]any{{"alias": "c", "key": "in-call"}}}
	}

	// An error rule refuses, with its code, severity and fix.
	got := validate(calc("sum(c) > 0"))
	if got.OK || len(got.Diagnostics) != 1 {
		t.Fatalf("sum(c) > 0: %+v, want one refusal", got)
	}
	d := got.Diagnostics[0]
	if d.Code != "sum-of-bools" || d.Severity != "error" || d.Phase != "type" || d.Line != 1 || d.Column != 1 ||
		d.Fix == nil || *d.Fix != (diagFix{Title: "Use count", Line: 1, Column: 1, EndLine: 1, EndColumn: 4, Replacement: "count"}) {
		t.Fatalf("sum(c) > 0 diagnostic %+v (fix %+v)", d, d.Fix)
	}
	// Expr's own refusal has no code and is an error.
	if got := validate(calc("c + 1 > 0")); got.OK || got.Diagnostics[0].Code != "" || got.Diagnostics[0].Severity != "error" {
		t.Fatalf("c + 1 > 0: %+v", got)
	}
	// A hint rides along with an expression that compiles.
	got = validate(calc("count(c, #) == len(c)"))
	if !got.OK || len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "count-equals-len" || got.Diagnostics[0].Severity != "hint" ||
		got.Diagnostics[0].Phase != "lint" || got.Diagnostics[0].Fix == nil || got.Diagnostics[0].Fix.Replacement != "all(c, #)" {
		t.Fatalf("count == len: %+v", got)
	}
	// A warning too.
	got = validate(map[string]any{"site": "criteria", "source": "value == 21.5", "key": "room-temperature"})
	if !got.OK || len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "float-equality" || got.Diagnostics[0].Severity != "warning" ||
		got.Diagnostics[0].Column != 7 || got.Diagnostics[0].Fix != nil {
		t.Fatalf("value == 21.5: %+v", got)
	}

	// A calc rule an error rule refuses is a 422 carrying the code.
	var p problem
	rule := map[string]any{"name": "counts", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "c", "key": "in-call"}}, "reduce": map[string]any{"expr": "sum(c) > 0"}, "output_key": "busy-flag"}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", rule, http.StatusUnprocessableEntity), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Errors) != 1 || p.Errors[0].Location != "body.reduce.expr" || p.Errors[0].Value["code"] != "sum-of-bools" {
		t.Fatalf("create: %+v, want body.reduce.expr with code sum-of-bools", p)
	}
	// One whose reduce only warns saves, and reads the warning back.
	rule["reduce"] = map[string]any{"expr": "true"}
	var made struct {
		CalcWarnings []ruleNote `json:"calc_warnings"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/calc-rules", rule, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if len(made.CalcWarnings) != 1 || made.CalcWarnings[0].Code != "unused-alias" || made.CalcWarnings[0].Field != "reduce.expr" {
		t.Fatalf("calc_warnings %+v, want unused-alias on reduce.expr", made.CalcWarnings)
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/calc-rules/counts", nil, http.StatusOK), &made); err != nil || len(made.CalcWarnings) != 1 {
		t.Fatalf("get calc_warnings %+v (%v)", made.CalcWarnings, err)
	}

	// An event rule's warning saves and reads back on its criteria.
	var ev struct {
		CriteriaWarnings []ruleNote `json:"criteria_warnings"`
	}
	er := map[string]any{"name": "exactly-warm", "sample_key": "room-temperature",
		"fire_criteria": map[string]any{"expr": "value == 21.5"}, "fire_event_type": "condition-started"}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/event-rules", er, http.StatusCreated), &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.CriteriaWarnings) != 1 || ev.CriteriaWarnings[0] != (ruleNote{Field: "fire_criteria", Line: 1, Column: 7,
		Message: ev.CriteriaWarnings[0].Message, Code: "float-equality"}) || ev.CriteriaWarnings[0].Message == "" {
		t.Fatalf("criteria_warnings %+v", ev.CriteriaWarnings)
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/event-rules/exactly-warm", nil, http.StatusOK), &ev); err != nil || len(ev.CriteriaWarnings) != 1 {
		t.Fatalf("get criteria_warnings %+v (%v)", ev.CriteriaWarnings, err)
	}
}
