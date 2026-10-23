package expression

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
)

// The diagnostics registry (#1021): each common mistake is one declared rule
// with a stable code, a severity, the sites it applies to, a trigger, a pure
// matcher over the typed tree, a message, an optional fix, and the fixtures
// that hold it to its word. Compile and Check run a site's rules once per
// compile:
//
//   - a refine rule runs when the engine refuses an expression and recognises
//     the shape behind the refusal, replacing the refusal's message with a
//     clearer one at the place the mistake is (always an error: it explains
//     a refusal, it never makes one);
//   - a lint rule runs on an expression that compiles, on its checked tree,
//     and flags a likely mistake: an error refuses it, a warning or a hint
//     rides along with the program.
//
// A rule is added as patterns show up: a registry entry and its fixtures. A
// rule without fixtures, or whose fixtures disagree with it, fails the build
// (validRules at init, checkFixtures in the tests).

// Severity is how much a diagnostic matters: an error refuses the
// expression; a warning (probably wrong) and a hint (could be simpler) ride
// along with one that compiles and never refuse it.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityHint    Severity = "hint"
)

// trigger is when a rule runs.
type trigger string

const (
	// triggerRefine runs on a refusal, to explain it.
	triggerRefine trigger = "refine"
	// triggerLint runs on an expression that compiles.
	triggerLint trigger = "lint"
)

// Fix is one text edit an editor offers as a quick fix: replace the source
// from Line:Column up to (not including) EndLine:EndColumn with Replacement.
// Lines and columns are 1-based and count characters, as a diagnostic's do.
type Fix struct {
	Title       string
	Line        int
	Column      int
	EndLine     int
	EndColumn   int
	Replacement string
}

// Diagnostic is a finding that rides along with an expression that compiles:
// a warning or a hint, with its rule's code, its position (Line 0 when it
// has none, such as an input the expression never names), its message, and
// its fix when the rule offers one.
type Diagnostic struct {
	Code     string
	Severity Severity
	Line     int
	Column   int
	Message  string
	Fix      *Fix
}

// rule is one registered diagnostic.
type rule struct {
	code     string
	severity Severity
	trigger  trigger
	// sites are the site names it applies to; none, every site.
	sites []string
	// message is what it says, and what the docs say it says; a finding may
	// say it in its own terms (naming the input it is about).
	message string
	match   func(s *subject) []finding
	// fixtures are the examples it must give its diagnostic for and must
	// not, each compiled against a site-shaped environment.
	fixtures []fixture
}

// finding is one place a rule matched.
type finding struct {
	// from is the rune offset the diagnostic points at; -1 for none.
	from    int
	message string // empty: the rule's
	fix     *edit
}

// edit is a fix in rune offsets of the author's source.
type edit struct {
	title       string
	from, to    int
	replacement string
}

// fixture is one example a rule is held to.
type fixture struct {
	env    Env
	source string
	// want is whether the rule gives its diagnostic for source.
	want bool
	// column is where on line 1 it points (1-based; 0 for no position).
	column int
	// fixed is the source once the rule's fix is applied: it must compile,
	// without this rule's diagnostic. Empty when the rule offers no fix.
	fixed string
}

// subject is what a matcher reads: the author's source, the checked tree
// (typed as far as the checker got, for a refusal), the site, and for a
// refine rule the refusal.
type subject struct {
	source string
	root   ast.Node
	env    Env
	fault  *fault
	toks   []lexer.Token
}

// fault is a refusal a refine rule may explain: where it points (a rune
// offset, -1 for the whole expression) and what it said.
type fault struct {
	from    int
	message string
}

// registry is every rule, in the order they run (the first refine rule that
// recognises a refusal explains it).
var registry = mustRules()

func mustRules(rs ...rule) []rule {
	if err := validRules(rs); err != nil {
		panic(err)
	}
	return rs
}

var codeShape = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// validRules refuses a registry it cannot stand behind.
func validRules(rs []rule) error {
	seen := map[string]bool{}
	for _, r := range rs {
		switch {
		case !codeShape.MatchString(r.code):
			return fmt.Errorf("diagnostic code %q is not lower-kebab-case", r.code)
		case seen[r.code]:
			return fmt.Errorf("diagnostic code %q is registered twice", r.code)
		case r.severity != SeverityError && r.severity != SeverityWarning && r.severity != SeverityHint:
			return fmt.Errorf("%s: unknown severity %q", r.code, r.severity)
		case r.trigger != triggerRefine && r.trigger != triggerLint:
			return fmt.Errorf("%s: unknown trigger %q", r.code, r.trigger)
		case r.trigger == triggerRefine && r.severity != SeverityError:
			return fmt.Errorf("%s: a refine rule explains a refusal, so it is an error", r.code)
		case r.match == nil:
			return fmt.Errorf("%s: no matcher", r.code)
		case r.message == "":
			return fmt.Errorf("%s: no message", r.code)
		}
		seen[r.code] = true
		var gives, spares bool
		for _, f := range r.fixtures {
			if f.env.typ == nil {
				return fmt.Errorf("%s: fixture %q has no environment", r.code, f.source)
			}
			gives = gives || f.want
			spares = spares || !f.want
		}
		if !gives || !spares {
			return fmt.Errorf("%s: a rule needs fixtures it gives its diagnostic for and fixtures it does not", r.code)
		}
	}
	return nil
}

// appliesAt reports whether the rule runs at a site.
func (r rule) appliesAt(site string) bool {
	return len(r.sites) == 0 || slices.Contains(r.sites, site)
}

// run is the rule's matcher, a panic in it a bug in the rule: no findings,
// so the expression is judged as if the rule were absent.
func (r rule) run(s *subject) (out []finding) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	return r.match(s)
}

// refine explains a refusal (e, pointing at from) with the first refine
// rule at the site that recognises it, or returns e unchanged.
func refine(rs []rule, source string, root ast.Node, env Env, from int, e *Error) *Error {
	if root == nil || e.Phase != PhaseType {
		return e
	}
	s := &subject{source: source, root: root, env: env, fault: &fault{from: from, message: e.Message}}
	for _, r := range rs {
		if r.trigger != triggerRefine || !r.appliesAt(env.site) {
			continue
		}
		fs := r.run(s)
		if len(fs) == 0 {
			continue
		}
		return r.refusal(s, fs[0], e)
	}
	return e
}

// lint runs the site's lint rules on a checked tree: the first error finding
// refuses, the warnings and hints are returned in source order.
func lint(rs []rule, source string, root ast.Node, env Env) ([]Diagnostic, *Error) {
	s := &subject{source: source, root: root, env: env}
	var out []Diagnostic
	for _, r := range rs {
		if r.trigger != triggerLint || !r.appliesAt(env.site) {
			continue
		}
		for _, f := range r.run(s) {
			if r.severity == SeverityError {
				return nil, r.refusal(s, f, nil)
			}
			d := Diagnostic{Code: r.code, Severity: r.severity, Message: r.say(f), Fix: s.fix(f.fix)}
			d.Line, d.Column = s.position(f.from)
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if a.Column != b.Column {
			return a.Column - b.Column
		}
		return strings.Compare(a.Code, b.Code)
	})
	return out, nil
}

func (r rule) say(f finding) string {
	if f.message != "" {
		return f.message
	}
	return r.message
}

// refusal is a finding as the refusal it makes, wrapping what it explains.
func (r rule) refusal(s *subject, f finding, explains *Error) *Error {
	e := &Error{Phase: PhaseType, Message: r.say(f), Code: r.code, Fix: s.fix(f.fix)}
	e.Line, e.Column = s.position(f.from)
	if f.from < 0 {
		e.Line, e.Column = 1, 1
	}
	if explains != nil {
		e.err = explains
	} else {
		e.err = errors.New(e.Message)
	}
	return e
}

// position is a rune offset's 1-based line and column; 0, 0 for none.
func (s *subject) position(from int) (int, int) {
	if from < 0 {
		return 0, 0
	}
	fe := (&file.Error{Location: file.Location{From: from}}).Bind(file.NewSource(s.source))
	return fe.Line, fe.Column + 1
}

func (s *subject) fix(e *edit) *Fix {
	if e == nil {
		return nil
	}
	f := &Fix{Title: e.title, Replacement: e.replacement}
	f.Line, f.Column = s.position(e.from)
	f.EndLine, f.EndColumn = s.position(e.to)
	return f
}

// tokens are the author's source lexed, once.
func (s *subject) tokens() []lexer.Token {
	if s.toks == nil {
		s.toks, _ = lexer.Lex(file.NewSource(s.source))
	}
	return s.toks
}

// text is the author's source between two rune offsets.
func (s *subject) text(from, to int) string {
	r := []rune(s.source)
	from, to = max(0, min(from, len(r))), max(0, min(to, len(r)))
	return string(r[from:to])
}

// span is the rune range a node's source covers: every node under it (the
// engine's charges seen through), and a call through its closing bracket.
// A node's own location is one token (a call's name, an operator), which is
// where a diagnostic points; a fix replaces a span.
func (s *subject) span(n ast.Node) (int, int) {
	from, to := -1, -1
	ast.Walk(&n, visitFunc(func(node *ast.Node) {
		switch m := (*node).(type) {
		case *ast.IdentifierNode:
			if isCharge(m.Value) || m.Value == meterField {
				return
			}
		case *ast.CallNode:
			if id, ok := m.Callee.(*ast.IdentifierNode); ok && isCharge(id.Value) {
				return
			}
		}
		loc := (*node).Location()
		end := loc.To
		switch (*node).(type) {
		case *ast.BuiltinNode, *ast.CallNode:
			end = s.closing(loc.From, end)
		}
		if from < 0 || loc.From < from {
			from = loc.From
		}
		if end > to {
			to = end
		}
	}))
	return from, to
}

// closing is the offset just past the bracket that closes the call whose
// name starts at from; end when the source does not say.
func (s *subject) closing(from, end int) int {
	toks := s.tokens()
	for i, t := range toks {
		if t.From != from {
			continue
		}
		if i+1 >= len(toks) || !toks[i+1].Is(lexer.Bracket, "(") {
			return end
		}
		depth := 0
		for _, u := range toks[i+1:] {
			if u.Kind != lexer.Bracket {
				continue
			}
			switch u.Value {
			case "(", "[", "{":
				depth++
			default:
				depth--
				if depth == 0 {
					return u.To
				}
			}
		}
		return end
	}
	return end
}

// near reports whether the refusal is about n: it points inside n's source,
// or at a construct n is an operand or argument of (sum(...) > 0 is refused
// at the >).
func (s *subject) near(n ast.Node) bool {
	if s.fault == nil {
		return false
	}
	from, to := s.span(n)
	if s.fault.from >= from && s.fault.from < to {
		return true
	}
	found := false
	root := s.root
	ast.Walk(&root, visitFunc(func(node *ast.Node) {
		if found || (*node).Location().From != s.fault.from {
			return
		}
		for _, c := range operands(*node) {
			if unwrapCharge(c) == n {
				found = true
			}
		}
	}))
	return found
}

// operands are a node's direct operands or arguments.
func operands(n ast.Node) []ast.Node {
	switch m := n.(type) {
	case *ast.BinaryNode:
		return []ast.Node{m.Left, m.Right}
	case *ast.UnaryNode:
		return []ast.Node{m.Node}
	case *ast.BuiltinNode:
		return m.Arguments
	case *ast.CallNode:
		return m.Arguments
	case *ast.MemberNode:
		return []ast.Node{m.Node}
	case *ast.ConditionalNode:
		return []ast.Node{m.Cond, m.Exp1, m.Exp2}
	}
	return nil
}

// checkFixtures holds the rule to its examples: each compiled against its
// environment with the registry's other rules and this one in force, the rule's diagnostic present
// where it must be (at the column given) and absent where it must not, and
// its fix yielding the fixed source, which compiles without it.
func (r rule) checkFixtures() error {
	for _, f := range r.fixtures {
		got, err := r.findIn(f.source, f.env)
		if err != nil {
			return fmt.Errorf("fixture %q: %v", f.source, err)
		}
		if !f.want {
			if got != nil {
				return fmt.Errorf("fixture %q: gave %s, which it must not", f.source, r.code)
			}
			continue
		}
		if got == nil {
			return fmt.Errorf("fixture %q: did not give %s", f.source, r.code)
		}
		if got.Column != f.column || (f.column > 0 && got.Line != 1) {
			return fmt.Errorf("fixture %q: %s at %d:%d, want 1:%d", f.source, r.code, got.Line, got.Column, f.column)
		}
		if (got.Fix == nil) != (f.fixed == "") {
			return fmt.Errorf("fixture %q: fix %+v, want the fixed source %q", f.source, got.Fix, f.fixed)
		}
		if got.Fix == nil {
			continue
		}
		fixed, err := Apply(f.source, *got.Fix)
		if err != nil {
			return fmt.Errorf("fixture %q: %v", f.source, err)
		}
		if fixed != f.fixed {
			return fmt.Errorf("fixture %q: the fix gives %q, want %q", f.source, fixed, f.fixed)
		}
		again, err := r.findIn(fixed, f.env)
		if err != nil {
			return fmt.Errorf("fixture %q: the fixed %q: %v", f.source, fixed, err)
		}
		if again != nil {
			return fmt.Errorf("fixture %q: the fixed %q still gives %s", f.source, fixed, r.code)
		}
	}
	return nil
}

// findIn checks source and returns the rule's diagnostic in it, or nil; an
// error when source is refused by anything but this rule.
func (r rule) findIn(source string, env Env) (*Diagnostic, error) {
	rs := []rule{r}
	for _, o := range registry {
		if o.code != r.code {
			rs = append(rs, o)
		}
	}
	diags, err := check(source, env, rs)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == r.code {
			return &Diagnostic{Code: e.Code, Severity: SeverityError, Line: e.Line, Column: e.Column, Message: e.Message, Fix: e.Fix}, nil
		}
		return nil, fmt.Errorf("refused: %v", err)
	}
	for _, d := range diags {
		if d.Code == r.code {
			return &d, nil
		}
	}
	return nil, nil
}

// Apply is source with one fix applied (what an editor's quick fix does).
func Apply(source string, f Fix) (string, error) {
	from, ok1 := offset(source, f.Line, f.Column)
	to, ok2 := offset(source, f.EndLine, f.EndColumn)
	if !ok1 || !ok2 || to < from {
		return "", fmt.Errorf("the fix's range %d:%d to %d:%d is not in the source", f.Line, f.Column, f.EndLine, f.EndColumn)
	}
	r := []rune(source)
	return string(r[:from]) + f.Replacement + string(r[to:]), nil
}

// offset is the rune offset of a 1-based line and column.
func offset(source string, line, column int) (int, bool) {
	if line < 1 || column < 1 {
		return 0, false
	}
	l, c := 1, 1
	for i, r := range []rune(source) {
		if l == line && c == column {
			return i, true
		}
		if r == '\n' {
			if l == line {
				return 0, false
			}
			l, c = l+1, 1
		} else {
			c++
		}
	}
	return len([]rune(source)), l == line && c == column
}

// DiagnosticRule is one registered rule as a caller (the docs, a test that
// sites exist) reads it.
type DiagnosticRule struct {
	Code     string
	Severity Severity
	// Trigger is refine (explains a refusal) or lint (flags an expression
	// that compiles).
	Trigger string
	// Sites are the sites it applies at; none, every site.
	Sites   []string
	Message string
}

// DiagnosticRules lists every registered rule, in the order they run; a
// fresh copy each call.
func DiagnosticRules() []DiagnosticRule {
	out := make([]DiagnosticRule, len(registry))
	for i, r := range registry {
		out[i] = DiagnosticRule{Code: r.code, Severity: r.severity, Trigger: string(r.trigger), Sites: slices.Clone(r.sites), Message: r.message}
	}
	return out
}
