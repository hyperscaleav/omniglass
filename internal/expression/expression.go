package expression

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// The caps every expression is held to. Each refusal wraps its sentinel.
const (
	// MaxSourceBytes is the longest source accepted, checked before parsing.
	MaxSourceBytes = 4096
	// MaxNodes is the most AST nodes a parsed expression may have.
	MaxNodes = 1000
	// MaxLiteralElements is the most elements an array or map literal may
	// have.
	MaxLiteralElements = 64
	// MaxPatternInsts is the most instructions a matches pattern may compile
	// to. RE2 is linear in the subject times the program, so a run is
	// charged len(subject) * instructions, and the cap keeps one match from
	// being worth a whole budget on a short subject.
	MaxPatternInsts = 256
	// MaxRunMemory is the ceiling on a site's allocation budget, in Expr's
	// memory units:
	// every element a range, list or map literal, or a building builtin
	// (map, filter, sort, repeat, ...) allocates.
	MaxRunMemory = 100_000
	// MaxRunIterations is the ceiling on a site's walk budget: the summed
	// length of every
	// collection or string a builtin or operator walks (budget.go). Expr has
	// no loop construct beyond these, so this bounds a run's time.
	MaxRunIterations = 1_000_000
	// MaxRunDuration is the ceiling on a site's wall-clock backstop, checked
	// where the walk budget is charged. It is the one limit that depends on the
	// machine (so not deterministic); the iteration budget is what bounds an
	// expression, and this is set well above the time that budget allows
	// (a run spending the whole budget measured about 50ms).
	MaxRunDuration = 250 * time.Millisecond
)

// DisabledBuiltins are the Expr builtins (as of v1.17.8) that are not pure,
// and so can never be vetted (every builtin outside the vetted registry is refused anyway):
// now reads the clock; date parses in the process's local zone and loads the
// timezone database; timezone loads the timezone database; duration builds a
// time.Duration whose methods compare against nothing but the clock it is
// meant for. A time an expression needs is bound by the caller as data.
func DisabledBuiltins() []string { return []string{"now", "date", "duration", "timezone"} }

var (
	ErrSourceTooLong   = fmt.Errorf("the expression is longer than %d bytes", MaxSourceBytes)
	ErrTooManyNodes    = fmt.Errorf("the expression has more than %d nodes", MaxNodes)
	ErrRunBudget       = errors.New("the expression exceeded its run budget")
	ErrPatternTooLarge = fmt.Errorf("a matches pattern compiles to more than %d regex instructions", MaxPatternInsts)
)

// Phase is where an expression failed: parse and type are compile-time
// refusals (an edit surface rejects the expression, it is never stored);
// limit is a cap, at compile or run time; run is an evaluation failure the
// caller degrades on.
type Phase string

const (
	PhaseParse Phase = "parse"
	PhaseType  Phase = "type"
	PhaseLimit Phase = "limit"
	PhaseRun   Phase = "run"
)

// Error is an expression diagnostic: the phase, the 1-based line and column
// of the fault (both 0 when it has no position, such as a length cap), and an
// operator-readable message.
type Error struct {
	Phase   Phase
	Line    int
	Column  int
	Message string
	// Unbound is the name the expression uses that nothing binds, when that
	// is the fault; empty otherwise.
	Unbound string
	err     error
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("%s: %s", e.Phase, e.Message)
	}
	return fmt.Sprintf("%s error at line %d, column %d: %s", e.Phase, e.Line, e.Column, e.Message)
}

func (e *Error) Unwrap() error { return e.err }

// fromExpr maps an Expr error to a diagnostic, its 0-based column to 1-based.
func fromExpr(phase Phase, err error) *Error {
	var fe *file.Error
	if !errors.As(err, &fe) {
		// An unpositioned compile error (the result type check) is about the
		// whole expression.
		e := &Error{Phase: phase, Message: err.Error(), err: err}
		if phase == PhaseType {
			e.Line, e.Column = 1, 1
		}
		return e
	}
	e := &Error{Phase: phase, Line: fe.Line, Column: fe.Column + 1, Message: fe.Message, err: err}
	if e.Line == 0 { // Expr left it unbound: the fault is the whole expression
		e.Line, e.Column = 1, 1
	}
	if phase == PhaseRun && (errors.Is(err, ErrRunBudget) || strings.Contains(fe.Message, "memory budget exceeded")) {
		e.Phase, e.Message, e.err = PhaseLimit, ErrRunBudget.Error(), ErrRunBudget
	}
	return e
}

// Program is a compiled expression, bound to the site it was compiled for.
// Safe for concurrent use.
type Program struct {
	env  Env
	prog *vm.Program
}

var (
	programs     = newCache(MaxCachedPrograms)
	compileCount atomic.Int64
)

// Compile parses source and type-checks it against env, returning a cached
// program when the same source was compiled for the same env shape.
func Compile(source string, env Env) (*Program, error) {
	if env.typ == nil {
		return nil, errors.New("compile needs a declared environment (EnvOf), not the zero Env")
	}
	if len(source) > MaxSourceBytes {
		return nil, &Error{Phase: PhaseLimit, Message: ErrSourceTooLong.Error(), err: ErrSourceTooLong}
	}
	key := cacheKey{source: source, env: env}
	if p, ok := programs.get(key); ok {
		return p, nil
	}
	src, renamed := oneListForms(source, env)
	if err := inspect(src, env, renamed); err != nil {
		return nil, err
	}
	p, err := compile(src, env)
	if err != nil {
		return nil, err
	}
	return programs.put(key, p), nil
}

// inspect parses source on its own and walks the tree: the node cap is ours,
// not Expr's default; a `$` name (Expr's dynamic $env, or the engine's own
// run plumbing) is refused, so the declared bindings are the whole namespace;
// a call is refused unless it is a builtin (Expr parses a builtin as its own
// node, so any call node is a method or a value being called); and a matches
// pattern must be a constant, so its regex compiles once, not per run. The
// allowlist is enforced here too: a builtin the site does not admit, the
// range operator, a predicate inside a predicate, and a long literal.
func inspect(source string, env Env, renamed map[int]bool) error {
	c := conf.CreateNew()
	c.MaxNodes = 0
	for name, f := range env.functions() {
		c.Functions[name] = f
	}
	tree, err := parser.ParseWithConfig(source, c)
	if err != nil {
		return fromExpr(PhaseParse, err)
	}
	w := walker{env: env, funcs: c.Functions, renamed: renamed}
	if err := guardPass(source, "inspect", func(p *pass) error {
		w.at = p
		ast.Walk(&tree.Node, &w)
		return nil
	}); err != nil {
		return err
	}
	if w.nodes > MaxNodes {
		return &Error{Phase: PhaseLimit, Message: ErrTooManyNodes.Error(), err: ErrTooManyNodes}
	}
	if w.fault != nil {
		e := fromExpr(w.phase, w.fault.Bind(tree.Source))
		if w.sentinel != nil {
			e.err = w.sentinel
		}
		return e
	}
	return nil
}

type walker struct {
	env      Env
	funcs    map[string]*builtin.Function // the engine functions the site installs
	renamed  map[int]bool                 // where oneListForms placed an engine name
	nodes    int
	fault    *file.Error // the first refusal met
	phase    Phase
	sentinel error
	at       *pass
}

func (w *walker) refuse(n ast.Node, format string, args ...any) {
	if w.fault == nil {
		w.fault = &file.Error{Location: n.Location(), Message: fmt.Sprintf(format, args...)}
		w.phase = PhaseType
	}
}

func (w *walker) limit(n ast.Node, sentinel error) {
	if w.fault == nil {
		w.fault = &file.Error{Location: n.Location(), Message: sentinel.Error()}
		w.phase, w.sentinel = PhaseLimit, sentinel
	}
}

func (w *walker) Visit(node *ast.Node) {
	w.nodes++
	w.at.at = *node
	switch n := (*node).(type) {
	case *ast.IdentifierNode:
		if strings.HasPrefix(n.Value, "$") && !w.renamed[n.Location().From] {
			w.refuse(n, "unknown name %s", n.Value)
		}
	case *ast.CallNode:
		name := "this value"
		switch c := n.Callee.(type) {
		case *ast.IdentifierNode:
			// An engine function the site admits (lists.go) is a call it
			// may make; a one-list form only where the rename put it.
			if _, ok := w.funcs[c.Value]; ok && (!strings.HasPrefix(c.Value, "$") || w.renamed[c.Location().From]) {
				return
			}
			name = c.Value
		case *ast.MemberNode:
			if s, ok := c.Property.(*ast.StringNode); ok {
				name = s.Value
			}
		}
		w.refuse(n, "%s is not a builtin function; an expression calls builtins only", name)
	case *ast.BinaryNode:
		if n.Operator == "matches" {
			pat, ok := n.Right.(*ast.StringNode)
			if !ok {
				w.refuse(n.Right, "matches needs a constant pattern (a quoted string), not one computed when it runs")
			} else if insts, err := patternInsts(pat.Value); err == nil && insts > MaxPatternInsts {
				w.limit(n.Right, ErrPatternTooLarge)
			}
		}
		if n.Operator == ".." {
			w.refuse(n, "the range operator .. is not available in an expression")
		}
	case *ast.BuiltinNode:
		if !w.env.admits(n.Name) {
			allowed := w.env.builtins
			if allowed == "" {
				allowed = "none"
			}
			w.refuse(n, "%s is not available here (this site admits: %s)", n.Name, strings.ReplaceAll(allowed, ",", ", "))
		}
	case *ast.PredicateNode:
		var inner nested
		ast.Walk(&n.Node, &inner)
		if inner.at != nil {
			w.refuse(inner.at, "a predicate inside a predicate is not allowed: its cost is the product of both lists")
		}
	case *ast.ArrayNode:
		if len(n.Nodes) > MaxLiteralElements {
			w.refuse(n, "a list literal has at most %d elements", MaxLiteralElements)
		}
	case *ast.MapNode:
		if len(n.Pairs) > MaxLiteralElements {
			w.refuse(n, "a map literal has at most %d entries", MaxLiteralElements)
		}
	}
}

// nested finds a predicate inside a predicate's body.
type nested struct{ at ast.Node }

func (f *nested) Visit(node *ast.Node) {
	if p, ok := (*node).(*ast.PredicateNode); ok && f.at == nil {
		f.at = p
	}
}

// typedChecks refuses, on the checked tree where every node has its type,
// what the parse alone cannot see:
//
//   - an array, map or unknown argument to a builtin admitted for scalars
//     only (min and max would otherwise walk it natively);
//   - count without a predicate over a list that is not of bools;
//   - ==, != and in on anything but scalars and lists of scalars (and a
//     scalar key in a map): a struct, a map, or a list of lists falls to a
//     deep comparison that its charge does not cover.
func typedChecks(source string, root ast.Node) error {
	var fault *file.Error
	fail := func(n ast.Node, format string, args ...any) {
		if fault == nil {
			fault = &file.Error{Location: n.Location(), Message: fmt.Sprintf(format, args...)}
		}
	}
	if err := guardPass(source, "typed checks", func(p *pass) error {
		ast.Walk(&root, visitFunc(func(node *ast.Node) {
			p.at = *node
			typedCheck(*node, fail)
		}))
		return nil
	}); err != nil {
		return err
	}
	if fault != nil {
		return fromExpr(PhaseType, fault.Bind(file.NewSource(source)))
	}
	return nil
}

// typedCheck is typedChecks on one node.
func typedCheck(node ast.Node, fail func(n ast.Node, format string, args ...any)) {
	listCheck(node, fail)
	switch n := node.(type) {
	case *ast.BuiltinNode:
		if scalarOnly[n.Name] {
			for _, arg := range n.Arguments {
				if !scalar(arg.Type()) {
					fail(arg, "%s takes numbers only here, not a list", n.Name)
				}
			}
		}
		if n.Name == "count" && len(n.Arguments) == 1 {
			if t := n.Arguments[0].Type(); t == nil || (t.Kind() != reflect.Slice && t.Kind() != reflect.Array) || t.Elem().Kind() != reflect.Bool {
				fail(n.Arguments[0], "count without a predicate counts true elements, so its list must be of bools; give a predicate, or count values with len")
			}
		}
	case *ast.BinaryNode:
		switch n.Operator {
		case "==", "!=":
			if !flat(n.Left) || !flat(n.Right) {
				fail(n, "%s compares scalars and lists of scalars only, not a struct, a map, or a list of lists", n.Operator)
			}
		case "in":
			if !scalar(unwrapCharge(n.Left).Type()) || !(flat(n.Right) && !scalar(unwrapCharge(n.Right).Type()) || scalarKeyed(n.Right)) {
				fail(n, "in looks for a scalar in a list of scalars or among a map's keys")
			}
		}
	}
}

// scalar is a type compared in constant time per byte: a bool, a number, a
// string, or nil (a nil literal), through a pointer.
func scalar(t reflect.Type) bool {
	if t == nil {
		return true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	case reflect.Pointer:
		return scalar(t.Elem())
	}
	return false
}

// flat is a scalar, or a list whose elements are scalars: a typed list, or a
// list literal of scalar elements (Expr types a literal as []any). The tree
// is unoptimized (compile turns Expr's optimizer off), so a constant literal
// is still its ArrayNode, not a folded ConstantNode.
func flat(n ast.Node) bool {
	n = unwrapCharge(n)
	if a, ok := n.(*ast.ArrayNode); ok {
		for _, el := range a.Nodes {
			if !scalar(el.Type()) {
				return false
			}
		}
		return true
	}
	t := n.Type()
	if scalar(t) {
		return true
	}
	return (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) && scalar(t.Elem())
}

func scalarKeyed(n ast.Node) bool {
	n = unwrapCharge(n)
	t := n.Type()
	return t != nil && t.Kind() == reflect.Map && scalar(t.Key())
}

// unwrapCharge is the node a charge wraps (budget.go), or n itself.
func unwrapCharge(n ast.Node) ast.Node {
	if call, ok := n.(*ast.CallNode); ok {
		if id, ok := call.Callee.(*ast.IdentifierNode); ok && isCharge(id.Value) {
			return call.Arguments[1]
		}
	}
	return n
}

type visitFunc func(*ast.Node)

func (f visitFunc) Visit(n *ast.Node) { f(n) }

func compile(source string, env Env) (*Program, error) {
	compileCount.Add(1)
	var prog *vm.Program
	err := guardPass(source, "Expr checker", func(p *pass) error {
		opts := []expr.Option{
			expr.Env(reflect.New(env.run).Elem().Interface()), chargeOption(p), functionsOption(env), expr.DisableAllBuiltins(),
			// Expr's optimizer is off: in v1.17.8 it rewrites filter(list, p)[0]
			// and [-1] into find and findLast and drops the struct's field table
			// from the type it copies onto the rewrite, so a field read on the
			// result (filter(list, p)[0].x) panics in Expr's compiler. Tracked in
			// #1006; this option comes off when upstream is fixed.
			expr.Optimize(false),
		}
		for _, name := range env.exprBuiltins() {
			opts = append(opts, expr.EnableBuiltin(name))
		}
		if env.admitsList("sum") {
			opts = append(opts, expr.EnableBuiltin("sum"))
		}
		switch env.result {
		case Bool:
			opts = append(opts, expr.AsBool())
		case Float:
			opts = append(opts, expr.AsFloat64())
		case Int:
			opts = append(opts, expr.AsInt())
		case String:
			opts = append(opts, expr.AsKind(reflect.String))
		}
		var err error
		prog, err = expr.Compile(source, opts...)
		if refused := fromCompilerPanic(err); refused != nil {
			return refused
		}
		return err
	})
	var refused *Error
	if errors.As(err, &refused) {
		return nil, refused
	}
	if err != nil {
		e := fromExpr(PhaseType, err)
		e.Message = env.speak(e.Message)
		named(e)
		return nil, e
	}
	if err := typedChecks(source, prog.Node()); err != nil {
		return nil, err
	}
	if env.result != Any {
		if t := prog.Node().Type(); t == nil || t.Kind() == reflect.Interface {
			return nil, &Error{Phase: PhaseType, Line: 1, Column: 1, Message: fmt.Sprintf(
				"expected %s, but the expression's type is only known when it runs; compare or convert it", env.result)}
		}
	}
	return &Program{env: env, prog: prog}, nil
}

// Run evaluates the program against one prepared input: a value of the
// site's env struct type, or a non-nil pointer to one.
func (p *Program) Run(input any) (any, error) {
	v := reflect.ValueOf(input)
	if v.Kind() == reflect.Pointer && v.Type().Elem() == p.env.typ && !v.IsNil() {
		v = v.Elem()
	}
	if !v.IsValid() || v.Type() != p.env.typ {
		return nil, &Error{Phase: PhaseRun, Message: fmt.Sprintf("the input is %T, not the site's %s", input, p.env.typ)}
	}
	w := reflect.New(p.env.run).Elem()
	w.Field(0).Set(v)
	w.Field(1).Set(reflect.ValueOf(newMeter(p.env.budget)))
	machine := vm.VM{MemoryBudget: uint(p.env.budget.Memory)}
	out, err := machine.Run(p.prog, w.Interface())
	if err != nil {
		return nil, fromExpr(PhaseRun, err)
	}
	// A float site's int result widens (sum over an empty list is Expr's
	// int 0, though the list is of float64).
	if i, ok := out.(int); ok && p.env.result == Float {
		out = float64(i)
	}
	if !resultIs(p.env.result, out) {
		return nil, &Error{Phase: PhaseRun, Message: fmt.Sprintf("the expression produced %T, not %s", out, p.env.result)}
	}
	return out, nil
}

func resultIs(r Result, out any) bool {
	switch r {
	case Bool:
		_, ok := out.(bool)
		return ok
	case Float:
		_, ok := out.(float64)
		return ok
	case Int:
		_, ok := out.(int)
		return ok
	case String:
		_, ok := out.(string)
		return ok
	}
	return true
}

// Check reports whether source compiles against env, with the diagnostics
// Compile would give, without touching the shared program cache: a caller
// that only validates (POST /expressions:validate) cannot evict the
// programs the engine runs on, however many sources it cycles through.
func Check(source string, env Env) error {
	if env.typ == nil {
		return errors.New("compile needs a declared environment (EnvOf), not the zero Env")
	}
	if len(source) > MaxSourceBytes {
		return &Error{Phase: PhaseLimit, Message: ErrSourceTooLong.Error(), err: ErrSourceTooLong}
	}
	src, renamed := oneListForms(source, env)
	if err := inspect(src, env, renamed); err != nil {
		return err
	}
	_, err := compile(src, env)
	return err
}

// named reads the two refusals an editor acts on from Expr's checker: a name
// nothing binds (Unbound, so an editor can offer to bind it), and a let that
// would shadow a binding, said in the author's terms.
func named(e *Error) {
	if name, ok := strings.CutPrefix(e.Message, "unknown name "); ok && !strings.ContainsAny(name, " $") {
		e.Unbound = name
		return
	}
	if rest, ok := strings.CutPrefix(e.Message, "cannot redeclare "); ok {
		name := rest[strings.LastIndex(rest, " ")+1:]
		e.Message = fmt.Sprintf("%s is already a name here; a let may not shadow it", name)
	}
}

// functionsOption installs a site's engine functions (lists.go).
func functionsOption(env Env) expr.Option {
	return func(c *conf.Config) {
		for name, f := range env.functions() {
			c.Functions[name] = f
		}
	}
}
