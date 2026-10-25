package expression

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// listEnv is a site over lists of each value type, the shape a calc rule's
// inputs bind as (#1018).
type listEnv struct {
	Nums  []float64 `expr:"nums" doc:"numbers"`
	Ints  []int     `expr:"ints"`
	Words []string  `expr:"words"`
	Flags []bool    `expr:"flags"`
	One   float64   `expr:"one"`
	Pts   []listPt  `expr:"pts"`
	// Records with a value field: a windowed calc input's samples (#1142).
	Samples []listSample `expr:"samples"`
	Tags    []listTag    `expr:"tags"`
	Ups     []listUp     `expr:"ups"`
}

type listSample struct {
	Value float64 `expr:"value"`
	At    float64 `expr:"at"`
}

type listTag struct {
	Value string `expr:"value"`
}

type listUp struct {
	Value bool `expr:"value"`
}

type listPt struct {
	V   float64 `expr:"v"`
	Has bool    `expr:"has"`
}

func listSite(t *testing.T, r Result, lists bool) Env {
	t.Helper()
	b := CriteriaBudget()
	b.Deadline = CeilingBudget().Deadline
	opts := []Option{WithBuiltins(CriteriaBuiltins()...), WithBudget(b)}
	if lists {
		opts = append(opts, WithListFunctions(ListFunctions()...))
	}
	env, err := EnvOf[listEnv](r, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

var listIn = listEnv{
	Nums:    []float64{2, 3, 1},
	Ints:    []int{4, 9, 2},
	Words:   []string{"b", "a", "b", "a", "c"},
	Flags:   []bool{true, false, false},
	One:     7,
	Pts:     []listPt{{V: 1, Has: true}, {V: 5}, {V: 3, Has: true}},
	Samples: []listSample{{Value: 4, At: 1}, {Value: 8, At: 2}, {Value: 3, At: 3}},
	Tags:    []listTag{{Value: "on"}, {Value: "off"}, {Value: "on"}},
	Ups:     []listUp{{Value: true}, {Value: false}},
}

// The list functions (#1018): each folds one list into one value, and the
// predicate forms Expr already had keep working beside them.
func TestListFunctions(t *testing.T) {
	for _, tc := range []struct {
		src  string
		r    Result
		want any
	}{
		{"max(nums)", Float, 3.0},
		{"min(nums)", Float, 1.0},
		{"avg(nums)", Float, 2.0},
		{"sum(nums)", Float, 6.0},
		{"len(nums)", Float, 3.0},
		{"max(ints)", Int, 9},
		{"min(ints) + 0.5", Float, 2.5},
		{"avg(ints)", Float, 5.0},
		{"max(one, 2)", Float, 7.0},
		{"min(one, 2, 3.5)", Float, 2.0},
		{"max(filter(nums, # > 1))", Float, 3.0},
		{"avg(filter(nums, # < 3))", Float, 1.5},
		{"sum(pts, .v)", Float, 9.0},
		{"majority(words)", String, "a"},
		{"majority(flags)", Bool, false},
		{"majority([true, true, false]) && true", Bool, true},
		{"any(flags)", Bool, true},
		{"all(flags)", Bool, false},
		{"all(filter(flags, #))", Bool, true},
		{"any(nums, # > 2)", Bool, true},
		{"all(pts, .v > 0)", Bool, true},
		{"count(flags)", Int, 1},
		{"count(nums, # > 1)", Int, 2},
		{"count(pts, .has && .v > 2)", Int, 1},
		{"any(flags) && all(pts, .v > 0)", Bool, true},
		{"any(\n  flags\n)", Bool, true},
		// A list of records with a value field folds by its value (#1142).
		{"max(samples)", Float, 8.0},
		{"min(samples)", Float, 3.0},
		{"avg(samples)", Float, 5.0},
		{"sum(samples)", Float, 15.0},
		{"majority(tags)", String, "on"},
		{"any(ups)", Bool, true},
		{"all(ups)", Bool, false},
		{"sum(samples, .at)", Float, 6.0},
		{"count(samples, .value > 3)", Int, 2},
	} {
		p, err := Compile(tc.src, listSite(t, tc.r, true))
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		got, err := p.Run(listIn)
		if err != nil || got != tc.want {
			t.Errorf("%q = %v (%T), %v; want %v (%T)", tc.src, got, got, err, tc.want, tc.want)
		}
	}
}

// A list function given something other than its list is refused at
// compile time, placed on the argument at fault.
func TestListFunctionsRefuseMistyped(t *testing.T) {
	for _, tc := range []struct {
		src       string
		r         Result
		line, col int
		says      string
	}{
		{"max(words)", Float, 1, 5, "number"},
		{"one + avg(flags)", Float, 1, 11, "number"},
		{"avg(one)", Float, 1, 5, "list"},
		{"sum(words) > 0", Bool, 1, 5, "number"},
		{"majority(nums) == 1", Bool, 1, 10, "text or bools"},
		{"true &&\n  any(nums)", Bool, 2, 7, "bools"},
		{"all(words)", Bool, 1, 5, "bools"},
		{"avg(nums, nums)", Float, 1, 1, "one list"},
		{"max(nums, 2)", Float, 1, 5, "number"},
		{"majority()", String, 1, 1, "one list"},
		// A record without a value field does not fold; a value of the
		// wrong type is refused as its list would be.
		{"avg(pts)", Float, 1, 5, "list of records"},
		{"avg(tags)", Float, 1, 5, "number"},
		{"any(samples)", Bool, 1, 5, "bools"},
		// A sum of bools is explained as the counting it meant (#1021).
		{"sum(pts, .has)", Bool, 1, 1, "count(...)"},
	} {
		_, err := Compile(tc.src, listSite(t, tc.r, true))
		e := asError(t, err)
		if e.Phase != PhaseType || e.Line != tc.line || e.Column != tc.col || !strings.Contains(e.Message, tc.says) {
			t.Errorf("%q: got %s %d:%d %q, want a type error at %d:%d saying %q", tc.src, e.Phase, e.Line, e.Column, e.Message, tc.line, tc.col, tc.says)
		}
		if strings.Contains(e.Message, "$") {
			t.Errorf("%q: message %q leaks the engine's plumbing", tc.src, e.Message)
		}
	}
}

// A site that does not admit the list functions keeps Expr's builtins as they
// were: max and min take scalars, any and all take a predicate, and avg,
// majority and sum are not available.
func TestListFunctionsOnlyWhereAdmitted(t *testing.T) {
	for _, src := range []string{"max(nums) > 0", "avg(nums) > 0", "majority(flags)", "sum(nums) > 0", "any(flags)"} {
		if _, err := Compile(src, listSite(t, Bool, false)); err == nil {
			t.Errorf("%q compiled at a site without the list functions", src)
		}
	}
	// A user cannot reach the one-list forms by their engine names.
	for _, src := range []string{"$an(flags)", "$al(flags)", "$su(nums) > 0"} {
		if _, err := Compile(src, listSite(t, Bool, true)); err == nil {
			t.Errorf("%q compiled", src)
		}
	}
}

// Each list function walks its list once, through a charge: a list past the
// walk budget is refused, and majority is charged its strings' bytes, since
// it hashes and compares them.
func TestListFunctionsCharged(t *testing.T) {
	long := make([]float64, CriteriaBudget().Iterations+1)
	for _, src := range []string{"max(nums)", "avg(nums)", "sum(nums)"} {
		p, err := Compile(src, listSite(t, Float, true))
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Run(listEnv{Nums: long})
		budgetErr(t, src, err)
	}
	p, err := Compile("majority(words)", listSite(t, String, true))
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", CriteriaBudget().Iterations/2)
	_, err = p.Run(listEnv{Words: []string{big, big + "y", big + "z"}})
	budgetErr(t, "majority over long words", err)
}

// A list function over an empty list: avg, max and min have no value (a run
// error the caller records), sum is zero, any is false and all is true.
func TestListFunctionsEmpty(t *testing.T) {
	for _, src := range []string{"avg(filter(nums, # > 10))", "max(filter(nums, # > 10))"} {
		p, err := Compile(src, listSite(t, Float, true))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Run(listIn); err == nil {
			t.Errorf("%q over an empty list ran", src)
		}
	}
	for _, tc := range []struct {
		src  string
		r    Result
		want any
	}{
		{"sum(filter(nums, # > 10))", Float, 0.0},
		{"all(filter(flags, # && !#))", Bool, true},
		{"any(filter(flags, # && !#))", Bool, false},
	} {
		p, err := Compile(tc.src, listSite(t, tc.r, true))
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		got, err := p.Run(listIn)
		if err != nil || got != tc.want {
			t.Errorf("%q = %v (%T), %v; want %v", tc.src, got, got, err, tc.want)
		}
	}
}

// A let may not shadow a binding: the name would mean the let's value in one
// place and the binding in another. Refused where the let is.
func TestLetShadowsBinding(t *testing.T) {
	e := asError(t, func() error {
		_, err := Compile("one > 0 &&\n  (let nums = 2; nums > 1)", listSite(t, Bool, true))
		return err
	}())
	if e.Phase != PhaseType || e.Line != 2 || e.Column != 8 || !strings.Contains(e.Message, "nums") {
		t.Errorf("got %s %d:%d %q, want a type error at 2:8 (the name) naming nums", e.Phase, e.Line, e.Column, e.Message)
	}
	if _, err := Compile("let x = max(nums); x > 1", listSite(t, Bool, true)); err != nil {
		t.Errorf("a let of a fresh name: %v", err)
	}
}

// A name nothing binds is a positioned type error that says which name, so
// an editor can offer to bind it.
func TestUnboundNameNamed(t *testing.T) {
	e := asError(t, func() error { _, err := Compile("max(nums) > limit", listSite(t, Bool, true)); return err }())
	if e.Phase != PhaseType || e.Line != 1 || e.Column != 13 || e.Unbound != "limit" {
		t.Errorf("got %s %d:%d unbound %q, want a type error at 1:13 naming limit", e.Phase, e.Line, e.Column, e.Unbound)
	}
	// Inside a list function's argument too: Expr would trust the call's
	// type and never look at the argument.
	for _, src := range []string{"max(limit) > 1", "avg(nums) + max(fan) > 1", "majority(words2) == 'a'", "any(flags2)", "sum(n2) > 1"} {
		e = asError(t, func() error { _, err := Compile(src, listSite(t, Bool, true)); return err }())
		if e.Unbound == "" || e.Phase != PhaseType {
			t.Errorf("%q: got %v, want an unbound name", src, e)
		}
	}
	e = asError(t, checkErr("value > 1", listSite(t, Bool, true)))
	if e.Unbound != "value" {
		t.Errorf("Check: unbound %q, want value", e.Unbound)
	}
	e = asError(t, func() error { _, err := Compile("max(words)", listSite(t, Float, true)); return err }())
	if e.Unbound != "" {
		t.Errorf("a mistyped argument names %q unbound", e.Unbound)
	}
}

// The list functions are vetted like builtins: a site admits only the ones
// the registry carries, each with its cost argument, and a site's builtins
// list names them beside Expr's.
func TestListFunctionsVetted(t *testing.T) {
	if _, err := EnvOf[listEnv](Float, WithListFunctions("median")); err == nil {
		t.Error("an unvetted list function was admitted")
	}
	for _, n := range ListFunctions() {
		if strings.TrimSpace(ListFunctionCost(n)) == "" {
			t.Errorf("%s carries no cost argument", n)
		}
	}
	got := strings.Join(listSite(t, Float, true).Builtins(), ",")
	for _, want := range []string{"avg", "majority", "sum", "max", "any"} {
		if !strings.Contains(","+got+",", ","+want+",") {
			t.Errorf("builtins %s do not name %s", got, want)
		}
	}
	if strings.Contains(got, "$") {
		t.Errorf("builtins %s name the engine's plumbing", got)
	}
	// The list functions' names are never bindable, admitted or not.
	for _, n := range []string{"avg", "majority"} {
		if err := BindableName(n); err == nil {
			t.Errorf("%q was bindable", n)
		}
	}
}

// The one-list any and all are told apart from the predicate forms by their
// argument count, lexically, so strings and nested calls that hold a comma
// do not confuse them.
func TestOneListFormsLexical(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`any(filter(pts, .has && .v > 2), .v > 4)`, false},
		{`all(filter(flags, # == ("a,b" == "a,b")))`, true},
		{`any([true, false])`, true},
		{`all(pts, .v in [1, 3, 5])`, true},
		{`all(pts, .v in [1, 3])`, false},
	} {
		p, err := Compile(tc.src, listSite(t, Bool, true))
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		got, err := p.Run(listIn)
		if err != nil || got != tc.want {
			t.Errorf("%q = %v, %v; want %v", tc.src, got, err, tc.want)
		}
	}
}

func TestListFunctionRunErrorIsRun(t *testing.T) {
	p, err := Compile("avg(filter(nums, # > 10))", listSite(t, Float, true))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Run(listIn)
	var e *Error
	if !errors.As(err, &e) || e.Phase != PhaseRun || !strings.Contains(e.Message, "empty") {
		t.Errorf("got %v, want a run error saying the list is empty", err)
	}
}

// An expression that reads each list only whole, folded into one value, is
// told apart from one that reads an element (by position, through a
// predicate, or bound by a let), so a preview may show the first over a list
// with elements its reader may not see.
func TestWholeListsOnly(t *testing.T) {
	for src, want := range map[string]bool{
		"avg(nums) + max(nums) - min(nums) + sum(nums) + len(nums) > 0": true,
		"any(flags) || all(flags) || count(flags) > 1":                  true,
		"majority(words) == 'a'":                                        true,
		"one > 1":                                                       true,
		"nums[0] > 1":                                                   false,
		"any(nums, # > 1)":                                              false,
		"let n = nums; max(n) > 1":                                      false,
		"max(nums[0], 2) > 1":                                           false,
		"len(pts) > 0":                                                  true,
		"pts[0].v > 0":                                                  false,
		"count(pts, .has) > 0":                                          false,
	} {
		p, err := Compile(src, listSite(t, Bool, true))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got := p.WholeListsOnly("nums", "flags", "words", "pts"); got != want {
			t.Errorf("%q: WholeListsOnly = %v, want %v", src, got, want)
		}
	}
}

// Integer totals stay exact (#1018's review): a sum or an extreme over ints
// is folded as an int, so a value past 2^53 is not rounded through a float,
// and an int sum wraps as Expr's int arithmetic does.
func TestListFunctionsKeepIntegersExact(t *testing.T) {
	big := 9007199254740993 // 2^53 + 1, not a float64
	in := listEnv{Ints: []int{big, 0}}
	for src, want := range map[string]int{"sum(ints)": big, "max(ints)": big, "min(ints)": 0, "max(ints[0], 1)": big} {
		p, err := Compile(src, listSite(t, Int, true))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got, err := p.Run(in); err != nil || got != want {
			t.Errorf("%q = %v, %v; want %d", src, got, err, want)
		}
	}
	p, err := Compile("sum(ints)", listSite(t, Int, true))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Run(listEnv{Ints: []int{math.MaxInt, 1}}); err != nil || got != math.MinInt {
		t.Errorf("sum over MaxInt+1 = %v, %v; want it to wrap to MinInt", got, err)
	}
}

// The refusals read cleanly and name what the site admits, list functions
// included.
func TestListFunctionRefusalsRead(t *testing.T) {
	e := asError(t, func() error { _, err := Compile("any(nums)", listSite(t, Bool, true)); return err }())
	if strings.Contains(e.Message, ",,") {
		t.Errorf("message %q has a doubled comma", e.Message)
	}
	e = asError(t, func() error { _, err := Compile("mean(nums) > 1", listSite(t, Bool, true)); return err }())
	for _, want := range []string{"avg", "majority", "max"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("message %q does not list %s", e.Message, want)
		}
	}
}
