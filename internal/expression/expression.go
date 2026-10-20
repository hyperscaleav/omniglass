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
var DisabledBuiltins = []string{"now", "date", "duration", "timezone"}

var (
	ErrSourceTooLong = fmt.Errorf("the expression is longer than %d bytes", MaxSourceBytes)
	ErrTooManyNodes  = fmt.Errorf("the expression has more than %d nodes", MaxNodes)
	ErrRunBudget     = errors.New("the expression exceeded its run budget")
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
	if err := inspect(source, env); err != nil {
		return nil, err
	}
	p, err := compile(source, env)
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
func inspect(source string, env Env) error {
	c := conf.CreateNew()
	c.MaxNodes = 0
	tree, err := parser.ParseWithConfig(source, c)
	if err != nil {
		return fromExpr(PhaseParse, err)
	}
	w := walker{env: env}
	ast.Walk(&tree.Node, &w)
	if w.nodes > MaxNodes {
		return &Error{Phase: PhaseLimit, Message: ErrTooManyNodes.Error(), err: ErrTooManyNodes}
	}
	if w.fault != nil {
		return fromExpr(PhaseType, w.fault.Bind(tree.Source))
	}
	return nil
}

type walker struct {
	env   Env
	nodes int
	fault *file.Error // the first refusal met
}

func (w *walker) refuse(n ast.Node, format string, args ...any) {
	if w.fault == nil {
		w.fault = &file.Error{Location: n.Location(), Message: fmt.Sprintf(format, args...)}
	}
}

func (w *walker) Visit(node *ast.Node) {
	w.nodes++
	switch n := (*node).(type) {
	case *ast.IdentifierNode:
		if strings.HasPrefix(n.Value, "$") {
			w.refuse(n, "unknown name %s", n.Value)
		}
	case *ast.CallNode:
		name := "this value"
		switch c := n.Callee.(type) {
		case *ast.IdentifierNode:
			name = c.Value
		case *ast.MemberNode:
			if s, ok := c.Property.(*ast.StringNode); ok {
				name = s.Value
			}
		}
		w.refuse(n, "%s is not a builtin function; an expression calls builtins only", name)
	case *ast.BinaryNode:
		if _, ok := n.Right.(*ast.StringNode); n.Operator == "matches" && !ok {
			w.refuse(n.Right, "matches needs a constant pattern (a quoted string), not one computed when it runs")
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

// scalarArgs refuses an array, map or unknown argument to a builtin that is
// admitted for scalars only (min and max would otherwise walk it natively).
// It runs on the checked tree, where every argument has its type.
func scalarArgs(source string, root ast.Node) error {
	var fault *file.Error
	ast.Walk(&root, visitFunc(func(node *ast.Node) {
		b, ok := (*node).(*ast.BuiltinNode)
		if !ok || !scalarOnly[b.Name] || fault != nil {
			return
		}
		for _, arg := range b.Arguments {
			t := arg.Type()
			if t == nil || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map || t.Kind() == reflect.Interface {
				fault = &file.Error{Location: arg.Location(), Message: fmt.Sprintf("%s takes numbers only here, not a list", b.Name)}
				return
			}
		}
	}))
	if fault != nil {
		return fromExpr(PhaseType, fault.Bind(file.NewSource(source)))
	}
	return nil
}

type visitFunc func(*ast.Node)

func (f visitFunc) Visit(n *ast.Node) { f(n) }

func compile(source string, env Env) (*Program, error) {
	compileCount.Add(1)
	opts := []expr.Option{expr.Env(reflect.New(env.run).Elem().Interface()), chargeOption(), expr.DisableAllBuiltins()}
	for _, name := range env.Builtins() {
		opts = append(opts, expr.EnableBuiltin(name))
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
	prog, err := expr.Compile(source, opts...)
	if err != nil {
		return nil, fromExpr(PhaseType, err)
	}
	if err := scalarArgs(source, prog.Node()); err != nil {
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
