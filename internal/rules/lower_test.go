package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

func short(t *testing.T, op string, v any) Criteria {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return Criteria{Op: op, Value: b}
}

// The lowering's literal forms: a comparison of value against a JSON-quoted
// string or a float literal.
func TestLowerForms(t *testing.T) {
	for _, tc := range []struct {
		lane     Lane
		dataType string
		c        Criteria
		want     string
	}{
		{LaneProperty, "string", short(t, "eq", "down"), `value == "down"`},
		{LaneProperty, "string", short(t, "ne", "up"), `value != "up"`},
		{LaneProperty, "json", short(t, "eq", `{"a":1}`), `value == "{\"a\":1}"`},
		{LaneProperty, "", short(t, "eq", "down"), `value == "down"`},
		{LaneProperty, "bool", short(t, "eq", "true"), `value == true`},
		{LaneProperty, "bool", short(t, "ne", "false"), `value != false`},
		{LaneProperty, "bool", short(t, "eq", "on"), `false`},
		{LaneProperty, "bool", short(t, "ne", "on"), `true`},
		{LaneMetric, "float", short(t, "ge", 80), `value >= 80.0`},
		{LaneMetric, "int", short(t, "gt", 0.5), `value > 0.5`},
		{LaneMetric, "float", short(t, "lt", -5), `value < -5.0`},
		{LaneMetric, "float", short(t, "le", 1e21), `value <= 1e+21`},
		{LaneMetric, "float", short(t, "eq", 0), `value == 0.0`},
		{LaneMetric, "float", Criteria{Op: "ne", Value: []byte("12345678901234567890")}, `value != 12345678901234567890.0`},
	} {
		got, err := Lower(tc.c, tc.lane, tc.dataType)
		if err != nil {
			t.Errorf("%s %s %+v: %v", tc.lane, tc.dataType, tc.c, err)
			continue
		}
		if got.Expr != tc.want || got.Op != "" || got.Value != nil {
			t.Errorf("%s %s %s %s = %+v, want {expr: %s}", tc.lane, tc.dataType, tc.c.Op, tc.c.Value, got, tc.want)
		}
	}
	// An expression lowers to itself; a criterion naming both forms, or an
	// invalid short form, is refused.
	if got, err := Lower(Criteria{Expr: "value > 1"}, LaneMetric, "float"); err != nil || got.Expr != "value > 1" {
		t.Errorf("an expression lowered to %+v, %v", got, err)
	}
	for _, bad := range []struct {
		lane Lane
		c    Criteria
	}{
		{LaneMetric, Criteria{Expr: "value > 1", Op: "gt", Value: []byte("1")}},
		{LaneProperty, short(t, "gt", "down")},
		{LaneProperty, short(t, "eq", 5)},
		{LaneMetric, short(t, "gt", "hot")},
		{LaneMetric, short(t, "like", 1)},
		{LaneMetric, Criteria{Op: "eq"}},
		{LaneMetric, Criteria{}},
	} {
		if _, err := Lower(bad.c, bad.lane, ""); err == nil {
			t.Errorf("%s %+v lowered", bad.lane, bad.c)
		}
	}
}

// The heart of the slice: for every short-form criterion and every sample,
// the lowered expression decides exactly what the {op,value} evaluator did
// (legacy_test.go), with and without a clear criterion. A sample the old
// evaluator refused (an error, so a hold) is one the new refuses too.
func TestLoweringDecidesIdentically(t *testing.T) {
	hostile := []string{
		"down", "up", "", " ", "Down", `quote"d`, `back\slash`, "line\nbreak", "tab\there", "nul\x00byte",
		"uni code", "émoji 🚀", `") || true || ("`, `now()`, "$env", `{"a":1}`, "</script>&", "\u0001ctl",
	}
	type kase struct {
		lane     Lane
		dataType string
		ops      []string
		values   []any
		samples  []any
	}
	nums := []any{80, 0.5, -5, 0, 1e21, 0.1, 1e-7, 12345678901234567, 2.5e-300}
	samples := []any{80.0, 80.000001, 79.99999, 0.5, 0.1, 0.30000000000000004, -5.0, -4.9, 0.0, math.Copysign(0, -1),
		1e21, 1e-7, 2.5e-300, 12345678901234567.0, 12345678901234568.0, math.NaN(), math.Inf(1), math.Inf(-1),
		"81", "abc", "NaN", "Inf", 3}
	var hs []any
	for _, h := range hostile {
		hs = append(hs, h)
	}
	cases := []kase{
		{LaneProperty, "string", []string{"eq", "ne"}, hs, hs},
		{LaneProperty, "json", []string{"eq", "ne"}, hs, hs},
		{LaneProperty, "bool", []string{"eq", "ne"}, []any{"true", "false", "on", ""}, []any{"true", "false"}},
		{LaneMetric, "float", []string{"eq", "ne", "gt", "ge", "lt", "le"}, nums, samples},
		{LaneMetric, "int", []string{"eq", "ne", "gt", "ge", "lt", "le"}, nums, samples},
	}
	n := 0
	for _, tc := range cases {
		for _, op := range tc.ops {
			for _, v := range tc.values {
				old := legacyCriteria{Op: op, Value: short(t, op, v).Value}
				if err := old.Validate(tc.lane); err != nil {
					t.Fatalf("fixture %s %v invalid on %s: %v", op, v, tc.lane, err)
				}
				for _, clearOp := range append([]string{""}, tc.ops...) {
					var oldClear *legacyCriteria
					var newClear *Criteria
					if clearOp != "" {
						oc := legacyCriteria{Op: clearOp, Value: old.Value}
						oldClear = &oc
						nc := Criteria{Op: clearOp, Value: old.Value}
						newClear = &nc
					}
					j, err := Compile(tc.lane, tc.dataType, Criteria{Op: op, Value: old.Value}, newClear)
					if err != nil {
						t.Fatalf("%s %s %s %v (clear %s): %v", tc.lane, tc.dataType, op, v, clearOp, err)
					}
					for _, s := range tc.samples {
						want, wantErr := legacyDecide(old, oldClear, s)
						got, gotErr := j.Decide(Sample{Value: s, Key: "k"})
						n++
						if (wantErr != nil) != (gotErr != nil) || (wantErr == nil && got != want) {
							t.Errorf("%s %s fire %s %v clear %q, sample %#v: old %v (%v), new %v (%v) via %q",
								tc.lane, tc.dataType, op, v, clearOp, s, want, wantErr, got, gotErr, j.Fire().Expr)
						}
					}
				}
			}
		}
	}
	t.Logf("%d decisions compared", n)
}

// A non-bool expression, an unknown binding, or a parse error is refused at
// compile time, naming which criteria and where.
func TestCompileRefusesWithPosition(t *testing.T) {
	for _, tc := range []struct {
		fire, clear string
		field       string
		line, col   int
	}{
		{"value + 1", "", "fire_criteria", 1, 1},
		{"value > 1 &&\n  humidity > 2", "", "fire_criteria", 2, 3},
		{"value > 1", "value <", "clear_criteria", 1, 7},
		{"now() > 0", "", "fire_criteria", 1, 1},
	} {
		var clear *Criteria
		if tc.clear != "" {
			clear = &Criteria{Expr: tc.clear}
		}
		_, err := Compile(LaneMetric, "float", Criteria{Expr: tc.fire}, clear)
		var ce *CriteriaError
		var ee *expression.Error
		if !errors.As(err, &ce) || !errors.As(err, &ee) {
			t.Errorf("%q / %q: err %v, want a CriteriaError over an expression.Error", tc.fire, tc.clear, err)
			continue
		}
		if ce.Field != tc.field || ee.Line != tc.line || ee.Column != tc.col {
			t.Errorf("%q / %q: %s at %d:%d, want %s at %d:%d", tc.fire, tc.clear, ce.Field, ee.Line, ee.Column, tc.field, tc.line, tc.col)
		}
	}
}

// The prepared input: value typed by the lane and data type, and the key,
// instance and component bound beside it.
func TestPreparedInput(t *testing.T) {
	j, err := Compile(LaneProperty, "string", Criteria{Expr: `value == "down" && instance != "aux" && component.type == "display" && key == "endpoint-reachable" && component.name startsWith "lobby"`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := Sample{Value: "down", Key: "endpoint-reachable", Instance: "tcp", Component: Component{Name: "lobby-1", Type: "display"}}
	if got, err := j.Decide(s); err != nil || got != Fire {
		t.Errorf("Decide = %v, %v; want Fire", got, err)
	}
	s.Instance = "aux"
	if got, err := j.Decide(s); err != nil || got != Clear {
		t.Errorf("aux: Decide = %v, %v; want Clear (the negation)", got, err)
	}
	// A bool property is a bool; text that is not true or false holds.
	b, err := Compile(LaneProperty, "bool", Criteria{Expr: "value"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]Action{"true": Fire, "false": Clear} {
		if got, err := b.Decide(Sample{Value: in}); err != nil || got != want {
			t.Errorf("bool %q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []any{"TRUE", "1", "yes", 1.0} {
		if _, err := b.Decide(Sample{Value: in}); err == nil {
			t.Errorf("bool %v decided; an unparseable value holds", in)
		}
	}
	// A metric's string type is refused at compile time.
	if _, err := Compile(LaneMetric, "float", Criteria{Expr: `value == "80"`}, nil); err == nil {
		t.Error("a metric compared to text compiled")
	}
}

// A hostile sample value is data, never executed.
func TestHostileSampleIsData(t *testing.T) {
	j, err := Compile(LaneProperty, "string", Criteria{Expr: `value == "down"`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{`") || true || ("`, "now()", "$env.value", strings.Repeat("x", 5000)} {
		if got, err := j.Decide(Sample{Value: h}); err != nil || got != Clear {
			t.Errorf("%q = %v, %v; want Clear", h, got, err)
		}
	}
}

// A run over the criteria budget is an error the caller holds on.
func TestOverBudgetIsAnError(t *testing.T) {
	j, err := Compile(LaneProperty, "string", Criteria{Expr: `value + value + value + value contains "zz"`}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.Decide(Sample{Value: strings.Repeat("a", 5000)})
	if !errors.Is(err, expression.ErrRunBudget) {
		t.Errorf("err %v, want ErrRunBudget", err)
	}
}

// Compiling counts, so a caller can prove it compiles once per rule.
func TestCompileCount(t *testing.T) {
	before := CompileCount()
	if _, err := Compile(LaneMetric, "float", Criteria{Expr: fmt.Sprintf("value > %d.5", 7)}, &Criteria{Expr: "value < 1.0"}); err != nil {
		t.Fatal(err)
	}
	if n := CompileCount() - before; n != 1 {
		t.Errorf("CompileCount moved %d, want 1 per judge", n)
	}
}
