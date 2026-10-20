package expression

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// sampleEnv is a site's declared environment: the bindings an expression may
// name, with their types and docs read from the struct.
type sampleEnv struct {
	Value  float64        `expr:"value" doc:"the arriving sample's number"`
	State  string         `expr:"state" doc:"the arriving sample's text"`
	Prior  float64        `expr:"prior" doc:"the previous sample's number"`
	Items  []int          `expr:"items" doc:"a list of numbers"`
	Labels []string       `expr:"labels"`
	Tags   map[string]int `expr:"tags" doc:"a map of counts"`
	N      int            `expr:"n"`
	Nested [][]int        `expr:"nested"`
	Point  point          `expr:"point"`
	Flags  []bool         `expr:"flags"`
}

type point struct {
	X int `expr:"x" doc:"the point's x"`
}

func boolEnv(t *testing.T) Env {
	t.Helper()
	return mustEnv(t, Bool)
}

// asError asserts err is a diagnostic and returns it.
func asError(t *testing.T, err error) *Error {
	t.Helper()
	if err == nil {
		t.Fatal("got no error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v (%T) is not an *expression.Error", err, err)
	}
	return e
}

// A compiled criterion runs against a prepared input of the site's type.
func TestCompileAndRun(t *testing.T) {
	p, err := Compile("value > 80 && state == 'down'", boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		in   sampleEnv
		want bool
	}{
		{sampleEnv{Value: 81, State: "down"}, true},
		{sampleEnv{Value: 81, State: "up"}, false},
		{sampleEnv{Value: 80, State: "down"}, false},
	} {
		got, err := p.Run(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Run(%+v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	// A pointer to the env type is the same input.
	if got, err := p.Run(&sampleEnv{Value: 99, State: "down"}); err != nil || got != true {
		t.Errorf("Run(pointer) = %v, %v", got, err)
	}
}

// An input of any other type is refused, not coerced.
func TestRunRefusesForeignInput(t *testing.T) {
	p, err := Compile("value > 1", boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []any{nil, map[string]any{"value": 2.0}, struct{ Value float64 }{2}} {
		if _, err := p.Run(in); err == nil {
			t.Errorf("Run(%T) accepted a foreign input", in)
		}
	}
}

// A name outside the declared environment is a type error, with its position.
func TestUnknownBinding(t *testing.T) {
	e := asError(t, func() error { _, err := Compile("value > 1 && humidity > 2", boolEnv(t)); return err }())
	if e.Phase != PhaseType {
		t.Errorf("phase = %q, want %q", e.Phase, PhaseType)
	}
	if !strings.Contains(e.Message, "humidity") {
		t.Errorf("message %q does not name the unknown binding", e.Message)
	}
	if e.Line != 1 || e.Column != 14 {
		t.Errorf("position = %d:%d, want 1:14", e.Line, e.Column)
	}
}

// The site pins the result type; an expression of another type is refused at
// compile time, never discovered at run time.
func TestWrongResultType(t *testing.T) {
	for _, src := range []string{"value + 1", "state", "items"} {
		e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
		if e.Phase != PhaseType {
			t.Errorf("%q: phase = %q, want %q", src, e.Phase, PhaseType)
		}
		if !strings.Contains(e.Message, "bool") {
			t.Errorf("%q: message %q does not name the expected type", src, e.Message)
		}
		if e.Line != 1 || e.Column != 1 {
			t.Errorf("%q: position = %d:%d, want 1:1", src, e.Line, e.Column)
		}
	}
}

// Each pinned result kind accepts its own type.
func TestResultKinds(t *testing.T) {
	for _, tc := range []struct {
		result Result
		src    string
		want   any
	}{
		{Float, "value / 2", 21.0},
		{Float, "len(items)", 3.0},
		{Int, "len(items)", 3},
		{String, "upper(state)", "DOWN"},
		{Any, "items", []int{1, 2, 3}},
	} {
		p, err := Compile(tc.src, mustEnv(t, tc.result))
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		got, err := p.Run(sampleEnv{Value: 42, State: "down", Items: []int{1, 2, 3}})
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(tc.want) || fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tc.want) {
			t.Errorf("%q = %v (%T), want %v (%T)", tc.src, got, got, tc.want, tc.want)
		}
	}
	if _, err := Compile("state", mustEnv(t, Int)); err == nil {
		t.Error("a string expression compiled against an int site")
	}
}

// mustEnv is the sample site with the criteria builtins and the criteria
// walk and memory budgets. Its deadline is the ceiling's, not the criteria's
// 5ms: the deadline is a machine-dependent backstop, and a test asserting
// honest work passes must not hang on how loaded the machine (or -race) is.
func mustEnv(t *testing.T, r Result, opts ...Option) Env {
	t.Helper()
	b := CriteriaBudget()
	b.Deadline = CeilingBudget().Deadline
	base := []Option{WithBuiltins(CriteriaBuiltins()...), WithBudget(b)}
	env, err := EnvOf[sampleEnv](r, append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// A parse error carries the line and column of the fault, on a multi-line
// source, 1-based.
func TestParsePosition(t *testing.T) {
	src := "value > 1 &&\n  state == 'down' &&\n  prior <"
	e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
	if e.Phase != PhaseParse {
		t.Errorf("phase = %q, want %q", e.Phase, PhaseParse)
	}
	if e.Line != 3 {
		t.Errorf("line = %d, want 3 (%v)", e.Line, e)
	}
	if !strings.Contains(e.Error(), "line 3") {
		t.Errorf("Error() = %q does not carry the line", e.Error())
	}
}

// A type error on a later line points at the offending token.
func TestTypePositionMultiline(t *testing.T) {
	src := "value > 1 &&\n  state == 'down' &&\n  nope > 2"
	e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
	if e.Phase != PhaseType {
		t.Errorf("phase = %q, want %q", e.Phase, PhaseType)
	}
	if e.Line != 3 || e.Column != 3 {
		t.Errorf("position = %d:%d, want 3:3 (%v)", e.Line, e.Column, e)
	}
}

// A source longer than the cap is refused before it is parsed.
func TestSourceLengthCap(t *testing.T) {
	src := "value > 1" + strings.Repeat(" ", MaxSourceBytes)
	e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
	if e.Phase != PhaseLimit || !errors.Is(e, ErrSourceTooLong) {
		t.Errorf("got %v (phase %q), want ErrSourceTooLong", e, e.Phase)
	}
	if _, err := Compile("value > 1"+strings.Repeat(" ", MaxSourceBytes-len("value > 1")), boolEnv(t)); err != nil {
		t.Errorf("a source exactly at the cap was refused: %v", err)
	}
}

// An expression with more AST nodes than the cap is refused, even when it fits
// the length cap.
func TestNodeCap(t *testing.T) {
	src := "value > 0" + strings.Repeat("+1", MaxNodes)
	if len(src) > MaxSourceBytes {
		t.Fatalf("test source %d bytes exceeds the length cap; the node cap is not what is tested", len(src))
	}
	e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
	if e.Phase != PhaseLimit || !errors.Is(e, ErrTooManyNodes) {
		t.Errorf("got %v (phase %q), want ErrTooManyNodes", e, e.Phase)
	}
}

// A run that walks past its site's budget stops with an error rather than
// running on: the criteria budget is small, a site may ask for more up to the
// ceiling.
func TestRunBudget(t *testing.T) {
	src := "count(items, # > 0) == -1"
	big := sampleEnv{Items: ints(20_000)}
	p, err := Compile(src, boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Run(big)
	e := asError(t, err)
	if e.Phase != PhaseLimit || !errors.Is(e, ErrRunBudget) {
		t.Errorf("got %v (phase %q), want ErrRunBudget", e, e.Phase)
	}
	p, err = Compile(src, mustEnv(t, Bool, WithBudget(CeilingBudget())))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Run(big); err != nil {
		t.Errorf("at the ceiling budget: %v", err)
	}
}

// The budget leaves room for honest criteria: a few passes over a bound list
// of a few hundred.
func TestRunBudgetAllowsHonestWork(t *testing.T) {
	p, err := Compile("all(items, # > 0) && len(filter(items, # % 2 == 0)) == 250 && 250 in items && count(items, # > 400) == 100", boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Run(sampleEnv{Items: ints(500)}); err != nil || got != true {
		t.Errorf("Run = %v, %v; want true, nil", got, err)
	}
}

// A realistic criteria compiles and runs well under the criteria budget.
func TestRealisticCriteria(t *testing.T) {
	for _, tc := range []struct {
		src  string
		in   sampleEnv
		want bool
	}{
		{`value > 90 && state == "down"`, sampleEnv{Value: 95, State: "down"}, true},
		{`value > 90 && state == "down"`, sampleEnv{Value: 95, State: "up"}, false},
		{`count(items, # > 90) >= 3 || any(labels, lower(#) startsWith "crit")`, sampleEnv{Items: ints(100), Labels: []string{"ok"}}, true},
		{`none(labels, trim(#) == "") && abs(value - prior) < 5`, sampleEnv{Labels: []string{"a"}, Value: 10, Prior: 7}, true},
	} {
		p, err := Compile(tc.src, boolEnv(t))
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if got, err := p.Run(tc.in); err != nil || got != tc.want {
			t.Errorf("%q = %v, %v; want %v", tc.src, got, err, tc.want)
		}
	}
}

// Review 2: every repro is refused at compile time, with a position. The
// builtins they use cannot be bounded by charging their inputs (Go-native
// work the deadline cannot pre-empt), so no site admits them.
func TestUnboundableRefused(t *testing.T) {
	for _, src := range []string{
		`len(repeat(repeat("a", 90000), 10000)) > 0`,
		`let a = 1..10; let b = map(a, a); max(b) > 0`,
		`len(toJSON(items)) > 0`,
		`len(flatten([items, items])) > 0`,
		`mean(items) > 0`,
		`len(uniq(1..40000)) > 0`,
		`len(replace(repeat("a", 10000), "", repeat("b", 10000))) > 0`,
		`len(join(map(1..5000, "x"), repeat("y", 20000))) > 0`,
		`len(1..10) > 0`,
		`sum(items) > 0`,
		`len(split(state, ",")) > 0`,
		`fromJSON(state) != nil`,
		`len(toBase64(state)) > 0`,
		`reduce(items, #acc + #, 0) > 0`,
		`len(sortBy(items, #)) > 0`,
		`len(concat(items, items)) > 0`,
		`int(state) > 0`,
		`max(items) > 0`,
		`min(0, items) < 0`,
		`all(items, any(items, # > 0))`,
		`count(items, len(filter(items, # > 1)) > 0) > 0`,
		`len([` + strings.Repeat("1,", MaxLiteralElements) + `1]) > 0`,
		`len({` + strings.Repeat("a: 1, ", MaxLiteralElements) + `b: 1}) > 0`,
	} {
		_, err := Compile(src, boolEnv(t))
		if err == nil {
			t.Errorf("%q compiled", src)
			continue
		}
		var e *Error
		if !errors.As(err, &e) || (e.Phase != PhaseType && e.Phase != PhaseLimit) || e.Line == 0 {
			t.Errorf("%q: got %v, want a positioned compile refusal", src, err)
		}
	}
}

// A site admits only builtins it names, and only from the vetted set; a
// budget is per site and no larger than the ceiling.
func TestSiteDeclaration(t *testing.T) {
	bare, err := EnvOf[sampleEnv](Bool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile("value > 1 && state == 'x'", bare); err != nil {
		t.Errorf("operators refused at a site with no builtins: %v", err)
	}
	// Compiled at the criteria site first, so a cache keyed without the
	// builtins would hand the bare site this program.
	if _, err := Compile("len(items) > 0", boolEnv(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile("len(items) > 0", bare); err == nil {
		t.Error("a site that names no builtins admitted len")
	}
	for name, opt := range map[string]Option{
		"unvetted builtin":   WithBuiltins("map"),
		"unknown builtin":    WithBuiltins("nope"),
		"over the ceiling":   WithBudget(Budget{Iterations: CeilingBudget().Iterations + 1, Memory: 1, Deadline: time.Millisecond}),
		"zero budget":        WithBudget(Budget{}),
		"deadline too large": WithBudget(Budget{Iterations: 1, Memory: 1, Deadline: time.Hour}),
	} {
		if _, err := EnvOf[sampleEnv](Bool, opt); err == nil {
			t.Errorf("%s: EnvOf accepted it", name)
		}
	}
}

// Every builtin that reads the clock, the timezone database, or the process
// environment is refused at compile time.
func TestDisabledBuiltins(t *testing.T) {
	for _, src := range []string{
		"now() != nil",
		"date('2024-01-01') != nil",
		"duration('1h') != nil",
		"timezone('UTC') != nil",
		"now().Year() > 2000",
	} {
		e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
		if e.Phase != PhaseType {
			t.Errorf("%q: phase = %q, want %q (%v)", src, e.Phase, PhaseType, e)
		}
	}
	for _, name := range DisabledBuiltins() {
		if _, err := Compile(name+"() != nil", boolEnv(t)); err == nil {
			t.Errorf("disabled builtin %s compiled", name)
		}
	}
}

// A hostile operator value is data: it is compared and returned literally,
// never executed.
func TestHostileInputIsData(t *testing.T) {
	hostile := []string{
		`now()`,
		`') || true || ('`,
		`" + toJSON($env) + "`,
		`1..10000000`,
		"value > 0 }}{{ state",
	}
	cmp, err := Compile("state == 'down'", boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	echo, err := Compile("state", mustEnv(t, String))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hostile {
		got, err := cmp.Run(sampleEnv{State: h})
		if err != nil || got != false {
			t.Errorf("state %q: compare = %v, %v; want false, nil", h, got, err)
		}
		got, err = echo.Run(sampleEnv{State: h})
		if err != nil || got != h {
			t.Errorf("state %q: echo = %v, %v; want it back verbatim", h, got, err)
		}
	}
}

// The declared bindings are the whole namespace: the engine's own plumbing
// and Expr's dynamic $env are not reachable from source.
func TestDollarNamesRefused(t *testing.T) {
	for _, src := range []string{
		"$meter != nil",
		"$site.value > 1",
		"$charge($meter, items) != nil",
		"$env.value > 1",
		`$env["$meter"] != nil`,
		"value > 1 &&\n $env.prior > 1",
	} {
		e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
		if e.Phase != PhaseType || e.Line == 0 {
			t.Errorf("%q: got %v (phase %q, line %d), want a positioned type error", src, e, e.Phase, e.Line)
		}
	}
	e := asError(t, func() error { _, err := Compile("value > 1 &&\n $env.prior > 1", boolEnv(t)); return err }())
	if e.Line != 2 || e.Column != 2 {
		t.Errorf("position = %d:%d, want 2:2", e.Line, e.Column)
	}
}

// The environment exposes data only: a struct with a func field, a channel,
// or methods is refused when the site is declared.
func TestEnvRefusesBehavior(t *testing.T) {
	type withFunc struct {
		F func() int `expr:"f"`
	}
	type withChan struct {
		C chan int `expr:"c"`
	}
	type withNested struct {
		N struct{ G func() } `expr:"n"`
	}
	type withMethodField struct {
		M methodful `expr:"m"`
	}
	type withAny struct {
		Extra any `expr:"extra"`
	}
	type withAnyMap struct {
		M map[string]any `expr:"m"`
	}
	type withAnyList struct {
		L []any `expr:"l"`
	}
	for name, err := range map[string]error{
		"func":         func() error { _, err := EnvOf[withFunc](Bool); return err }(),
		"chan":         func() error { _, err := EnvOf[withChan](Bool); return err }(),
		"nested func":  func() error { _, err := EnvOf[withNested](Bool); return err }(),
		"methods":      func() error { _, err := EnvOf[methodful](Bool); return err }(),
		"method field": func() error { _, err := EnvOf[withMethodField](Bool); return err }(),
		"not a struct": func() error { _, err := EnvOf[int](Bool); return err }(),
		// An interface field, even the empty one, lets a caller bind a func,
		// a time.Time, or a Stringer, and an expression then calls it.
		"any":      func() error { _, err := EnvOf[withAny](Bool); return err }(),
		"any map":  func() error { _, err := EnvOf[withAnyMap](Bool); return err }(),
		"any list": func() error { _, err := EnvOf[withAnyList](Bool); return err }(),
	} {
		if err == nil {
			t.Errorf("%s: env accepted", name)
		}
	}
}

type methodful struct {
	X int `expr:"x"`
}

func (methodful) Leak() string { return "side effect" }

// The bindings list comes from the struct: names from the expr tag, types from
// the field, docs from the doc tag, in declaration order; a nested struct's
// fields are listed by their dotted path (point.x), the way an expression
// names them.
func TestBindings(t *testing.T) {
	got := boolEnv(t).Bindings()
	want := []Binding{
		{Name: "value", Type: "float64", Doc: "the arriving sample's number"},
		{Name: "state", Type: "string", Doc: "the arriving sample's text"},
		{Name: "prior", Type: "float64", Doc: "the previous sample's number"},
		{Name: "items", Type: "[]int", Doc: "a list of numbers"},
		{Name: "labels", Type: "[]string"},
		{Name: "tags", Type: "map[string]int", Doc: "a map of counts"},
		{Name: "n", Type: "int"},
		{Name: "nested", Type: "[][]int"},
		{Name: "point.x", Type: "int", Doc: "the point's x"},
		{Name: "flags", Type: "[]bool"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Bindings() =\n%v\nwant\n%v", got, want)
	}
}

// Compiling the same source against the same env shape compiles once; another
// shape or another result recompiles.
func TestCacheReuse(t *testing.T) {
	// A fresh cache, so the test holds when run repeatedly (-count).
	saved := programs
	programs = newCache(MaxCachedPrograms)
	defer func() { programs = saved }()
	src := "value > 12345.678 // cache reuse"
	before := compileCount.Load()
	p1, err := Compile(src, boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Compile(src, boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := compileCount.Load() - before; n != 1 {
		t.Errorf("two compiles of one key compiled %d times, want 1", n)
	}
	if p1 != p2 {
		t.Error("the cached program was not reused")
	}

	type otherEnv struct {
		Value float64 `expr:"value"`
	}
	other, err := EnvOf[otherEnv](Bool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(src, other); err != nil {
		t.Fatal(err)
	}
	if n := compileCount.Load() - before; n != 2 {
		t.Errorf("a different env shape compiled %d times in total, want 2", n)
	}
}

// The cache is bounded: past its size the least recently used program goes.
func TestCacheBound(t *testing.T) {
	c := newCache(2)
	env := boolEnv(t)
	k := func(s string) cacheKey { return cacheKey{source: s, env: env} }
	c.put(k("a"), &Program{})
	c.put(k("b"), &Program{})
	if _, ok := c.get(k("a")); !ok { // a is now most recent
		t.Fatal("a missing")
	}
	c.put(k("c"), &Program{})
	if _, ok := c.get(k("b")); ok {
		t.Error("b survived past the bound; it was least recently used")
	}
	for _, s := range []string{"a", "c"} {
		if _, ok := c.get(k(s)); !ok {
			t.Errorf("%s evicted", s)
		}
	}
	if c.len() != 2 {
		t.Errorf("cache holds %d, want 2", c.len())
	}
}

// Compile and Run are safe from many goroutines at once (run under -race).
func TestConcurrent(t *testing.T) {
	env := boolEnv(t)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				src := fmt.Sprintf("value > %d", i%7)
				p, err := Compile(src, env)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := p.Run(sampleEnv{Value: float64(g)})
				if err != nil {
					t.Error(err)
					return
				}
				if got != (g > i%7) {
					t.Errorf("%s with value %d = %v", src, g, got)
				}
			}
		}(g)
	}
	wg.Wait()
}

// The zero Env is not a site; compiling against it is refused.
func TestZeroEnv(t *testing.T) {
	if _, err := Compile("true", Env{}); err == nil {
		t.Error("compiled against the zero Env")
	}
}

// budgetErr asserts a run failed on its budget.
func budgetErr(t *testing.T, src string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%q: ran within budget, want ErrRunBudget", src)
		return
	}
	if !errors.Is(err, ErrRunBudget) {
		t.Errorf("%q: got %v, want ErrRunBudget", src, err)
	}
}

func runOn(t *testing.T, src string, in sampleEnv) error {
	t.Helper()
	p, err := Compile(src, boolEnv(t))
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	_, err = p.Run(in)
	return err
}

func ints(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// Review #1: string concatenation is charged, so doubling a string in a chain
// of lets stops at the budget instead of allocating.
func TestStringConcatCharged(t *testing.T) {
	var b strings.Builder
	b.WriteString("let a0 = state; ")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "let a%d = a%d + a%d; ", i, i-1, i-1)
	}
	b.WriteString("len(a12) > 0")
	budgetErr(t, b.String(), runOn(t, b.String(), sampleEnv{State: strings.Repeat("x", 2000)}))
}

// Review #3: binary operators that walk their operands are charged.
func TestWalkingOperatorsCharged(t *testing.T) {
	big := strings.Repeat("a", 100_000)
	for _, src := range []string{
		"count(labels, state contains 'zz') == -1",
		"count(labels, state startsWith 'zz') == -1",
		"count(labels, state endsWith 'zz') == -1",
		"count(labels, state matches 'zz') == -1",
		"count(labels, state == #) == -1",
		"count(labels, state != #) == -1",
		"count(labels, state < #) == -1",
		"count(labels, state >= #) == -1",
		"count(labels, items == items) == -1",
		"count(labels, upper(state) == '') == -1",
	} {
		budgetErr(t, src, runOn(t, src, sampleEnv{State: big, Labels: make([]string, 50), Items: ints(50_000)}))
	}
}

// Review #3: a matches pattern is a constant, so its regex compiles once; a
// pattern computed at run time is refused at compile time, with its position.
func TestDynamicMatchesRefused(t *testing.T) {
	e := asError(t, func() error { _, err := Compile("value > 0 &&\n state matches state", boolEnv(t)); return err }())
	if e.Phase != PhaseType || e.Line != 2 {
		t.Errorf("got %v (phase %q, line %d), want a type error on line 2", e, e.Phase, e.Line)
	}
	if _, err := Compile("state matches '^d.*n$'", boolEnv(t)); err != nil {
		t.Errorf("a constant pattern was refused: %v", err)
	}
}

// Review #4: an expression calls builtins only: no method, no value.
func TestOnlyBuiltinsCalled(t *testing.T) {
	for _, src := range []string{
		"state.x() == 1",
		"value() > 1",
		"state.Len() > 1",
	} {
		e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
		if e.Phase != PhaseType || e.Line == 0 || !strings.Contains(e.Message, "builtins only") {
			t.Errorf("%q: got %v (phase %q), want a positioned type error naming builtins only", src, e, e.Phase)
		}
	}
}

// Review #5: `in` on a map is a lookup, not a walk, and is not charged.
func TestInOnMapNotCharged(t *testing.T) {
	tags := make(map[string]int, 100_000)
	for i := 0; i < 100_000; i++ {
		tags[fmt.Sprint(i)] = i
	}
	src := "count(labels, 'k' in tags) == 0"
	if err := runOn(t, src, sampleEnv{Tags: tags, Labels: make([]string, 50)}); err != nil {
		t.Errorf("%q: %v", src, err)
	}
}

// Review #6: integer arithmetic wraps on overflow (Expr's int is Go's int).
// Pinned so a change in Expr is seen.
func TestIntegerOverflowWraps(t *testing.T) {
	p, err := Compile("n + 1", mustEnv(t, Int))
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Run(sampleEnv{N: math.MaxInt})
	if err != nil || got != math.MinInt {
		t.Errorf("MaxInt + 1 = %v, %v; want MinInt (wrapped)", got, err)
	}
}

// Review #7: the wall-clock deadline is a backstop on charged work. With
// it set to a nanosecond, a run stops at its first charge.
func TestDeadlineBackstop(t *testing.T) {
	env := mustEnv(t, Bool, WithBudget(Budget{Iterations: 10_000, Memory: 10_000, Deadline: time.Nanosecond}))
	p, err := Compile("count(items, # > 0) == -1", env)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Run(sampleEnv{Items: ints(10)})
	budgetErr(t, "deadline", err)
}

// Review 3 #1: a regex costs its subject times its program. A pattern whose
// compiled program is past the cap is refused, with its position; a run is
// charged the subject's length times the program's size.
func TestRegexCost(t *testing.T) {
	e := asError(t, func() error {
		_, err := Compile("value > 0 &&\n state matches '(a|aa|aaa|b){0,1000}z'", boolEnv(t))
		return err
	}())
	if e.Phase != PhaseLimit || !errors.Is(e, ErrPatternTooLarge) || e.Line != 2 {
		t.Errorf("got %v (phase %q, line %d), want ErrPatternTooLarge on line 2", e, e.Phase, e.Line)
	}
	src := "state matches '(a|b)+z'"
	budgetErr(t, src, runOn(t, src, sampleEnv{State: strings.Repeat("a", 5000)}))
	if err := runOn(t, src, sampleEnv{State: strings.Repeat("a", 50) + "z"}); err != nil {
		t.Errorf("a short subject: %v", err)
	}
}

// Review 3 #2: equality and membership on a struct, a map, or a list of
// lists would fall to a deep comparison its charge does not cover; refused
// at compile time. Scalars, lists of scalars, and a key in a map stay.
func TestDeepEqualityRefused(t *testing.T) {
	for _, src := range []string{
		"tags == tags",
		"tags != tags",
		"nested == nested",
		"point == point",
		"point != point",
		"items in nested",
		"point in [point]",
	} {
		e := asError(t, func() error { _, err := Compile(src, boolEnv(t)); return err }())
		if e.Phase != PhaseType || e.Line == 0 {
			t.Errorf("%q: got %v (phase %q), want a positioned type error", src, e, e.Phase)
		}
	}
	for _, src := range []string{
		"items == items", "'k' in tags", "1 in items", "state == 'x'", "point.x == 1", "labels != labels",
		"items == [1, 2]",
	} {
		if _, err := Compile(src, boolEnv(t)); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	// A list literal of scalars is the common criteria form.
	p, err := Compile(`state in ["down", "fault"]`, boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Run(sampleEnv{State: "fault"}); err != nil || got != true {
		t.Errorf(`state in ["down", "fault"] = %v, %v`, got, err)
	}
}

// Review 3 #3: count without a predicate counts true elements, so its list
// must be of bools, checked at compile time rather than failing at run time.
func TestCountWithoutPredicateNeedsBools(t *testing.T) {
	e := asError(t, func() error { _, err := Compile("count(items) > 0", boolEnv(t)); return err }())
	if e.Phase != PhaseType || e.Line == 0 {
		t.Errorf("got %v (phase %q), want a positioned type error", e, e.Phase)
	}
	p, err := Compile("count(flags) == 2", boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Run(sampleEnv{Flags: []bool{true, false, true}}); err != nil || got != true {
		t.Errorf("count(flags) == 2 = %v, %v", got, err)
	}
}

// Review 3 #4: the sandbox's settings are values handed out fresh, so no
// caller can widen them for everyone.
func TestSettingsAreCopies(t *testing.T) {
	b := CriteriaBuiltins()
	b[0] = "repeat"
	if CriteriaBuiltins()[0] == "repeat" {
		t.Error("CriteriaBuiltins is shared")
	}
	d := DisabledBuiltins()
	d[0] = "len"
	if DisabledBuiltins()[0] == "len" {
		t.Error("DisabledBuiltins is shared")
	}
	c := CriteriaBudget()
	c.Iterations = 1 << 40
	if CriteriaBudget().Iterations == 1<<40 {
		t.Error("CriteriaBudget is shared")
	}
	if _, err := EnvOf[sampleEnv](Bool, WithBuiltins("repeat")); err == nil {
		t.Error("an unvetted builtin was admitted")
	}
}

// Check compiles without the shared program cache: a caller cycling sources
// (the validate route) cannot evict the programs rules run on, and gets the
// same positioned diagnostics Compile would.
func TestCheckDoesNotCache(t *testing.T) {
	saved := programs
	programs = newCache(MaxCachedPrograms)
	defer func() { programs = saved }()
	if err := Check("value > 42.4242 // check only", boolEnv(t)); err != nil {
		t.Fatal(err)
	}
	if n := programs.len(); n != 0 {
		t.Errorf("Check cached %d programs, want 0", n)
	}
	e := asError(t, Check("value > 1 &&\n  nope", boolEnv(t)))
	if e.Line != 2 || e.Column != 3 || e.Phase != PhaseType {
		t.Errorf("Check's diagnostic %v at %d:%d, want a type error at 2:3", e, e.Line, e.Column)
	}
	if err := Check(strings.Repeat(" ", MaxSourceBytes+1), boolEnv(t)); !errors.Is(err, ErrSourceTooLong) {
		t.Errorf("Check skipped the length cap: %v", err)
	}
}

// A diagnostic names a nested binding by its path, never by its Go type.
func TestDiagnosticsSpeakInBindings(t *testing.T) {
	e := asError(t, func() error { _, err := Compile("point > 1", boolEnv(t)); return err }())
	if strings.Contains(e.Message, "expression.point") || !strings.Contains(e.Message, "point") {
		t.Errorf("message %q names the Go type, want the binding point", e.Message)
	}
}
