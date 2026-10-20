package expression

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
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
}

func boolEnv(t *testing.T) Env {
	t.Helper()
	env, err := EnvOf[sampleEnv](Bool)
	if err != nil {
		t.Fatal(err)
	}
	return env
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
	// An expression whose type is unknown until run time is refused too: the
	// site asked for a bool, and "maybe" is not one.
	if _, err := Compile(`fromJSON(state)`, boolEnv(t)); err == nil {
		t.Error("an expression of unknown type compiled against a bool site")
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
		env, err := EnvOf[sampleEnv](tc.result)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Compile(tc.src, env)
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

func mustEnv(t *testing.T, r Result) Env {
	t.Helper()
	env, err := EnvOf[sampleEnv](r)
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

// A run that allocates or iterates past the budget stops with an error rather
// than exhausting memory or time.
func TestRunBudget(t *testing.T) {
	for _, src := range []string{
		"len(1..10000000) > 0",
		"len(map(1..1000, map(1..1000, map(1..1000, #)))) > 0",
		// Iteration over bound data allocates nothing; it is the work budget
		// that stops these. (== -1, not > 0: Expr ends count early at a
		// threshold it can see.)
		"count(items, all(items, all(items, # > 0))) == -1",
		"count(items, all(items, # in items)) == -1",
		"count(items, sum(items) > 0 && sum(items) > 1 && sum(items) > 2) == -1",
	} {
		p, err := Compile(src, boolEnv(t))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		items := make([]int, 1000)
		for i := range items {
			items[i] = i + 1
		}
		_, err = p.Run(sampleEnv{Items: items})
		e := asError(t, err)
		if e.Phase != PhaseLimit || !errors.Is(e, ErrRunBudget) {
			t.Errorf("%q: got %v (phase %q), want ErrRunBudget", src, e, e.Phase)
		}
	}
}

// The budget leaves room for honest work: one pass, or a few, over a bound
// list of a thousand runs fine.
func TestRunBudgetAllowsHonestWork(t *testing.T) {
	items := make([]int, 1000)
	for i := range items {
		items[i] = i + 1
	}
	p, err := Compile("all(items, # > 0) && sum(items) > 0 && len(filter(items, # % 2 == 0)) == 500 && 500 in items", boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Run(sampleEnv{Items: items}); err != nil || got != true {
		t.Errorf("Run = %v, %v; want true, nil", got, err)
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
	for _, name := range DisabledBuiltins {
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
// the field, docs from the doc tag, in declaration order.
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
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Bindings() =\n%v\nwant\n%v", got, want)
	}
}

// Compiling the same source against the same env shape compiles once; another
// shape or another result recompiles.
func TestCacheReuse(t *testing.T) {
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
	k := func(s string) cacheKey { return cacheKey{source: s, env: env.typ, result: env.result} }
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
// of lets stops at the budget instead of allocating gigabytes.
func TestStringConcatCharged(t *testing.T) {
	var b strings.Builder
	b.WriteString("let a0 = repeat('x', 99999); ")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "let a%d = a%d + a%d; ", i, i-1, i-1)
	}
	b.WriteString("len(a5) > 0")
	budgetErr(t, b.String(), runOn(t, b.String(), sampleEnv{}))
}

// Review #2: a variadic builtin is charged for every argument it walks, not
// only its first.
func TestVariadicArgumentsCharged(t *testing.T) {
	src := "count(1..300, max(0, items, items, items, items, items) > 0) == -1"
	budgetErr(t, src, runOn(t, src, sampleEnv{Items: ints(1000)}))
}

// Review #3: binary operators that walk their operands are charged.
func TestWalkingOperatorsCharged(t *testing.T) {
	big := strings.Repeat("a", 100_000)
	for _, src := range []string{
		"count(1..50, state contains 'zz') == -1",
		"count(1..50, state startsWith 'zz') == -1",
		"count(1..50, state endsWith 'zz') == -1",
		"count(1..50, state matches 'zz') == -1",
		"count(1..50, state == labels[0]) == -1",
		"count(1..50, state != labels[0]) == -1",
		"count(1..50, state < labels[0]) == -1",
		"count(1..50, state >= labels[0]) == -1",
		"count(1..50, items == items) == -1",
	} {
		budgetErr(t, src, runOn(t, src, sampleEnv{State: big, Labels: []string{big}, Items: ints(50_000)}))
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
		"fromJSON(state).x() == 1",
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
	src := "count(1..50, 'k' in tags) == 0"
	if err := runOn(t, src, sampleEnv{Tags: tags}); err != nil {
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

// Review #7: the wall-clock deadline is a backstop. With it set to nothing,
// any run that reaches a charge point stops on it.
func TestDeadlineBackstop(t *testing.T) {
	saved := runDeadline
	runDeadline = 0
	defer func() { runDeadline = saved }()
	src := "sum(items) > 0"
	budgetErr(t, src, runOn(t, src, sampleEnv{Items: ints(10)}))
}
