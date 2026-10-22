package rules

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"
)

func mustCompile(t *testing.T, owner string, inputs []TypedInput, reduce CalcReduce, out string) *CalcProgram {
	t.Helper()
	c, err := CompileCalc(owner, inputs, reduce, out)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// oldFold is the named reducer the calc rules ran before #1018, kept here as
// the oracle the list functions are proven against: each instance's value
// folded into one.
func oldFold(name string, vs []any) any {
	switch name {
	case "count":
		return float64(len(vs))
	case "average", "min", "max", "sum":
		acc, sum := vs[0].(float64), 0.0
		for _, v := range vs {
			f := v.(float64)
			sum += f
			if (name == "min" && f < acc) || (name == "max" && f > acc) {
				acc = f
			}
		}
		switch name {
		case "average":
			return sum / float64(len(vs))
		case "sum":
			return sum
		}
		return acc
	case "any", "all":
		want := name == "any"
		for _, v := range vs {
			if v.(bool) == want {
				return want
			}
		}
		return !want
	case "majority":
		counts, byText := map[string]int{}, map[string]any{}
		for _, v := range vs {
			k := FormatCalcValue(v)
			counts[k]++
			byText[k] = v
		}
		keys := slices.Sorted(maps.Keys(counts))
		best := keys[0]
		for _, k := range keys[1:] {
			if counts[k] > counts[best] {
				best = k
			}
		}
		return byText[best]
	}
	panic(name)
}

// reducerExprs is each retired reducer as the expression the data migration
// writes for it (db/migrations, the calc_rule_reduce_expr backfill), over the
// input's alias x.
var reducerExprs = map[string]string{
	"average": "avg(x)", "min": "min(x)", "max": "max(x)", "sum": "sum(x)", "count": "len(x)",
	"majority": "majority(x)", "any": "any(x)", "all": "all(x)",
}

// The list functions compute what the named reducers did (#1018): over the
// same values, on a component (its instances) and on a system (every
// member's instances), each retired reducer's expression equals its fold,
// majority's tie included.
func TestCalcListFunctionsEqualTheReducers(t *testing.T) {
	nums := [][]any{{3.0}, {3.0, 1.0, 5.0, 3.0}, {-2.5, 7.25}}
	texts := [][]any{{"hdmi2", "hdmi1", "hdmi2"}, {"b", "a", "a", "b"}, {"x"}}
	bools := [][]any{{true, false, false}, {false, true}, {true, false}, {true, true}, {false}}
	for _, tc := range []struct {
		reducer, vt, out string
		cases            [][]any
	}{
		{"average", ValueNumber, ValueNumber, nums}, {"min", ValueNumber, ValueNumber, nums}, {"max", ValueNumber, ValueNumber, nums},
		{"sum", ValueNumber, ValueNumber, nums}, {"count", ValueNumber, ValueNumber, nums}, {"count", ValueText, ValueNumber, texts},
		{"count", ValueBool, ValueNumber, bools}, {"majority", ValueText, ValueText, texts}, {"majority", ValueBool, ValueBool, bools},
		{"any", ValueBool, ValueBool, bools}, {"all", ValueBool, ValueBool, bools},
	} {
		src := reducerExprs[tc.reducer]
		comp := mustCompile(t, OwnerComponent, []TypedInput{in("x", "k", tc.vt)}, CalcReduce{Expr: src}, tc.out)
		sys := mustCompile(t, OwnerSystem, []TypedInput{in("x", "k", tc.vt)}, CalcReduce{Expr: src}, tc.out)
		for _, vs := range tc.cases {
			want := oldFold(tc.reducer, vs)
			if got, err := comp.Reduce([][]any{vs}); err != nil || got != want {
				t.Errorf("component %s over %v = %v (%v), the %s reducer gave %v", src, vs, got, err, tc.reducer, want)
			}
			// The same values spread over members, the first holding two
			// instances: the system's list is every member's values.
			var members []CalcMember
			for i := 0; i < len(vs); {
				n := 1
				if i == 0 && len(vs) > 1 {
					n = 2
				}
				members = append(members, CalcMember{ID: fmt.Sprint(i), Name: fmt.Sprintf("m%02d", i), Values: [][]any{vs[i : i+n]}, Record: []any{nil}})
				i += n
			}
			if got, err := sys.ReduceSystem(members); err != nil || got != want {
				t.Errorf("system %s over %v = %v (%v), the %s reducer gave %v", src, vs, got, err, tc.reducer, want)
			}
		}
	}
}

// A component expression reads each input as the list of its values; a
// single-instance key is a one-element list.
func TestCalcReduceExpression(t *testing.T) {
	c := mustCompile(t, OwnerComponent, []TypedInput{in("power", "p", ValueText), in("call", "c", ValueBool), in("people", "o", ValueNumber)},
		CalcReduce{Expr: `power[0] == "on" || any(call) || max(people) > 0`}, ValueBool)
	got, err := c.Reduce([][]any{{"off"}, {false}, {2.0}})
	if err != nil || got != true {
		t.Fatalf("component expression = %v (%v), want true", got, err)
	}
	m := mustCompile(t, OwnerComponent, []TypedInput{in("tcp", "tcp-connect-time", ValueNumber)}, CalcReduce{Expr: "max(tcp) - min(tcp)"}, ValueNumber)
	if got, err := m.Reduce([][]any{{12.0, 40.0, 19.0}}); err != nil || got != 28.0 {
		t.Fatalf("max(tcp) - min(tcp) = %v (%v), want 28", got, err)
	}
	s := mustCompile(t, OwnerSystem, []TypedInput{in("temps", "t", ValueNumber)}, CalcReduce{Expr: "count(members, .value.temps > 30) * 1.0"}, ValueNumber)
	members := []CalcMember{{ID: "1", Name: "a", Values: [][]any{{31.0}}, Record: []any{31.0}}, {ID: "2", Name: "b", Values: [][]any{{20.0}}, Record: []any{20.0}},
		{ID: "3", Name: "c", Values: [][]any{{40.0}}, Record: []any{40.0}}}
	if got, err := s.ReduceSystem(members); err != nil || got != 2.0 {
		t.Fatalf("system expression = %v (%v), want 2", got, err)
	}
}

// A system expression's records line up per member: a member missing one
// input keeps its other input in its own record, flagged by has, and the
// records are in a stable order (name, then id). The alias lists follow the
// same order.
func TestCalcSystemRecordsAlign(t *testing.T) {
	inputs := []TypedInput{in("a", "ka", ValueNumber), in("b", "kb", ValueNumber)}
	members := []CalcMember{
		{ID: "2", Name: "m2", Values: [][]any{{5.0}, {7.0}}, Record: []any{5.0, 7.0}},
		{ID: "1", Name: "m1", Values: [][]any{{3.0}, nil}, Record: []any{3.0, nil}},
	}
	for expr, want := range map[string]any{
		`members[0].value.a`: 3.0,
		`members[0].has.b`:   false,
		`members[1].value.b`: 7.0,
		`members[0].name == "m1" && members[1].id == "2"`: true,
		`count(members, .has.a && .has.b) * 1.0`:          1.0,
		`a[0] * 10 + a[1]`:                                35.0,
		`len(b) * 1.0`:                                    1.0,
	} {
		out := ValueNumber
		if _, ok := want.(bool); ok {
			out = ValueBool
		}
		c := mustCompile(t, OwnerSystem, inputs, CalcReduce{Expr: expr}, out)
		if got, err := c.ReduceSystem(members); err != nil || got != want {
			t.Errorf("%s = %v (%v), want %v", expr, got, err, want)
		}
	}
	// An input no member holds is missing, and so is every input of a
	// system with no members.
	c := mustCompile(t, OwnerSystem, inputs, CalcReduce{Expr: "1.0"}, ValueNumber)
	var miss *MissingInputError
	if _, err := c.ReduceSystem([]CalcMember{{ID: "1", Values: [][]any{{1.0}, nil}, Record: []any{1.0, nil}}}); !errors.As(err, &miss) || miss.Key != "kb" {
		t.Errorf("an input no member holds: %v", err)
	}
	if _, err := c.ReduceSystem(nil); !errors.Is(err, ErrMissingInput) {
		t.Errorf("no members: %v", err)
	}
}

// An input with no value is missing: the reduce refuses rather than guess,
// and names the input. So does a value of the wrong type, and a result that
// is no finite number.
func TestCalcReduceRefusals(t *testing.T) {
	c := mustCompile(t, OwnerComponent, []TypedInput{in("a", "ka", ValueNumber), in("b", "kb", ValueNumber)}, CalcReduce{Expr: "sum(a) / sum(b)"}, ValueNumber)
	var miss *MissingInputError
	if _, err := c.Reduce([][]any{{1.0}, nil}); !errors.As(err, &miss) || miss.Key != "kb" || miss.Alias != "b" || !errors.Is(err, ErrMissingInput) {
		t.Errorf("a missing input: %v, want a MissingInputError on kb", err)
	}
	if got, err := c.Reduce([][]any{{1.0, 2.0}, {1.0}}); err != nil || got != 3.0 {
		t.Errorf("two values of an input: %v (%v), want 3", got, err)
	}
	if _, err := c.Reduce([][]any{{"x"}, {1.0}}); err == nil {
		t.Error("a text value for a number input was accepted")
	}
	if got, err := c.Reduce([][]any{{1.0}, {0.0}}); err == nil {
		t.Errorf("a division by zero gave %v, want refused as not finite", got)
	}
	r := mustCompile(t, OwnerComponent, []TypedInput{in("a", "ka", ValueNumber)}, CalcReduce{Expr: "avg(a)"}, ValueNumber)
	if _, err := r.Reduce([][]any{{}}); !errors.Is(err, ErrMissingInput) {
		t.Errorf("an input with no values: %v, want missing", err)
	}
	if _, err := r.Reduce([][]any{{math.NaN()}}); err == nil {
		t.Error("a NaN average was accepted")
	}
}

// A reduce that reads its lists only whole is an aggregate; one that reads
// an element, or a member's record, is not.
func TestCalcAggregate(t *testing.T) {
	inputs := []TypedInput{in("t", "k", ValueNumber)}
	for src, want := range map[string]bool{
		"avg(t)":                       true,
		"max(t) - min(t) + len(t)":     true,
		"sum(t) / len(members)":        true,
		"t[0]":                         false,
		"members[0].value.t":           false,
		"count(members, .has.t) + 0.0": false,
	} {
		c := mustCompile(t, OwnerSystem, inputs, CalcReduce{Expr: src}, ValueNumber)
		if got := c.Aggregate(); got != want {
			t.Errorf("%s: Aggregate = %v, want %v", src, got, want)
		}
	}
}

// Stored text reads back as the value type it carries.
func TestCalcValue(t *testing.T) {
	for _, tc := range []struct {
		vt, raw string
		want    any
		ok      bool
	}{
		{ValueText, "hdmi1", "hdmi1", true},
		{ValueBool, "true", true, true},
		{ValueBool, "false", false, true},
		{ValueBool, "yes", nil, false},
		{ValueNumber, "2.5", 2.5, true},
		{ValueNumber, "x", nil, false},
	} {
		got, err := CalcValue(tc.vt, tc.raw)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("CalcValue(%s, %q) = %v (%v)", tc.vt, tc.raw, got, err)
		}
	}
	if FormatCalcValue(true) != "true" || FormatCalcValue("on") != "on" {
		t.Error("FormatCalcValue does not write a property's text")
	}
}
