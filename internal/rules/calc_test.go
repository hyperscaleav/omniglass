package rules

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

func in(alias, key, valueType string) TypedInput {
	return TypedInput{Alias: alias, Key: key, ValueType: valueType}
}

// A component rule's expression reads each input by its alias as the list of
// its current values (one per instance), of its key's type, and must yield
// the output key's type (#1018).
func TestCompileCalcComponentExpression(t *testing.T) {
	inputs := []TypedInput{in("power", "display-power", ValueText), in("call", "in-call", ValueBool), in("people", "occupancy-count", ValueNumber)}
	c, err := CompileCalc(OwnerComponent, inputs, CalcReduce{Expr: `majority(power) == "on" || any(call) || max(people) > 0`}, ValueBool)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Env.Bindings()
	if len(got) != 3 || got[0].Name != "power" || got[0].Type != "[]string" || got[1].Type != "[]bool" || got[2].Type != "[]float64" {
		t.Fatalf("bindings %+v, want power []string, call []bool, people []float64", got)
	}
	if c.Env.Result() != expression.Bool || c.Program == nil {
		t.Fatalf("compiled %+v, want a bool expression", c)
	}
	if c.Env.Budget() != CalcBudget() || !slices.Equal(c.Env.Builtins(), CalcBuiltins()) {
		t.Errorf("calc env budget %+v builtins %v, want the calc budget and %v", c.Env.Budget(), c.Env.Builtins(), CalcBuiltins())
	}
	for _, f := range []string{"avg", "majority", "max", "min", "sum", "any", "all", "count"} {
		if !slices.Contains(CalcBuiltins(), f) {
			t.Errorf("the calc site does not admit %s", f)
		}
	}
	// A number output takes an int expression too (it widens).
	if _, err := CompileCalc(OwnerComponent, inputs[2:], CalcReduce{Expr: "len(people) * 2"}, ValueNumber); err != nil {
		t.Errorf("an int-valued expression for a number output: %v", err)
	}
}

// A system rule's expression reads each input by its alias as the list of
// every member's values, and its members as one list of records, each with
// the member's id and name, its own value of each input under value, and
// whether it has one under has, so the inputs of one member stay together.
func TestCompileCalcSystemExpression(t *testing.T) {
	c, err := CompileCalc(OwnerSystem, []TypedInput{in("temps", "room-temperature", ValueNumber), in("on", "power", ValueBool)},
		CalcReduce{Expr: "avg(temps) > 30 || count(members, .has.temps && .value.temps > 30) > 0 || all(on)"}, ValueBool)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, b := range c.Env.Bindings() {
		got[b.Name] = b.Type
	}
	want := map[string]string{"temps": "[]float64", "on": "[]bool", "members": "[]record", "members[].id": "string", "members[].name": "string",
		"members[].value.temps": "float64", "members[].value.on": "bool", "members[].has.temps": "bool", "members[].has.on": "bool"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bindings %v, want %v", got, want)
	}
}

// A reduce that does not compile is refused with its position, and a result
// that is not the output key's type is a type error. A list function given a
// value of the wrong type is placed on the argument.
func TestCompileCalcRefusals(t *testing.T) {
	num := []TypedInput{in("t", "room-temperature", ValueNumber)}
	for name, tc := range map[string]struct {
		owner  string
		inputs []TypedInput
		reduce CalcReduce
		out    string
		field  string
		phase  expression.Phase
		col    int
	}{
		"unknown name":      {OwnerComponent, num, CalcReduce{Expr: "max(t) + nope"}, ValueNumber, "reduce.expr", expression.PhaseType, 10},
		"wrong result":      {OwnerComponent, num, CalcReduce{Expr: "max(t) > 1"}, ValueNumber, "reduce.expr", expression.PhaseType, 1},
		"a list result":     {OwnerComponent, num, CalcReduce{Expr: "t"}, ValueNumber, "reduce.expr", expression.PhaseType, 1},
		"parse":             {OwnerComponent, num, CalcReduce{Expr: "t +"}, ValueNumber, "reduce.expr", expression.PhaseParse, 0},
		"unvetted builtin":  {OwnerComponent, num, CalcReduce{Expr: "mean(t)"}, ValueNumber, "reduce.expr", expression.PhaseType, 1},
		"avg of text":       {OwnerComponent, []TypedInput{in("p", "video-input", ValueText)}, CalcReduce{Expr: "avg(p)"}, ValueNumber, "reduce.expr", expression.PhaseType, 5},
		"any of numbers":    {OwnerComponent, num, CalcReduce{Expr: "any(t)"}, ValueBool, "reduce.expr", expression.PhaseType, 5},
		"majority numbers":  {OwnerSystem, num, CalcReduce{Expr: "majority(t)"}, ValueNumber, "reduce.expr", expression.PhaseType, 10},
		"let shadows alias": {OwnerComponent, num, CalcReduce{Expr: "let t = 1.0; t"}, ValueNumber, "reduce.expr", expression.PhaseType, 5},
		"empty":             {OwnerComponent, num, CalcReduce{}, ValueNumber, "reduce.expr", "", 0},
		"no inputs":         {OwnerComponent, nil, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs", "", 0},
		"bad alias":         {OwnerComponent, []TypedInput{in("in-call", "in-call", ValueBool)}, CalcReduce{Expr: "true"}, ValueBool, "inputs[0].alias", "", 0},
		"keyword alias":     {OwnerComponent, []TypedInput{in("not", "in-call", ValueBool)}, CalcReduce{Expr: "true"}, ValueBool, "inputs[0].alias", "", 0},
		"builtin alias":     {OwnerComponent, []TypedInput{in("len", "in-call", ValueBool)}, CalcReduce{Expr: "true"}, ValueBool, "inputs[0].alias", "", 0},
		"function alias":    {OwnerComponent, []TypedInput{in("avg", "uptime", ValueNumber)}, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs[0].alias", "", 0},
		"members alias":     {OwnerSystem, []TypedInput{in("members", "uptime", ValueNumber)}, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs[0].alias", "", 0},
		"component alias":   {OwnerComponent, []TypedInput{in("component", "uptime", ValueNumber)}, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs[0].alias", "", 0},
		"value alias":       {OwnerComponent, []TypedInput{in("value", "uptime", ValueNumber)}, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs[0].alias", "", 0},
		"has alias":         {OwnerSystem, []TypedInput{in("has", "uptime", ValueNumber)}, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs[0].alias", "", 0},
		"long alias":        {OwnerComponent, []TypedInput{in(strings.Repeat("a", 65), "uptime", ValueNumber)}, CalcReduce{Expr: "1.0"}, ValueNumber, "inputs[0].alias", "", 0},
		"duplicate alias":   {OwnerComponent, []TypedInput{in("a", "in-call", ValueBool), in("a", "uptime", ValueNumber)}, CalcReduce{Expr: "true"}, ValueBool, "inputs[1].alias", "", 0},
		"duplicate key":     {OwnerComponent, []TypedInput{in("a", "uptime", ValueNumber), in("b", "uptime", ValueNumber)}, CalcReduce{Expr: "max(a)"}, ValueNumber, "inputs[1].key", "", 0},
		"health input":      {OwnerComponent, []TypedInput{in("h", HealthKey, ValueText)}, CalcReduce{Expr: `true`}, ValueBool, "inputs[0].key", "", 0},
		"unknown owner":     {"location", num, CalcReduce{Expr: "max(t)"}, ValueNumber, "owner_kind", "", 0},
		"unknown out type":  {OwnerComponent, num, CalcReduce{Expr: "max(t)"}, "matrix", "output_key", "", 0},
		"unknown input typ": {OwnerComponent, []TypedInput{in("t", "x", "matrix")}, CalcReduce{Expr: "max(t)"}, ValueNumber, "inputs[0].key", "", 0},
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
		if tc.phase != "" && (!errors.As(err, &ee) || ee.Phase != tc.phase || ee.Line == 0 || (tc.col > 0 && ee.Column != tc.col)) {
			t.Errorf("%s: %v (column %d), want a positioned %s error at column %d", name, err, func() int {
				if ee != nil {
					return ee.Column
				}
				return 0
			}(), tc.phase, tc.col)
		}
	}
	// A name no input binds says which, so the console can offer to bind it.
	_, err := CompileCalc(OwnerComponent, num, CalcReduce{Expr: "max(t) + max(fan)"}, ValueNumber)
	var ee *expression.Error
	if !errors.As(err, &ee) || ee.Unbound != "fan" || !strings.Contains(ee.Message, "fan") {
		t.Errorf("an unbound name: %v, want Unbound fan", err)
	}
}

// An alias is checked alone as the server, the form and the editor check it:
// an identifier of at most 64, not a keyword, builtin or list function, not
// one of the calc site's own bindings.
func TestCheckAlias(t *testing.T) {
	for _, ok := range []string{"tcp", "tcp_connect_time", "_x", "Temp2", strings.Repeat("a", 64)} {
		if err := CheckAlias(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "2x", "a-b", "a b", "true", "let", "len", "max", "avg", "majority", "count",
		"members", "component", "system", "value", "has", strings.Repeat("a", 65)} {
		if err := CheckAlias(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	reserved := CalcReservedNames()
	for _, n := range []string{"members", "component", "value", "has", "true", "len", "avg", "majority", "max"} {
		if !slices.Contains(reserved, n) {
			t.Errorf("the reserved names lack %q", n)
		}
	}
	if !slices.IsSorted(reserved) {
		t.Error("the reserved names are not sorted")
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

// The calc site is registered beside criteria, its bindings described rather
// than listed (they are each rule's inputs), with the list functions and a
// budget larger than criteria's and within the ceiling.
func TestCalcSite(t *testing.T) {
	s, ok := LookupSite("calc")
	if !ok {
		t.Fatal("the calc site is not registered")
	}
	if !s.KeyRequired || s.Bindings == "" {
		t.Fatalf("calc site %+v, want a key (the output) and a bindings description", s)
	}
	for _, vt := range []string{ValueNumber, ValueText, ValueBool} {
		v, ok := s.Variant(vt)
		if !ok || len(v.Env.Bindings()) != 0 || v.Env.Budget() != CalcBudget() || !slices.Equal(v.Env.Builtins(), CalcBuiltins()) {
			t.Errorf("variant %s: %+v, want one with no fixed bindings, the calc budget and builtins", vt, v)
		}
	}
	b, c, ceil := CalcBudget(), expression.CriteriaBudget(), expression.CeilingBudget()
	if b.Iterations <= c.Iterations || b.Iterations > ceil.Iterations || b.Memory > ceil.Memory || b.Deadline > ceil.Deadline {
		t.Errorf("calc budget %+v: want larger than criteria %+v and within the ceiling %+v", b, c, ceil)
	}
	f := s.Facts()
	if f.Bindings == "" || !slices.Equal(f.ReservedNames, CalcReservedNames()) || len(f.Functions) != len(expression.ListFunctions()) {
		t.Errorf("facts %+v, want the bindings description, the reserved names and the list functions", f)
	}
	for _, fn := range f.Functions {
		if fn.Name == "" || fn.Cost == "" {
			t.Errorf("list function %+v has no name or cost", fn)
		}
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
