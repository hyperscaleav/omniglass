package rules

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

// The calc rule's pure core (#995, epic #525): a calc rule reads named
// inputs (sample keys) on an owner, reduces them, and writes one output key.
// This file declares what a rule may say and checks it; the storage gateway
// keeps the rules, and the tick that runs them arrives with #525's second
// slice.

// The owners a calc rule derives for.
const (
	OwnerComponent = "component"
	OwnerSystem    = "system"
)

// HealthKey is the property the health rollup writes. A calc rule may not
// read it (#525 decision 3: an alarm moves it, so a rule reading it could
// feed back through an event rule) or write it (it has its own producer).
const HealthKey = "health"

// MaxCalcInputs is the most inputs one rule may read.
const MaxCalcInputs = 16

// CalcInput is one input as authored: a sample key, and the name the reduce
// knows it by.
type CalcInput struct {
	As  string `json:"as"`
	Key string `json:"key"`
}

// TypedInput is an input with its key's value type resolved from the
// registries (ValueTypeFor).
type TypedInput struct {
	As, Key, ValueType string
}

// CalcReduce is how a rule folds its inputs into its output: a named reducer
// or an expression, exactly one.
type CalcReduce struct {
	Reducer string `json:"reducer,omitempty"`
	Expr    string `json:"expr,omitempty"`
}

// Reducer is a named fold over every value of one input: on a component
// owner, every instance of the input key on the component; on a system
// owner, every instance on every role-bound member. It yields one value, so a
// rule's output stays aggregate (instance ”). Declared here and checked at
// authoring; the tick that folds arrives with #525's second slice.
type Reducer struct {
	Name string `json:"name"`
	// Accepts are the input value types it folds.
	Accepts []string `json:"accepts"`
	// Result is the value type it yields; empty is the input's own.
	Result string `json:"result,omitempty"`
	Doc    string `json:"doc"`
}

var reducers = []Reducer{
	{Name: "average", Accepts: []string{ValueNumber}, Result: ValueNumber, Doc: "The mean of the values folded in"},
	{Name: "min", Accepts: []string{ValueNumber}, Result: ValueNumber, Doc: "The smallest value folded in"},
	{Name: "max", Accepts: []string{ValueNumber}, Result: ValueNumber, Doc: "The largest value folded in"},
	{Name: "sum", Accepts: []string{ValueNumber}, Result: ValueNumber, Doc: "The total of the values folded in"},
	{Name: "count", Accepts: []string{ValueNumber, ValueText, ValueBool}, Result: ValueNumber, Doc: "How many values are present to fold, of any type"},
	{Name: "majority", Accepts: []string{ValueText, ValueBool}, Doc: "The most common value; a tie goes to the value that sorts first"},
	{Name: "any", Accepts: []string{ValueBool}, Result: ValueBool, Doc: "True when any value folded in is true"},
	{Name: "all", Accepts: []string{ValueBool}, Result: ValueBool, Doc: "True when every value folded in is true"},
}

// CalcReducers are the named reducers, a fresh copy each call.
func CalcReducers() []Reducer {
	out := make([]Reducer, len(reducers))
	for i, r := range reducers {
		r.Accepts = slices.Clone(r.Accepts)
		out[i] = r
	}
	return out
}

func lookupReducer(name string) (Reducer, bool) {
	for _, r := range reducers {
		if r.Name == name {
			return r, true
		}
	}
	return Reducer{}, false
}

func reducerNames() string {
	names := make([]string, len(reducers))
	for i, r := range reducers {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

// CalcBudget is the calc site's run budget: a reduce runs on the server's
// tick, off the ingest path, and a system rule's expression walks a list per
// input across every member, so it may walk ten times what a criteria may,
// still a tenth of the ceiling. A fresh value each call.
func CalcBudget() expression.Budget {
	return expression.Budget{Iterations: 100_000, Memory: 50_000, Deadline: 25 * time.Millisecond}
}

// calcOptions are the calc site's builtins (the criteria set: arithmetic is
// the language's operators, not a builtin, and no builtin is vetted for calc
// alone) and its budget.
func calcOptions() []expression.Option {
	return []expression.Option{expression.WithBuiltins(expression.CriteriaBuiltins()...), expression.WithBudget(CalcBudget())}
}

// CalcError is a rule refused at authoring, naming the field at fault
// (owner_kind, inputs, inputs[i].as, inputs[i].key, reduce, reduce.reducer,
// reduce.expr, output_key). It wraps an *expression.Error, with its line and
// column, when the reduce's expression is what failed.
type CalcError struct {
	Field string
	Err   error
}

func (e *CalcError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e *CalcError) Unwrap() error { return e.Err }

func calcErr(field, format string, a ...any) error {
	return &CalcError{Field: field, Err: fmt.Errorf(format, a...)}
}

// CalcProgram is a rule's reduce checked against its inputs and output: a
// named reducer, or a compiled expression and the environment it runs on.
type CalcProgram struct {
	Env     expression.Env
	Program *expression.Program
	Reducer *Reducer

	ownerKind string
	inputs    []TypedInput
	output    string
	envType   reflect.Type // the expression's input struct; nil for a reducer
}

// valueGoType is the Go type a value type binds as.
func valueGoType(valueType string) (reflect.Type, bool) {
	switch valueType {
	case ValueNumber:
		return reflect.TypeFor[float64](), true
	case ValueText:
		return reflect.TypeFor[string](), true
	case ValueBool:
		return reflect.TypeFor[bool](), true
	}
	return nil, false
}

func resultFor(valueType string) (expression.Result, bool) {
	switch valueType {
	case ValueNumber:
		return expression.Float, true
	case ValueText:
		return expression.String, true
	case ValueBool:
		return expression.Bool, true
	}
	return 0, false
}

// checkCalcInputs checks an owner kind and its inputs: one to MaxCalcInputs,
// each named by a bindable, unique name, each key read once, each of a known
// value type.
func checkCalcInputs(ownerKind string, inputs []TypedInput) error {
	if ownerKind != OwnerComponent && ownerKind != OwnerSystem {
		return calcErr("owner_kind", "a calc rule derives for a component or a system, not %q", ownerKind)
	}
	if len(inputs) == 0 || len(inputs) > MaxCalcInputs {
		return calcErr("inputs", "a calc rule reads 1 to %d inputs, not %d", MaxCalcInputs, len(inputs))
	}
	seenAs, seenKey := map[string]bool{}, map[string]bool{}
	for i, x := range inputs {
		if err := expression.BindableName(x.As); err != nil {
			return &CalcError{Field: fmt.Sprintf("inputs[%d].as", i), Err: err}
		}
		if seenAs[x.As] {
			return calcErr(fmt.Sprintf("inputs[%d].as", i), "%q names two inputs; each input's name is unique", x.As)
		}
		if x.Key == HealthKey {
			return calcErr(fmt.Sprintf("inputs[%d].key", i), "%s may not be an input: an alarm moves it, so a rule reading it could feed back through an event rule", HealthKey)
		}
		if seenKey[x.Key] {
			return calcErr(fmt.Sprintf("inputs[%d].key", i), "%q is read twice; read it once and use its name", x.Key)
		}
		if _, ok := valueGoType(x.ValueType); !ok {
			return calcErr(fmt.Sprintf("inputs[%d].key", i), "%q has no value type a calc rule reads (%q)", x.Key, x.ValueType)
		}
		seenAs[x.As], seenKey[x.Key] = true, true
	}
	return nil
}

// CalcEnv is the environment a rule's expression compiles against: each
// input bound by its name, as one value of its key's type for a component
// owner (the component's current value on its aggregate instance, ”) or as
// the list of those values for a system owner (one per role-bound member that
// has one), yielding the output key's type.
func CalcEnv(ownerKind string, inputs []TypedInput, output string) (expression.Env, error) {
	env, _, err := calcEnv(ownerKind, inputs, output)
	return env, err
}

func calcEnv(ownerKind string, inputs []TypedInput, output string) (expression.Env, reflect.Type, error) {
	if err := checkCalcInputs(ownerKind, inputs); err != nil {
		return expression.Env{}, nil, err
	}
	result, ok := resultFor(output)
	if !ok {
		return expression.Env{}, nil, calcErr("output_key", "the output has no value type a calc rule writes (%q)", output)
	}
	fields := make([]reflect.StructField, len(inputs))
	for i, x := range inputs {
		t, _ := valueGoType(x.ValueType)
		doc := x.Key + ": the component's current value"
		if ownerKind == OwnerSystem {
			t = reflect.SliceOf(t)
			doc = x.Key + ": each role-bound member's current value"
		}
		fields[i] = reflect.StructField{Name: fmt.Sprintf("In%d", i), Type: t,
			Tag: reflect.StructTag(fmt.Sprintf("expr:%q doc:%q", x.As, doc))}
	}
	typ := reflect.StructOf(fields)
	env, err := expression.EnvFor(typ, result, calcOptions()...)
	if err != nil {
		return expression.Env{}, nil, calcErr("inputs", "%v", err)
	}
	return env, typ, nil
}

// CompileCalc checks a rule's reduce against its owner kind, its inputs and
// its output key's value type. A named reducer reads exactly one input, of a
// type it folds, and yields the output's type; an expression compiles against
// CalcEnv. Every refusal is a *CalcError naming its field.
func CompileCalc(ownerKind string, inputs []TypedInput, reduce CalcReduce, output string) (*CalcProgram, error) {
	if err := checkCalcInputs(ownerKind, inputs); err != nil {
		return nil, err
	}
	if _, ok := resultFor(output); !ok {
		return nil, calcErr("output_key", "the output has no value type a calc rule writes (%q)", output)
	}
	switch {
	case reduce.Reducer != "" && reduce.Expr != "":
		return nil, calcErr("reduce", "a reduce is a named reducer or an expression, not both")
	case reduce.Reducer == "" && reduce.Expr == "":
		return nil, calcErr("reduce", "a reduce names a reducer (%s) or gives an expression", reducerNames())
	case reduce.Reducer != "":
		r, ok := lookupReducer(reduce.Reducer)
		if !ok {
			return nil, calcErr("reduce.reducer", "%q is not a reducer (%s)", reduce.Reducer, reducerNames())
		}
		if len(inputs) != 1 {
			return nil, calcErr("reduce.reducer", "%s folds one input, not %d; reduce several with an expression", r.Name, len(inputs))
		}
		x := inputs[0]
		if !slices.Contains(r.Accepts, x.ValueType) {
			return nil, calcErr("reduce.reducer", "%s folds %s values, and %s is %s", r.Name, strings.Join(r.Accepts, " or "), x.Key, x.ValueType)
		}
		yields := r.Result
		if yields == "" {
			yields = x.ValueType
		}
		if yields != output {
			return nil, calcErr("output_key", "%s yields %s, and the output is %s", r.Name, yields, output)
		}
		return &CalcProgram{Reducer: &r, ownerKind: ownerKind, inputs: slices.Clone(inputs), output: output}, nil
	}
	env, typ, err := calcEnv(ownerKind, inputs, output)
	if err != nil {
		return nil, err
	}
	p, err := expression.Compile(reduce.Expr, env)
	if err != nil {
		return nil, &CalcError{Field: "reduce.expr", Err: err}
	}
	return &CalcProgram{Env: env, Program: p, ownerKind: ownerKind, inputs: slices.Clone(inputs), output: output, envType: typ}, nil
}

// CalcEdge is one enabled rule in the key graph: its input keys lead to its
// output key.
type CalcEdge struct {
	Rule   string
	Inputs []string
	Output string
}

// Cycle is a loop in the key graph: Keys runs from the candidate's input,
// through its output and on, back to that input; Rules are the rules along
// it, in order, the candidate first.
type Cycle struct {
	Keys  []string
	Rules []string
}

func (c *Cycle) String() string { return strings.Join(c.Keys, " -> ") }

// ErrCalcCycle is a rule that would close a loop in the key graph.
var ErrCalcCycle = errors.New("the rule closes a cycle in the calc key graph")

// CalcCycle reports whether adding the candidate rule to the graph closes a
// cycle, the shortest such loop, or nil. The candidate replaces any stored
// edge of the same rule (an update). The graph is acyclic before the
// candidate (every write is checked), so any new loop runs through it: from
// its output, a path back to one of its inputs. Deterministic: neighbours are
// visited in key order.
func CalcCycle(graph []CalcEdge, candidate CalcEdge) *Cycle {
	type step struct{ to, rule string }
	next := map[string][]step{}
	add := func(e CalcEdge) {
		for _, k := range e.Inputs {
			next[k] = append(next[k], step{to: e.Output, rule: e.Rule})
		}
	}
	for _, e := range graph {
		if e.Rule != candidate.Rule {
			add(e)
		}
	}
	add(candidate)
	for k := range next {
		slices.SortFunc(next[k], func(a, b step) int {
			return strings.Compare(a.to+"\x00"+a.rule, b.to+"\x00"+b.rule)
		})
	}
	inputs := map[string]bool{}
	for _, k := range candidate.Inputs {
		inputs[k] = true
	}
	// Breadth first from the candidate's output, so the loop found is the
	// shortest.
	type visit struct {
		key  string
		prev *visit
		rule string
	}
	start := &visit{key: candidate.Output, rule: candidate.Rule}
	queue := []*visit{start}
	seen := map[string]bool{candidate.Output: true}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		if inputs[v.key] {
			var keys, rules []string
			for p := v; p != nil; p = p.prev {
				keys = append(keys, p.key)
				rules = append(rules, p.rule)
			}
			slices.Reverse(keys)
			slices.Reverse(rules)
			return &Cycle{Keys: append([]string{v.key}, keys...), Rules: rules}
		}
		for _, s := range next[v.key] {
			if !seen[s.to] {
				seen[s.to] = true
				queue = append(queue, &visit{key: s.to, prev: v, rule: s.rule})
			}
		}
	}
	return nil
}
