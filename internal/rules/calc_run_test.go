package rules

import (
	"errors"
	"math"
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

// Each named reducer folds every value of its one input into one value.
func TestCalcFold(t *testing.T) {
	nums := []any{3.0, 1.0, 5.0, 3.0}
	for _, tc := range []struct {
		reducer string
		in      []any
		vt, out string
		want    any
	}{
		{"average", nums, ValueNumber, ValueNumber, 3.0},
		{"min", nums, ValueNumber, ValueNumber, 1.0},
		{"max", nums, ValueNumber, ValueNumber, 5.0},
		{"sum", nums, ValueNumber, ValueNumber, 12.0},
		{"count", nums, ValueNumber, ValueNumber, 4.0},
		{"count", []any{"a", "b"}, ValueText, ValueNumber, 2.0},
		{"majority", []any{"hdmi2", "hdmi1", "hdmi2"}, ValueText, ValueText, "hdmi2"},
		{"majority", []any{"b", "a", "a", "b"}, ValueText, ValueText, "a"}, // a tie: the value that sorts first
		{"majority", []any{true, false, false}, ValueBool, ValueBool, false},
		{"any", []any{false, true}, ValueBool, ValueBool, true},
		{"any", []any{false, false}, ValueBool, ValueBool, false},
		{"all", []any{true, true}, ValueBool, ValueBool, true},
		{"all", []any{true, false}, ValueBool, ValueBool, false},
	} {
		c := mustCompile(t, OwnerComponent, []TypedInput{in("x", "k", tc.vt)}, CalcReduce{Reducer: tc.reducer}, tc.out)
		got, err := c.Reduce([][]any{tc.in})
		if err != nil || got != tc.want {
			t.Errorf("%s over %v = %v (%v), want %v", tc.reducer, tc.in, got, err, tc.want)
		}
	}
}

// A component expression reads each input's one current value; a system
// expression reads each input as the list of its members' values.
func TestCalcReduceExpression(t *testing.T) {
	c := mustCompile(t, OwnerComponent, []TypedInput{in("power", "p", ValueText), in("call", "c", ValueBool), in("people", "o", ValueNumber)},
		CalcReduce{Expr: `power == "on" || call || people > 0`}, ValueBool)
	got, err := c.Reduce([][]any{{"off"}, {false}, {2.0}})
	if err != nil || got != true {
		t.Fatalf("component expression = %v (%v), want true", got, err)
	}
	s := mustCompile(t, OwnerSystem, []TypedInput{in("temps", "t", ValueNumber)}, CalcReduce{Expr: "count(members, .value.temps > 30) * 1.0"}, ValueNumber)
	members := []CalcMember{{ID: "1", Name: "a", Values: [][]any{{31.0}}}, {ID: "2", Name: "b", Values: [][]any{{20.0}}},
		{ID: "3", Name: "c", Values: [][]any{{40.0}}}}
	if got, err := s.ReduceSystem(members); err != nil || got != 2.0 {
		t.Fatalf("system expression = %v (%v), want 2", got, err)
	}
}

// A system expression's records line up per member: a member missing one
// input keeps its other input in its own record, flagged by has, and the
// records are in a stable order (name, then id).
func TestCalcSystemRecordsAlign(t *testing.T) {
	inputs := []TypedInput{in("a", "ka", ValueNumber), in("b", "kb", ValueNumber)}
	members := []CalcMember{
		{ID: "2", Name: "m2", Values: [][]any{{5.0}, {7.0}}},
		{ID: "1", Name: "m1", Values: [][]any{{3.0}, nil}},
	}
	for expr, want := range map[string]any{
		`members[0].value.a`: 3.0,
		`members[0].has.b`:   false,
		`members[1].value.b`: 7.0,
		`members[0].name == "m1" && members[1].id == "2"`: true,
		`count(members, .has.a && .has.b) * 1.0`:          1.0,
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
	// A named reducer folds every member's values of its input.
	r := mustCompile(t, OwnerSystem, inputs[:1], CalcReduce{Reducer: "sum"}, ValueNumber)
	if got, err := r.ReduceSystem([]CalcMember{{ID: "1", Values: [][]any{{1.0, 2.0}}}, {ID: "2", Values: [][]any{{4.0}}}}); err != nil || got != 7.0 {
		t.Errorf("sum over members = %v (%v), want 7", got, err)
	}
	// An input no member holds is missing, and so is every input of a
	// system with no members.
	c := mustCompile(t, OwnerSystem, inputs, CalcReduce{Expr: "1.0"}, ValueNumber)
	var miss *MissingInputError
	if _, err := c.ReduceSystem([]CalcMember{{ID: "1", Values: [][]any{{1.0}, nil}}}); !errors.As(err, &miss) || miss.Key != "kb" {
		t.Errorf("an input no member holds: %v", err)
	}
	if _, err := c.ReduceSystem(nil); !errors.Is(err, ErrMissingInput) {
		t.Errorf("no members: %v", err)
	}
}

// An input with no value is missing: the reduce refuses rather than guess,
// and names the input. So does a component expression input with more than
// one value, a value of the wrong type, and a result that is no finite number.
func TestCalcReduceRefusals(t *testing.T) {
	c := mustCompile(t, OwnerComponent, []TypedInput{in("a", "ka", ValueNumber), in("b", "kb", ValueNumber)}, CalcReduce{Expr: "a / b"}, ValueNumber)
	var miss *MissingInputError
	if _, err := c.Reduce([][]any{{1.0}, nil}); !errors.As(err, &miss) || miss.Key != "kb" || !errors.Is(err, ErrMissingInput) {
		t.Errorf("a missing input: %v, want a MissingInputError on kb", err)
	}
	if _, err := c.Reduce([][]any{{1.0, 2.0}, {1.0}}); err == nil {
		t.Error("two values for a component expression input were accepted")
	}
	if _, err := c.Reduce([][]any{{"x"}, {1.0}}); err == nil {
		t.Error("a text value for a number input was accepted")
	}
	if got, err := c.Reduce([][]any{{1.0}, {0.0}}); err == nil {
		t.Errorf("a division by zero gave %v, want refused as not finite", got)
	}
	r := mustCompile(t, OwnerComponent, []TypedInput{in("a", "ka", ValueNumber)}, CalcReduce{Reducer: "average"}, ValueNumber)
	if _, err := r.Reduce([][]any{{}}); !errors.Is(err, ErrMissingInput) {
		t.Errorf("a reducer over no values: %v, want missing", err)
	}
	if _, err := r.Reduce([][]any{{math.NaN()}}); err == nil {
		t.Error("a NaN average was accepted")
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
