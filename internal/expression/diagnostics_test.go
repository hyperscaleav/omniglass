package expression

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// checkErr is Check's refusal alone.
func checkErr(source string, env Env) error {
	_, err := Check(source, env)
	return err
}

// useRules runs a test against rs in place of the registry, restored after.
// Tests that use it check with Check (no program cache) or a source no other
// test compiles, so a cached program never carries another registry's
// findings.
func useRules(t *testing.T, rs ...rule) {
	t.Helper()
	if err := validRules(rs); err != nil {
		t.Fatalf("test rules are not valid: %v", err)
	}
	was := registry
	registry = rs
	t.Cleanup(func() { registry = was })
}

// named finds the first identifier called name, for a test matcher.
func identNamed(root ast.Node, name string) ast.Node {
	var at ast.Node
	ast.Walk(&root, visitFunc(func(n *ast.Node) {
		if id, ok := (*n).(*ast.IdentifierNode); ok && id.Value == name && at == nil {
			at = id
		}
	}))
	return at
}

// testRule is a rule that flags the first use of name: a refine rule (an
// error, offering to rename it to fix), or a lint rule of the given severity.
func testRule(code string, trig trigger, sev Severity, name, fix string, sites ...string) rule {
	opts := []Option{WithBuiltins(CriteriaBuiltins()...)}
	if len(sites) > 0 {
		opts = append(opts, WithSite(sites[0]))
	}
	env := MustEnv[sampleEnv](Bool, opts...)
	r := rule{
		code: code, severity: sev, trigger: trig, sites: sites,
		message: name + " is flagged",
		match: func(s *subject) []finding {
			at := identNamed(s.root, name)
			if at == nil || (s.fault != nil && !s.near(at)) {
				return nil
			}
			f := finding{from: at.Location().From}
			if fix != "" {
				from, to := s.span(at)
				f.fix = &edit{title: "Use " + fix, from: from, to: to, replacement: fix}
			}
			return []finding{f}
		},
	}
	// The fixtures: one where it gives the diagnostic, one where it does not.
	switch trig {
	case triggerRefine:
		r.fixtures = []fixture{{env: env, source: "flags + 1 > 0", want: true, column: 1, fixed: fixedOr(fix, "flags + 1 > 0")}, {env: env, source: "value + 1 > 0"}}
	default:
		r.fixtures = []fixture{{env: env, source: name + " > 1", want: true, column: 1, fixed: fixedOr(fix, name+" > 1")}, {env: env, source: "value > 1"}}
	}
	return r
}

func fixedOr(fix, source string) string {
	if fix == "" {
		return ""
	}
	return strings.Replace(source, strings.Fields(source)[0], fix, 1)
}

// A refine rule replaces a refusal it recognises with its own, at the place
// it names, with its code and its fix as a positioned edit.
func TestRefineReplacesRefusal(t *testing.T) {
	useRules(t, testRule("flags-arith", triggerRefine, SeverityError, "flags", "items"))
	_, err := Check("value > 0 &&\n  flags + 1 > 0", boolEnv(t))
	e := asError(t, err)
	if e.Code != "flags-arith" || e.Line != 2 || e.Column != 3 || e.Message != "flags is flagged" || e.Phase != PhaseType {
		t.Fatalf("refusal = %+v, want flags-arith at 2:3", e)
	}
	if e.Fix == nil || *e.Fix != (Fix{Title: "Use items", Line: 2, Column: 3, EndLine: 2, EndColumn: 8, Replacement: "items"}) {
		t.Fatalf("fix = %+v", e.Fix)
	}
	if e.Unbound != "" {
		t.Errorf("a refined refusal names no unbound name, got %q", e.Unbound)
	}
}

// A refusal no refine rule recognises is Expr's, unchanged and uncoded.
func TestRefineLeavesUnrecognisedRefusal(t *testing.T) {
	useRules(t, testRule("flags-arith", triggerRefine, SeverityError, "flags", "items"))
	_, err := Check("state + 1 > 0", boolEnv(t))
	e := asError(t, err)
	if e.Code != "" || e.Fix != nil || !strings.Contains(e.Message, "mismatched types") {
		t.Fatalf("refusal = %+v, want Expr's own", e)
	}
	// Nor does a refine rule run on an expression that compiles.
	if diags, err := Check("len(flags) > 0", boolEnv(t)); err != nil || len(diags) != 0 {
		t.Fatalf("Check = %v, %v", diags, err)
	}
}

// A lint warning or hint rides along with an expression that compiles: Check
// returns it and the compiled program carries it; neither refuses.
func TestLintFindingsRideAlong(t *testing.T) {
	useRules(t,
		testRule("prior-read", triggerLint, SeverityWarning, "prior", ""),
		testRule("n-read", triggerLint, SeverityHint, "n", "1"),
	)
	src := "n > 0 && prior > 1 // lint-ride-along"
	diags, err := Check(src, boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []Diagnostic{
		{Code: "n-read", Severity: SeverityHint, Line: 1, Column: 1, Message: "n is flagged",
			Fix: &Fix{Title: "Use 1", Line: 1, Column: 1, EndLine: 1, EndColumn: 2, Replacement: "1"}},
		{Code: "prior-read", Severity: SeverityWarning, Line: 1, Column: 10, Message: "prior is flagged"},
	}
	if !sameDiagnostics(diags, want) {
		t.Fatalf("Check diagnostics = %+v, want %+v", diags, want)
	}
	p, err := Compile(src, boolEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	if !sameDiagnostics(p.Diagnostics(), want) {
		t.Fatalf("program diagnostics = %+v", p.Diagnostics())
	}
	// A caller cannot change what the cached program carries.
	p.Diagnostics()[0].Message = "changed"
	if p.Diagnostics()[0].Message != "n is flagged" {
		t.Error("Diagnostics returned the program's own slice")
	}
}

func sameDiagnostics(got, want []Diagnostic) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		g, w := got[i], want[i]
		if g.Code != w.Code || g.Severity != w.Severity || g.Line != w.Line || g.Column != w.Column || g.Message != w.Message {
			return false
		}
		if (g.Fix == nil) != (w.Fix == nil) || (g.Fix != nil && *g.Fix != *w.Fix) {
			return false
		}
	}
	return true
}

// A lint rule of error severity refuses an expression that otherwise
// compiles, with its code.
func TestLintErrorRefuses(t *testing.T) {
	useRules(t, testRule("prior-banned", triggerLint, SeverityError, "prior", ""))
	_, err := Check("prior > 1", boolEnv(t))
	e := asError(t, err)
	if e.Code != "prior-banned" || e.Line != 1 || e.Column != 1 || e.Phase != PhaseType {
		t.Fatalf("refusal = %+v", e)
	}
}

// A rule runs only at the sites it names; a rule naming none runs at every
// site, and a site-less environment runs only those.
func TestRuleSites(t *testing.T) {
	useRules(t,
		testRule("calc-only", triggerLint, SeverityWarning, "prior", "", "calc"),
		testRule("everywhere", triggerLint, SeverityWarning, "n", ""),
	)
	codes := func(env Env) []string {
		diags, err := Check("prior > n", env)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range diags {
			out = append(out, d.Code)
		}
		return out
	}
	if got := strings.Join(codes(mustEnv(t, Bool, WithSite("calc"))), ","); got != "calc-only,everywhere" {
		t.Errorf("at calc: %s", got)
	}
	if got := strings.Join(codes(mustEnv(t, Bool, WithSite("criteria"))), ","); got != "everywhere" {
		t.Errorf("at criteria: %s", got)
	}
	if got := strings.Join(codes(boolEnv(t)), ","); got != "everywhere" {
		t.Errorf("site-less: %s", got)
	}
	// The site is part of the environment's identity, so the program cache
	// never hands one site's findings to another.
	if mustEnv(t, Bool, WithSite("calc")) == mustEnv(t, Bool, WithSite("criteria")) {
		t.Error("two sites' environments are equal")
	}
	if got := mustEnv(t, Bool, WithSite("calc")).Site(); got != "calc" {
		t.Errorf("Site() = %q", got)
	}
}

// A matcher that panics is a bug in the rule, not in the expression: the
// expression is judged as if the rule were absent (a refusal stays Expr's, a
// compiling expression compiles), and the server never crashes.
func TestPanickingMatcherIsSkipped(t *testing.T) {
	boom := func(code string, trig trigger) rule {
		r := testRule(code, trig, SeverityError, "flags", "")
		r.match = func(*subject) []finding { panic("matcher bug") }
		return r
	}
	useRules(t, boom("boom-refine", triggerRefine), boom("boom-lint", triggerLint))
	if _, err := Check("len(flags) > 0", boolEnv(t)); err != nil {
		t.Fatalf("a panicking lint refused a good expression: %v", err)
	}
	_, err := Check("flags + 1 > 0", boolEnv(t))
	if e := asError(t, err); e.Code != "" || !strings.Contains(e.Message, "mismatched types") {
		t.Fatalf("refusal = %+v, want Expr's own", e)
	}
}

// The registry refuses a rule it cannot stand behind: no fixtures, no case it
// must give and one it must not, a duplicate or ill-formed code, a refine
// rule that is not an error, no matcher, no message.
func TestValidRulesRefuses(t *testing.T) {
	good := func() rule { return testRule("good-rule", triggerLint, SeverityWarning, "prior", "") }
	for name, mutate := range map[string]func(r *rule) []rule{
		"no fixtures":         func(r *rule) []rule { r.fixtures = nil; return []rule{*r} },
		"no positive fixture": func(r *rule) []rule { r.fixtures = r.fixtures[1:]; return []rule{*r} },
		"no negative fixture": func(r *rule) []rule { r.fixtures = r.fixtures[:1]; return []rule{*r} },
		"duplicate code":      func(r *rule) []rule { return []rule{*r, *r} },
		"bad code":            func(r *rule) []rule { r.code = "Bad Code"; return []rule{*r} },
		"refine warning":      func(r *rule) []rule { r.trigger = triggerRefine; return []rule{*r} },
		"unknown severity":    func(r *rule) []rule { r.severity = "fatal"; return []rule{*r} },
		"unknown trigger":     func(r *rule) []rule { r.trigger = "always"; return []rule{*r} },
		"no matcher":          func(r *rule) []rule { r.match = nil; return []rule{*r} },
		"no message":          func(r *rule) []rule { r.message = ""; return []rule{*r} },
		"fixture without env": func(r *rule) []rule { r.fixtures[0].env = Env{}; return []rule{*r} },
	} {
		r := good()
		if err := validRules(mutate(&r)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := validRules([]rule{good()}); err != nil {
		t.Errorf("a good rule refused: %v", err)
	}
}

// The fixture runner holds each rule to its examples: the diagnostic where it
// must be, at the column given, absent where it must not be, and a fix that
// yields the fixed source, which compiles without it.
func TestFixturesHoldARule(t *testing.T) {
	ok := testRule("prior-read", triggerLint, SeverityWarning, "prior", "n")
	if err := ok.checkFixtures(); err != nil {
		t.Fatalf("agreeing fixtures failed: %v", err)
	}
	for name, mutate := range map[string]func(r *rule){
		"wrong column":      func(r *rule) { r.fixtures[0].column = 4 },
		"missing finding":   func(r *rule) { r.fixtures[0].source = "value > 1" },
		"unwanted finding":  func(r *rule) { r.fixtures[1].source = "prior > 2" },
		"wrong fixed":       func(r *rule) { r.fixtures[0].fixed = "value > 1" },
		"fix not declared":  func(r *rule) { r.fixtures[0].fixed = "" },
		"fixed not compile": func(r *rule) { r.fixtures[0].fixed = "n >" },
		// A positive fixture refused by anything but the rule.
		"source not compile": func(r *rule) {
			r.fixtures[0].source = "prior >"
		},
	} {
		r := testRule("prior-read", triggerLint, SeverityWarning, "prior", "n")
		if name == "fixed not compile" {
			// A fix that breaks the expression is the rule's bug.
			r.match = func(s *subject) []finding {
				at := identNamed(s.root, "prior")
				if at == nil {
					return nil
				}
				from, _ := s.span(at)
				return []finding{{from: from, fix: &edit{title: "break", from: from, to: len([]rune(s.source)), replacement: "n >"}}}
			}
		}
		mutate(&r)
		if err := r.checkFixtures(); err == nil {
			t.Errorf("%s: fixtures passed", name)
		}
	}
}

// Every registered rule passes its own fixtures: a rule whose example
// disagrees with it fails the build here.
func TestEveryDiagnosticPassesItsFixtures(t *testing.T) {
	if err := validRules(registry); err != nil {
		t.Fatal(err)
	}
	for _, r := range registry {
		if err := r.checkFixtures(); err != nil {
			t.Errorf("%s: %v", r.code, err)
		}
	}
}

// span covers a node's whole source: a call through its closing bracket, an
// operator from its left operand to its right, never the engine's own
// plumbing (a charge's wrapper).
func TestSpan(t *testing.T) {
	src := "count(items, # > 1) == len(items) && (n + 1) > 0"
	s := &subject{source: src}
	parsed, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	tree := parsed.Node
	s.root = tree
	bin := tree.(*ast.BinaryNode).Left
	from, to := s.span(bin)
	if got := src[from:to]; got != "count(items, # > 1) == len(items)" {
		t.Errorf("span = %q", got)
	}
	call := bin.(*ast.BinaryNode).Left
	from, to = s.span(call)
	if got := src[from:to]; got != "count(items, # > 1)" {
		t.Errorf("call span = %q", got)
	}
}

// DiagnosticRules is the registry as a caller reads it, a copy.
func TestDiagnosticRulesListsTheRegistry(t *testing.T) {
	useRules(t, testRule("calc-only", triggerLint, SeverityWarning, "prior", "", "calc"))
	got := DiagnosticRules()
	if len(got) != 1 || got[0].Code != "calc-only" || got[0].Severity != SeverityWarning || got[0].Trigger != "lint" ||
		len(got[0].Sites) != 1 || got[0].Sites[0] != "calc" || got[0].Message != "prior is flagged" {
		t.Fatalf("DiagnosticRules() = %+v", got)
	}
	got[0].Sites[0] = "criteria"
	if DiagnosticRules()[0].Sites[0] != "calc" {
		t.Error("DiagnosticRules shares the registry's sites")
	}
}

// A site declares the bindings its author named (a calc rule's aliases);
// each must be one of its bindings.
func TestWithInputs(t *testing.T) {
	env := mustEnv(t, Float, WithInputs("value", "items"))
	if got := strings.Join(env.Inputs(), ","); got != "value,items" {
		t.Errorf("Inputs() = %q", got)
	}
	if len(boolEnv(t).Inputs()) != 0 {
		t.Error("an environment that declared no inputs has some")
	}
	if _, err := EnvOf[sampleEnv](Float, WithInputs("nope")); err == nil {
		t.Error("an input that is no binding was accepted")
	}
}

// A pass that panics is an engine bug, reported as one: no refine rule may
// explain it away with a fix for an expression that was never wrong.
func TestRefineLeavesEngineBugs(t *testing.T) {
	rs := []rule{testRule("flags-arith", triggerRefine, SeverityError, "flags", "items")}
	src := "flags + 1 > 0"
	parsed, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	bug := passRefusal(src, "typed checks", identNamed(parsed.Node, "flags"), "boom")
	if got := refine(rs, src, parsed.Node, boolEnv(t), 0, bug); got != bug || got.Code != "" {
		t.Fatalf("refine explained an engine bug: %+v", got)
	}
}

// Fixtures run with the registry in its real order: a refine rule whose
// positive fixture an earlier rule explains first fails, since production
// would answer with the earlier rule's code.
func TestFixturesRunInRegistryOrder(t *testing.T) {
	first := testRule("first-refine", triggerRefine, SeverityError, "flags", "n")
	second := testRule("second-refine", triggerRefine, SeverityError, "flags", "n")
	useRules(t, first, second)
	if err := second.checkFixtures(); err == nil {
		t.Error("a fixture the first rule answers passed for the second")
	}
	if err := first.checkFixtures(); err != nil {
		t.Errorf("the first rule's fixtures failed: %v", err)
	}
}

// A negative fixture may be an expression refused by something else: the
// rule leaving another refusal alone is exactly what it must pin.
func TestNegativeFixtureMayBeRefused(t *testing.T) {
	r := testRule("flags-arith", triggerRefine, SeverityError, "flags", "n")
	r.fixtures = append(r.fixtures, fixture{env: r.fixtures[0].env, source: "state + 1 > 0"})
	if err := r.checkFixtures(); err != nil {
		t.Fatalf("a negative fixture refused by Expr failed: %v", err)
	}
}

// A fixture compiled at a site the rule does not apply at proves nothing:
// the registry refuses it.
func TestFixtureSiteMustApply(t *testing.T) {
	r := testRule("calc-only", triggerLint, SeverityWarning, "prior", "", "calc")
	r.fixtures[1].env = MustEnv[sampleEnv](Bool, WithBuiltins(CriteriaBuiltins()...), WithSite("criteria"))
	if err := validRules([]rule{r}); err == nil {
		t.Error("a fixture at a site the rule does not apply at was accepted")
	}
}

// float-equality's advice is source that means what it says: the range is
// the literal's own precision (the value rounds to it), and a compound
// operand keeps its brackets.
func TestFloatEqualityAdvice(t *testing.T) {
	for src, want := range map[string]string{
		"value == 1.5":         "abs(value - 1.5) < 0.05",
		"value == 0.001":       "abs(value - 0.001) < 0.0005",
		"(value + 1) == 21.25": "abs((value + 1) - 21.25) < 0.005",
	} {
		diags, err := Check(src, criteriaFixture)
		if err != nil || len(diags) != 1 || !strings.Contains(diags[0].Message, want) {
			t.Errorf("%q: %+v (%v), want advice %q", src, diags, err, want)
		}
	}
}

// The docs table is the registry: every rule, in order, with its examples
// read from its fixtures (what it flags, with the fix's result, and what it
// leaves alone), so no example is hand-written.
func TestDiagnosticsFacts(t *testing.T) {
	raw, err := DiagnosticsFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Comment     string           `json:"//"`
		Diagnostics []DiagnosticRule `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Comment, "make gen") || len(doc.Diagnostics) != len(registry) {
		t.Fatalf("facts %q with %d rules, want the registry's %d", doc.Comment, len(doc.Diagnostics), len(registry))
	}
	for i, r := range registry {
		d := doc.Diagnostics[i]
		if d.Code != r.code || d.Severity != r.severity || d.Trigger != string(r.trigger) || d.Message != r.message ||
			len(d.Examples) != len(r.fixtures) || !slices.Equal(d.Sites, append([]string{}, r.sites...)) {
			t.Errorf("%s: facts %+v", r.code, d)
			continue
		}
		for j, f := range r.fixtures {
			e := d.Examples[j]
			if e.Source != f.source || e.Flags != f.want || e.Fixed != f.fixed {
				t.Errorf("%s example %d = %+v, want fixture %q", r.code, j, e, f.source)
			}
			// Each example says where it is written, from its environment.
			if e.Site != f.env.Site() || !slices.Equal(e.Inputs, append([]string{}, f.env.Inputs()...)) || e.Result != f.env.Result().String() {
				t.Errorf("%s example %q at %q %v %q, want its fixture's environment", r.code, f.source, e.Site, e.Inputs, e.Result)
			}
			// A case left alone because the engine refuses it says so.
			_, err := Check(f.source, f.env)
			if e.Refused != (!f.want && err != nil) {
				t.Errorf("%s example %q refused = %v, want %v", r.code, f.source, e.Refused, !f.want && err != nil)
			}
		}
	}
}

// The committed facts are what make gen writes.
func TestDiagnosticsFactsMatchCommitted(t *testing.T) {
	fresh, err := DiagnosticsFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../" + DiagnosticsFactsPath)
	if err != nil {
		t.Fatalf("read the committed diagnostics facts (run make gen and commit them): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Errorf("%s drifted from the diagnostics registry: run make gen and commit the result", DiagnosticsFactsPath)
	}
}
