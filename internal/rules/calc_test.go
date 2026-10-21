package rules

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

func in(as, key, valueType string) TypedInput {
	return TypedInput{As: as, Key: key, ValueType: valueType}
}

// A component rule's expression reads each named input as one value of its
// key's type, and must yield the output key's type.
func TestCompileCalcComponentExpression(t *testing.T) {
	inputs := []TypedInput{in("power", "display-power", ValueText), in("call", "in-call", ValueBool), in("people", "occupancy-count", ValueNumber)}
	c, err := CompileCalc(OwnerComponent, inputs, CalcReduce{Expr: `power == "on" || call || people > 0`}, ValueBool)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Env.Bindings()
	if len(got) != 3 || got[0].Name != "power" || got[0].Type != "string" || got[1].Type != "bool" || got[2].Type != "float64" {
		t.Fatalf("bindings %+v, want power string, call bool, people float64", got)
	}
	if c.Env.Result() != expression.Bool || c.Program == nil || c.Reducer != nil {
		t.Fatalf("compiled %+v, want a bool expression", c)
	}
	if c.Env.Budget() != CalcBudget() || !slices.Equal(c.Env.Builtins(), expression.CriteriaBuiltins()) {
		t.Errorf("calc env budget %+v builtins %v, want the calc budget and the criteria builtins", c.Env.Budget(), c.Env.Builtins())
	}
	// A number output takes an int expression too (it widens).
	if _, err := CompileCalc(OwnerComponent, inputs[2:], CalcReduce{Expr: "people * 2"}, ValueNumber); err != nil {
		t.Errorf("an int-valued expression for a number output: %v", err)
	}
}

// A system rule's expression reads each named input as the list of its
// members' values.
func TestCompileCalcSystemExpression(t *testing.T) {
	c, err := CompileCalc(OwnerSystem, []TypedInput{in("temps", "room-temperature", ValueNumber)},
		CalcReduce{Expr: "count(temps, # > 30) > 0"}, ValueBool)
	if err != nil {
		t.Fatal(err)
	}
	if b := c.Env.Bindings(); len(b) != 1 || b[0].Type != "[]float64" {
		t.Fatalf("bindings %+v, want temps []float64", b)
	}
}

// A reduce that does not compile is refused with its position, and a result
// that is not the output key's type is a type error.
func TestCompileCalcRefusals(t *testing.T) {
	num := []TypedInput{in("t", "room-temperature", ValueNumber)}
	for name, tc := range map[string]struct {
		owner  string
		inputs []TypedInput
		reduce CalcReduce
		out    string
		field  string
		phase  expression.Phase
	}{
		"unknown name":      {OwnerComponent, num, CalcReduce{Expr: "t + nope"}, ValueNumber, "reduce.expr", expression.PhaseType},
		"wrong result":      {OwnerComponent, num, CalcReduce{Expr: "t > 1"}, ValueNumber, "reduce.expr", expression.PhaseType},
		"parse":             {OwnerComponent, num, CalcReduce{Expr: "t +"}, ValueNumber, "reduce.expr", expression.PhaseParse},
		"unvetted builtin":  {OwnerComponent, num, CalcReduce{Expr: "sum([t])"}, ValueNumber, "reduce.expr", expression.PhaseType},
		"both":              {OwnerComponent, num, CalcReduce{Expr: "t", Reducer: "average"}, ValueNumber, "reduce", ""},
		"neither":           {OwnerComponent, num, CalcReduce{}, ValueNumber, "reduce", ""},
		"unknown reducer":   {OwnerComponent, num, CalcReduce{Reducer: "median"}, ValueNumber, "reduce.reducer", ""},
		"reducer two in":    {OwnerComponent, append(num, in("u", "uptime", ValueNumber)), CalcReduce{Reducer: "average"}, ValueNumber, "reduce.reducer", ""},
		"reducer lane":      {OwnerComponent, []TypedInput{in("p", "video-input", ValueText)}, CalcReduce{Reducer: "average"}, ValueNumber, "reduce.reducer", ""},
		"reducer out type":  {OwnerComponent, num, CalcReduce{Reducer: "average"}, ValueText, "output_key", ""},
		"any on text":       {OwnerComponent, []TypedInput{in("p", "video-input", ValueText)}, CalcReduce{Reducer: "any"}, ValueBool, "reduce.reducer", ""},
		"majority type":     {OwnerSystem, []TypedInput{in("p", "video-input", ValueText)}, CalcReduce{Reducer: "majority"}, ValueBool, "output_key", ""},
		"no inputs":         {OwnerComponent, nil, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs", ""},
		"bad as":            {OwnerComponent, []TypedInput{in("in-call", "in-call", ValueBool)}, CalcReduce{Expr: "true"}, ValueBool, "inputs[0].as", ""},
		"keyword as":        {OwnerComponent, []TypedInput{in("not", "in-call", ValueBool)}, CalcReduce{Expr: "true"}, ValueBool, "inputs[0].as", ""},
		"duplicate as":      {OwnerComponent, []TypedInput{in("a", "in-call", ValueBool), in("a", "uptime", ValueNumber)}, CalcReduce{Expr: "a"}, ValueBool, "inputs[1].as", ""},
		"duplicate key":     {OwnerComponent, []TypedInput{in("a", "uptime", ValueNumber), in("b", "uptime", ValueNumber)}, CalcReduce{Expr: "a + b"}, ValueNumber, "inputs[1].key", ""},
		"unknown owner":     {"location", num, CalcReduce{Expr: "t"}, ValueNumber, "owner_kind", ""},
		"unknown out type":  {OwnerComponent, num, CalcReduce{Expr: "t"}, "matrix", "output_key", ""},
		"unknown input typ": {OwnerComponent, []TypedInput{in("t", "x", "matrix")}, CalcReduce{Expr: "t"}, ValueNumber, "inputs[0].key", ""},
	} {
		_, err := CompileCalc(tc.owner, tc.inputs, tc.reduce, tc.out)
		var ce *CalcError
		if !errors.As(err, &ce) {
			t.Errorf("%s: err %v, want a *CalcError", name, err)
			continue
		}
		if ce.Field != tc.field {
			t.Errorf("%s: field %q, want %q (%v)", name, ce.Field, tc.field, err)
		}
		var ee *expression.Error
		if tc.phase != "" && (!errors.As(err, &ee) || ee.Phase != tc.phase || ee.Line == 0) {
			t.Errorf("%s: %v, want a positioned %s error", name, err, tc.phase)
		}
	}
}

// Too many inputs is refused, so a rule's environment stays small.
func TestCompileCalcInputCap(t *testing.T) {
	var many []TypedInput
	for i := 0; i <= MaxCalcInputs; i++ {
		many = append(many, in("i"+strings.Repeat("x", i), "k"+strings.Repeat("x", i), ValueNumber))
	}
	var ce *CalcError
	if _, err := CompileCalc(OwnerComponent, many, CalcReduce{Expr: "1.0"}, ValueNumber); !errors.As(err, &ce) || ce.Field != "inputs" {
		t.Fatalf("%d inputs: %v, want refused on inputs", len(many), err)
	}
}

// Every named reducer is declared once: which input value types it folds and
// the type it yields (empty: the input's own).
func TestCalcReducers(t *testing.T) {
	want := map[string]struct {
		accepts []string
		result  string
	}{
		"average":  {[]string{ValueNumber}, ValueNumber},
		"min":      {[]string{ValueNumber}, ValueNumber},
		"max":      {[]string{ValueNumber}, ValueNumber},
		"sum":      {[]string{ValueNumber}, ValueNumber},
		"count":    {[]string{ValueNumber}, ValueNumber},
		"majority": {[]string{ValueText, ValueBool}, ""},
		"any":      {[]string{ValueBool}, ValueBool},
		"all":      {[]string{ValueBool}, ValueBool},
	}
	got := CalcReducers()
	if len(got) != len(want) {
		t.Fatalf("%d reducers, want %d", len(got), len(want))
	}
	for _, r := range got {
		w, ok := want[r.Name]
		if !ok || !slices.Equal(r.Accepts, w.accepts) || r.Result != w.result || r.Doc == "" {
			t.Errorf("reducer %+v, want %+v with a doc", r, w)
		}
	}
	for _, tc := range []struct {
		reducer, input, output string
	}{
		{"average", ValueNumber, ValueNumber},
		{"count", ValueNumber, ValueNumber},
		{"majority", ValueText, ValueText},
		{"majority", ValueBool, ValueBool},
		{"all", ValueBool, ValueBool},
	} {
		c, err := CompileCalc(OwnerSystem, []TypedInput{in("x", "k", tc.input)}, CalcReduce{Reducer: tc.reducer}, tc.output)
		if err != nil || c.Reducer == nil || c.Reducer.Name != tc.reducer || c.Program != nil {
			t.Errorf("%s over %s: %+v (%v), want the named reducer", tc.reducer, tc.input, c, err)
		}
	}
}

// The calc site is registered beside criteria, its bindings described rather
// than listed (they are each rule's inputs), with its reducers, a budget
// larger than criteria's and within the ceiling.
func TestCalcSite(t *testing.T) {
	s, ok := LookupSite("calc")
	if !ok {
		t.Fatal("the calc site is not registered")
	}
	if !s.KeyRequired || s.Bindings == "" || len(s.Reducers) != len(CalcReducers()) {
		t.Fatalf("calc site %+v, want a key (the output), a bindings description and the reducers", s)
	}
	for _, vt := range []string{ValueNumber, ValueText, ValueBool} {
		v, ok := s.Variant(vt)
		if !ok || len(v.Env.Bindings()) != 0 || v.Env.Budget() != CalcBudget() {
			t.Errorf("variant %s: %+v, want one with no fixed bindings and the calc budget", vt, v)
		}
	}
	b, c, ceil := CalcBudget(), expression.CriteriaBudget(), expression.CeilingBudget()
	if b.Iterations <= c.Iterations || b.Iterations > ceil.Iterations || b.Memory > ceil.Memory || b.Deadline > ceil.Deadline {
		t.Errorf("calc budget %+v: want larger than criteria %+v and within the ceiling %+v", b, c, ceil)
	}
	f := s.Facts()
	if f.Bindings == "" || len(f.Reducers) != len(CalcReducers()) {
		t.Errorf("facts %+v, want the bindings description and the reducers", f)
	}
}

// The graph from each enabled rule's input keys to its output key stays
// acyclic: a rule that would close a loop is found, with the chain of keys
// from the rule's input around to itself and the rules along it.
func TestCalcCycle(t *testing.T) {
	graph := []CalcEdge{
		{Rule: "avg-temp", Inputs: []string{"room-temperature"}, Output: "avg-temp"},
		{Rule: "hot", Inputs: []string{"avg-temp", "occupancy-count"}, Output: "room-hot"},
	}
	if c := CalcCycle(graph, CalcEdge{Rule: "busy", Inputs: []string{"occupancy-count"}, Output: "room-busy"}); c != nil {
		t.Fatalf("an acyclic addition found %+v", c)
	}
	c := CalcCycle(graph, CalcEdge{Rule: "loop", Inputs: []string{"room-hot", "uptime"}, Output: "room-temperature"})
	if c == nil {
		t.Fatal("a closing rule found no cycle")
	}
	if want := []string{"room-hot", "room-temperature", "avg-temp", "room-hot"}; !reflect.DeepEqual(c.Keys, want) {
		t.Errorf("cycle keys %v, want %v", c.Keys, want)
	}
	if want := []string{"loop", "avg-temp", "hot"}; !reflect.DeepEqual(c.Rules, want) {
		t.Errorf("cycle rules %v, want %v", c.Rules, want)
	}
	if got := c.String(); got != "room-hot -> room-temperature -> avg-temp -> room-hot" {
		t.Errorf("cycle reads %q", got)
	}
	// A rule reading its own output is the shortest cycle.
	self := CalcCycle(nil, CalcEdge{Rule: "self", Inputs: []string{"x"}, Output: "x"})
	if self == nil || self.String() != "x -> x" {
		t.Errorf("a self loop: %+v", self)
	}
	// The candidate replaces its own stored edge (an update), so a rule never
	// cycles against the version of itself it replaces.
	graph = append(graph, CalcEdge{Rule: "busy", Inputs: []string{"room-hot"}, Output: "room-busy"})
	if c := CalcCycle(graph, CalcEdge{Rule: "busy", Inputs: []string{"occupancy-count"}, Output: "room-busy"}); c != nil {
		t.Errorf("an update against its own stored edge found %+v", c)
	}
}
