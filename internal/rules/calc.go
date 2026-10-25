package rules

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
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

// HealthKey is the property the health rollup writes, a platform key
// (writerclass.Health, ADR-0161). A calc rule may not read it (#525 decision
// 3: an alarm moves it, so a rule reading it could feed back through an event
// rule) or write it (its writer class admits only the process).
const HealthKey = writerclass.Health

// MaxCalcInputs is the most inputs one rule may read.
const MaxCalcInputs = 16

// CalcInput is one input as authored and stored (#1018): the alias the
// expression knows it by, and the key it reads, held by id in its lane's
// registry (KeyID, Lane) so a key rename changes nothing; Key is the key's
// name, resolved when the rule is read (on a write, Key may name the key or
// give its id).
//
// Window, in seconds, reads the key over a time window instead of its current
// values (#1142): the list of its samples in the last Window seconds, oldest
// first, each a record (CalcSample). Zero is the current values.
type CalcInput struct {
	Alias  string `json:"alias"`
	Key    string `json:"key"`
	KeyID  string `json:"key_id,omitempty"`
	Lane   Lane   `json:"lane,omitempty"`
	Window int    `json:"window_seconds,omitempty"`
}

// TypedInput is an input with its key's value type resolved from the
// registries (ValueTypeFor).
type TypedInput struct {
	Alias, Key, KeyID string
	Lane              Lane
	ValueType         string
	Window            int
}

// MaxCalcWindow is the longest window an input may read over: the staleness
// bound on a current value (an hour), so a window never reaches past what a
// current-value input would still trust.
const MaxCalcWindow = time.Hour

// MaxCalcWindowSamples bounds one window's read: the newest this many samples
// in the window are kept, so a key sampled faster than its window expects
// cannot make one computation unbounded.
const MaxCalcWindowSamples = 1000

// CalcReduce is how a rule folds its inputs into its output: always an
// expression over the inputs by alias (#1018 retired the named reducers;
// the list functions avg, max, min, sum, majority, any and all are what they
// were).
type CalcReduce struct {
	Expr string `json:"expr"`
}

// CalcBudget is the calc site's run budget: a reduce runs on the server's
// tick, off the ingest path, and a system rule's expression walks a list per
// input across every member, so it may walk ten times what a criteria may,
// still a tenth of the ceiling. A fresh value each call.
func CalcBudget() expression.Budget {
	return expression.Budget{Iterations: 100_000, Memory: 50_000, Deadline: 25 * time.Millisecond}
}

// calcOptions are the calc site's functions (the criteria builtins and every
// list function, since an input binds as a list), its budget, and its name,
// so the diagnostic rules for calc run on its expressions.
func calcOptions() []expression.Option {
	return []expression.Option{expression.WithBuiltins(expression.CriteriaBuiltins()...),
		expression.WithListFunctions(expression.ListFunctions()...), expression.WithBudget(CalcBudget()), expression.WithSite("calc")}
}

// CalcBuiltins are the functions the calc site admits, sorted, a fresh slice
// each call.
func CalcBuiltins() []string { return calcVariant(ValueNumber, "").Env.Builtins() }

// calcSiteBindings are the names the calc site binds itself, or keeps for
// itself, beside a rule's aliases: members (a system rule's records), the
// record's value and has, and the owners' own names (component, system), kept
// so a later binding of them never changes what a stored rule means.
var calcSiteBindings = []string{"component", "has", "members", "system", "value"}

// CalcReservedNames is every name an alias may not take: the expression
// language's keywords, builtin and list function names, and the calc site's
// own bindings. Sorted, a fresh slice each call; generated for the console
// (make gen) so the form and editor refuse what the server refuses.
func CalcReservedNames() []string {
	out := slices.Concat(expression.ReservedNames(), calcSiteBindings)
	slices.Sort(out)
	return slices.Compact(out)
}

// CheckAlias reports whether an alias may name a calc rule's input: a name
// the expression can say that means only the input (expression.BindableName:
// an identifier of at most 64, not a keyword, builtin or list function), and
// not one of the site's own bindings.
func CheckAlias(alias string) error {
	if err := expression.BindableName(alias); err != nil {
		return err
	}
	if slices.Contains(calcSiteBindings, alias) {
		return fmt.Errorf("%q is a name the calc site binds itself", alias)
	}
	return nil
}

// CalcError is a rule refused at authoring, naming the field at fault
// (owner_kind, inputs, inputs[i].alias, inputs[i].key, reduce.expr,
// output_key). It wraps an *expression.Error, with its line and
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

// CalcProgram is a rule's reduce compiled against its inputs and output, and
// the environment it runs on.
type CalcProgram struct {
	Env     expression.Env
	Program *expression.Program

	ownerKind string
	inputs    []TypedInput
	output    string
	envType   reflect.Type // the expression's input struct
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
// each named by a unique alias CheckAlias accepts, each key read once, each
// of a known value type.
func checkCalcInputs(ownerKind string, inputs []TypedInput) error {
	if ownerKind != OwnerComponent && ownerKind != OwnerSystem {
		return calcErr("owner_kind", "a calc rule derives for a component or a system, not %q", ownerKind)
	}
	if len(inputs) == 0 || len(inputs) > MaxCalcInputs {
		return calcErr("inputs", "a calc rule reads 1 to %d inputs, not %d", MaxCalcInputs, len(inputs))
	}
	seenAlias, seenKey := map[string]bool{}, map[string]bool{}
	for i, x := range inputs {
		if err := CheckAlias(x.Alias); err != nil {
			return &CalcError{Field: fmt.Sprintf("inputs[%d].alias", i), Err: err}
		}
		if seenAlias[x.Alias] {
			return calcErr(fmt.Sprintf("inputs[%d].alias", i), "%q names two inputs; each input's alias is unique", x.Alias)
		}
		if x.Key == HealthKey {
			return calcErr(fmt.Sprintf("inputs[%d].key", i), "%s may not be an input: an alarm moves it, so a rule reading it could feed back through an event rule", HealthKey)
		}
		key := x.Key
		if x.KeyID != "" {
			key = string(x.Lane) + ":" + x.KeyID
		}
		if seenKey[key] {
			return calcErr(fmt.Sprintf("inputs[%d].key", i), "%q is read twice; read it once and use its alias", x.Key)
		}
		if _, ok := valueGoType(x.ValueType); !ok {
			return calcErr(fmt.Sprintf("inputs[%d].key", i), "%q has no value type a calc rule reads (%q)", x.Key, x.ValueType)
		}
		if err := checkWindow(ownerKind, x.Window); err != nil {
			return &CalcError{Field: fmt.Sprintf("inputs[%d].window_seconds", i), Err: err}
		}
		seenAlias[x.Alias], seenKey[key] = true, true
	}
	return nil
}

// checkWindow refuses a window outside 1s to MaxCalcWindow, and any window on
// a system rule (a system's windows are #1143's).
func checkWindow(ownerKind string, seconds int) error {
	switch {
	case seconds == 0:
		return nil
	case ownerKind == OwnerSystem:
		return errors.New("a system rule's inputs read current values; a window is a component rule's for now (#1143)")
	case seconds < 0 || time.Duration(seconds)*time.Second > MaxCalcWindow:
		return fmt.Errorf("a window is 1 to %d seconds, not %d", int(MaxCalcWindow/time.Second), seconds)
	}
	return nil
}

// sampleType is the record a windowed input's list holds, of a value type:
// the sample's value, when it was taken (at, unix seconds, since an
// expression environment holds no time.Time) and its instance. A list of
// these folds by value in the list functions.
func sampleType(valueType string) reflect.Type {
	t, _ := valueGoType(valueType)
	return reflect.StructOf([]reflect.StructField{
		{Name: "Value", Type: t, Tag: `expr:"value" doc:"The sample's value"`},
		{Name: "At", Type: reflect.TypeFor[float64](), Tag: `expr:"at" doc:"When the sample was taken, in unix seconds"`},
		{Name: "Instance", Type: reflect.TypeFor[string](), Tag: `expr:"instance" doc:"The sample's instance; empty for the aggregate instance"`},
	})
}

// CalcEnv is the environment a rule's expression compiles against: each
// input bound by its alias as the list of its current values, of its key's
// type (#1018), or, for a windowed input, the list of its samples in the
// window, one record each (#1142): on a component owner one per instance (the named instances,
// the aggregate instance ” only when it is the key's one, a stale one left
// out), on a system owner every role-bound member's, in member order. A
// system owner also binds members, one record per member. It yields the
// output key's type.
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
	lists := make([]reflect.StructField, len(inputs))
	for i, x := range inputs {
		t, _ := valueGoType(x.ValueType)
		doc := x.Key + ": the component's current values, one per instance"
		if ownerKind == OwnerSystem {
			doc = x.Key + ": every member's current values, one per instance, by member name"
		}
		if x.Window > 0 {
			t = sampleType(x.ValueType)
			doc = fmt.Sprintf("%s: the component's samples over the last %ds, oldest first, one record each", x.Key, x.Window)
		}
		lists[i] = reflect.StructField{Name: fmt.Sprintf("In%d", i), Type: reflect.SliceOf(t),
			Tag: reflect.StructTag(fmt.Sprintf("expr:%q doc:%q", x.Alias, doc))}
	}
	typ := reflect.StructOf(lists)
	if ownerKind == OwnerSystem {
		typ = systemEnvType(inputs, lists)
	}
	aliases := make([]string, len(inputs))
	for i, x := range inputs {
		aliases[i] = x.Alias
	}
	env, err := expression.EnvFor(typ, result, append(calcOptions(), expression.WithInputs(aliases...))...)
	if err != nil {
		return expression.Env{}, nil, calcErr("inputs", "%v", err)
	}
	return env, typ, nil
}

// systemEnvType is a system expression's environment: each input's list
// (lists), then members, one record per role-bound member in a stable order
// (name, then id), each with the member's id and name, its own value of each
// input under value (its aggregate instance's), and whether it holds one
// under has. One record per member keeps a member's inputs together, which
// the per-input lists cannot once a member lacks one of them.
func systemEnvType(inputs []TypedInput, lists []reflect.StructField) reflect.Type {
	has := make([]reflect.StructField, len(inputs))
	values := make([]reflect.StructField, len(inputs))
	for i, x := range inputs {
		t, _ := valueGoType(x.ValueType)
		values[i] = reflect.StructField{Name: fmt.Sprintf("In%d", i), Type: t,
			Tag: reflect.StructTag(fmt.Sprintf("expr:%q doc:%q", x.Alias, x.Key+": the member's own current value (its aggregate instance; the type's zero when it has none, see has)"))}
		has[i] = reflect.StructField{Name: fmt.Sprintf("In%d", i), Type: reflect.TypeFor[bool](),
			Tag: reflect.StructTag(fmt.Sprintf("expr:%q doc:%q", x.Alias, "whether the member holds its own value of "+x.Key))}
	}
	member := reflect.StructOf([]reflect.StructField{
		{Name: "ID", Type: reflect.TypeFor[string](), Tag: `expr:"id" doc:"The member component's uuid"`},
		{Name: "Name", Type: reflect.TypeFor[string](), Tag: `expr:"name" doc:"The member component's name"`},
		{Name: "Value", Type: reflect.StructOf(values), Tag: `expr:"value" doc:"The member's own value of each input, by its alias"`},
		{Name: "Has", Type: reflect.StructOf(has), Tag: `expr:"has" doc:"Whether the member holds its own value of each input, by its alias"`},
	})
	return reflect.StructOf(append(slices.Clone(lists), reflect.StructField{Name: "Members", Type: reflect.SliceOf(member),
		Tag: `expr:"members" doc:"The system's role-bound members, one record each, by name then id"`}))
}

// CompileCalc checks a rule's reduce against its owner kind, its inputs and
// its output key's value type: the expression compiles against CalcEnv.
// Every refusal is a *CalcError naming its field; an expression's carries the
// positioned *expression.Error (with Unbound set for a name no alias binds).
func CompileCalc(ownerKind string, inputs []TypedInput, reduce CalcReduce, output string) (*CalcProgram, error) {
	env, typ, err := calcEnv(ownerKind, inputs, output)
	if err != nil {
		return nil, err
	}
	if reduce.Expr == "" {
		return nil, calcErr("reduce.expr", "a reduce is an expression over the inputs by alias, e.g. max(%s)", inputs[0].Alias)
	}
	p, err := expression.Compile(reduce.Expr, env)
	if err != nil {
		return nil, &CalcError{Field: "reduce.expr", Err: calcDiagnostic(err)}
	}
	return &CalcProgram{Env: env, Program: p, ownerKind: ownerKind, inputs: slices.Clone(inputs), output: output, envType: typ}, nil
}

// Warnings are the warnings the reduce compiled with: notes on a rule that
// computes all the same.
func (c *CalcProgram) Warnings() []FieldDiagnostic { return warningsOf("reduce.expr", c.Program) }

// Aggregate reports whether the reduce reads every input, and a system's
// members, only as a whole list folded into one value (avg(t), max(t),
// len(members)), never one element by position or through a predicate: its
// result is an aggregate over the owner's values, as a named reducer's was,
// rather than one member's own.
func (c *CalcProgram) Aggregate() bool {
	names := []string{"members"}
	for _, in := range c.inputs {
		names = append(names, in.Alias)
	}
	return c.Program.WholeListsOnly(names...)
}

// CheckCalc is CompileCalc's check of a source against a built env without
// the program cache (the validate route), its refusal said the same way,
// with the warnings and hints of one that compiles.
func CheckCalc(source string, env expression.Env) ([]expression.Diagnostic, error) {
	diags, err := expression.Check(source, env)
	return diags, calcDiagnostic(err)
}

// calcDiagnostic says an unbound name in a calc rule's terms: a name no
// input's alias binds, which the author binds by giving a key that alias.
func calcDiagnostic(err error) error {
	var ee *expression.Error
	if errors.As(err, &ee) && ee.Unbound != "" {
		ee.Message = fmt.Sprintf("%s is not an input of this rule: no input has the alias %s; bind it to a key", ee.Unbound, ee.Unbound)
	}
	return err
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
