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
	// MaxRunMemory is one run's allocation budget in Expr's memory units:
	// every element a range, list or map literal, or a building builtin
	// (map, filter, sort, repeat, ...) allocates.
	MaxRunMemory = 100_000
	// MaxRunIterations is one run's work budget: the summed length of every
	// collection or string a builtin or operator walks (budget.go). Expr has
	// no loop construct beyond these, so this bounds a run's time.
	MaxRunIterations = 1_000_000
	// MaxRunDuration is a wall-clock backstop on one run, checked wherever
	// the iteration budget is charged. It is the one limit that depends on the
	// machine (so not deterministic); the iteration budget is what bounds an
	// expression, and this is set well above the time that budget allows
	// (a run spending the whole budget measured about 50ms).
	MaxRunDuration = 250 * time.Millisecond
)

// DisabledBuiltins are the Expr builtins (as of v1.17.8) that are not pure:
// now reads the clock; date parses in the process's local zone and loads the
// timezone database; timezone loads the timezone database; duration builds a
// time.Duration whose methods compare against nothing but the clock it is
// meant for. A time an expression needs is bound by the caller as data.
var DisabledBuiltins = []string{"now", "date", "duration", "timezone"}

var (
	ErrSourceTooLong = fmt.Errorf("the expression is longer than %d bytes", MaxSourceBytes)
	ErrTooManyNodes  = fmt.Errorf("the expression has more than %d nodes", MaxNodes)
	ErrRunBudget     = fmt.Errorf("the expression exceeded its run budget (%d allocated or %d iterated)", MaxRunMemory, MaxRunIterations)
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
	key := cacheKey{source: source, env: env.typ, result: env.result}
	if p, ok := programs.get(key); ok {
		return p, nil
	}
	if err := inspect(source); err != nil {
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
// pattern must be a constant, so its regex compiles once, not per run.
func inspect(source string) error {
	c := conf.CreateNew()
	c.MaxNodes = 0
	tree, err := parser.ParseWithConfig(source, c)
	if err != nil {
		return fromExpr(PhaseParse, err)
	}
	var w walker
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
	}
}

func compile(source string, env Env) (*Program, error) {
	compileCount.Add(1)
	opts := []expr.Option{expr.Env(reflect.New(env.run).Elem().Interface()), chargeOption()}
	for _, name := range DisabledBuiltins {
		opts = append(opts, expr.DisableBuiltin(name))
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
	w.Field(1).Set(reflect.ValueOf(newMeter()))
	machine := vm.VM{MemoryBudget: MaxRunMemory}
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
