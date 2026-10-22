package expression

import (
	"errors"
	"fmt"
	"reflect"
	"regexp/syntax"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/checker/nature"
	"github.com/expr-lang/expr/conf"
)

// Expr's run budget (MaxRunMemory) counts what a run allocates, not what it
// walks: all(items, all(items, ...)) over a bound list, a string compared or
// searched in a loop, or a string doubled by + charge it nothing. So the
// program is patched (charger) to route every value a walk reads through a
// charge on the run's meter, which adds the value's length (at least one) and
// refuses past the site's Budget.Iterations:
//
//   - every argument of a builtin that walks (all but len, type, get, first,
//     last), except its predicate;
//   - both operands of +, the comparisons, contains, startsWith, endsWith;
//   - the subject of matches, times the pattern's compiled size (RE2 is
//     linear in both; the pattern is a constant, compiled once, and capped
//     at MaxPatternInsts);
//   - the collection on the right of `in`, unless it is a map (a lookup).
//
// The charge returns its argument unchanged, so types and results do not
// move. The meter is per run, carried in a hidden field of the struct the
// program is compiled against (the site's env embedded, plus the meter), so
// a cached program shared across goroutines still counts each run on its own.
//
// Charging bounds only what passes through a charge. A Go-native builtin
// runs to completion once called (repeat, replace, join, uniq, toJSON and
// flatten can each do work far beyond their charged inputs), so the real
// bound is the allowlist (vetted, env.go): a site admits only builtins whose cost is
// linear in what they are charged for and whose output is no larger than
// their inputs, with no range operator, no nested predicate, and short
// literals.
//
// The meter also holds the site's wall-clock deadline (Budget.Deadline) from
// the start of the run, checked at charge points. It is a backstop for
// charged work only: it cannot pre-empt a native call, which is why the
// allowlist exists, and it is the one limit that depends on the machine, so
// the deterministic iteration budget is what bounds an expression.

const (
	chargeFn      = "$charge"      // charges a collection, map, or string
	chargeSeqFn   = "$chargeSeq"   // as chargeFn, but a map is free (an `in` lookup)
	chargeTimesFn = "$chargeTimes" // as chargeFn, times a constant (a regex's size)
	chargeDeepFn  = "$chargeDeep"  // as chargeFn, plus each string element's bytes
	meterField    = "$meter"
)

func isCharge(name string) bool {
	return name == chargeFn || name == chargeSeqFn || name == chargeTimesFn || name == chargeDeepFn
}

// patternInsts is the size of the program RE2 runs for a matches pattern
// (Expr compiles it with the same Perl syntax).
func patternInsts(pattern string) (int, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return 0, err
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil {
		return 0, err
	}
	return len(prog.Inst), nil
}

// constantTime are the builtins that do not walk their arguments.
var constantTime = map[string]bool{"len": true}

// walksOperands are the binary operators whose cost grows with both operands.
var walksOperands = map[string]bool{
	"+": true, "==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"contains": true, "startsWith": true, "endsWith": true,
}

// meter counts one run's walk. It has no methods Expr can see (they are
// unexported): it is data to the program.
type meter struct {
	used     int
	calls    int
	limit    int
	start    time.Time
	deadline time.Duration
}

func newMeter(b Budget) *meter {
	return &meter{limit: b.Iterations, start: time.Now(), deadline: b.Deadline}
}

// charge adds n, at least 1: each charged step is work even when the value
// it reads is a number. The clock is read every clockEvery charges and never
// before the first clockEvery: a run that small is bounded by its iteration
// budget alone, so a busy machine (a goroutine preempted between Run and its
// first charge) can never refuse it.
func (m *meter) charge(n int) error {
	m.used += max(n, 1)
	m.calls++
	if m.used > m.limit {
		return ErrRunBudget
	}
	if m.calls%clockEvery == 0 && time.Since(m.start) > m.deadline {
		return ErrRunBudget
	}
	return nil
}

const clockEvery = 256

// runType is the struct a site's programs compile against and run on: the
// site's env embedded (so its fields are the bindings) and the meter.
func runType(env reflect.Type) (t reflect.Type, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s cannot be an expression environment: %v", env, r)
		}
	}()
	return reflect.StructOf([]reflect.StructField{
		{Name: "Site", Type: env, Anonymous: true, Tag: `expr:"$site"`},
		{Name: "Meter", Type: reflect.TypeFor[*meter](), Tag: reflect.StructTag(fmt.Sprintf("expr:%q", meterField))},
	}), nil
}

func chargeFunc(mapsFree, times bool, deep ...bool) func(args ...any) (any, error) {
	return func(args ...any) (any, error) {
		m, ok := args[0].(*meter)
		if !ok {
			return nil, errors.New("charge needs the run meter")
		}
		v := reflect.ValueOf(args[1])
		n := 0
		switch v.Kind() {
		case reflect.Slice, reflect.Array, reflect.String:
			n = v.Len()
		case reflect.Map:
			if !mapsFree {
				n = v.Len()
			}
		}
		if times {
			n *= args[2].(int)
		}
		if len(deep) > 0 && deep[0] && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
			for i := 0; i < v.Len(); i++ {
				if s, ok := v.Index(i).Interface().(string); ok {
					n += len(s)
				}
			}
		}
		if err := m.charge(n); err != nil {
			return nil, err
		}
		return args[1], nil
	}
}

func chargeValidate(args []reflect.Type) (reflect.Type, error) {
	if len(args) != 2 && len(args) != 3 {
		return nil, errors.New("charge takes the meter, one argument, and an optional factor")
	}
	if args[1] == nil {
		return reflect.TypeFor[any](), nil
	}
	return args[1], nil
}

// chargeOption installs the charges and the charger, which reports where it
// is to p (guard.go).
func chargeOption(p *pass) expr.Option {
	return func(c *conf.Config) {
		c.Functions[chargeFn] = &builtin.Function{Name: chargeFn, Func: chargeFunc(false, false), Validate: chargeValidate}
		c.Functions[chargeSeqFn] = &builtin.Function{Name: chargeSeqFn, Func: chargeFunc(true, false), Validate: chargeValidate}
		c.Functions[chargeTimesFn] = &builtin.Function{Name: chargeTimesFn, Func: chargeFunc(false, true), Validate: chargeValidate}
		c.Functions[chargeDeepFn] = &builtin.Function{Name: chargeDeepFn, Func: chargeFunc(false, false, true), Validate: chargeValidate}
		c.Visitors = append(c.Visitors, charger{p})
	}
}

type charger struct{ p *pass }

// Reset and ShouldRepeat make the charger one of Expr's repeatable patchers,
// which run after the others, each walk right after a type check and once
// (ShouldRepeat is false): so the final check follows its walk directly, and
// a call whose type it clears is checked whole (see the CallNode case).
func (charger) Reset()             {}
func (charger) ShouldRepeat() bool { return false }

func (c charger) Visit(node *ast.Node) {
	outer := c.p.name
	c.p.name, c.p.at = "charge", *node
	switch n := (*node).(type) {
	case *ast.BuiltinNode:
		if constantTime[n.Name] {
			return
		}
		for i := range n.Arguments {
			if _, ok := n.Arguments[i].(*ast.PredicateNode); !ok {
				wrap(&n.Arguments[i], chargeFn)
			}
		}
	case *ast.CallNode:
		// An engine function (lists.go; inspect admitted no other call)
		// walks each argument once; majority hashes its strings too.
		if id, ok := n.Callee.(*ast.IdentifierNode); ok && !isCharge(id.Value) {
			fn := chargeFn
			if listFuncs[spoken(id.Value)].deep {
				fn = chargeDeepFn
			}
			for i := range n.Arguments {
				wrap(&n.Arguments[i], fn)
			}
			// Expr's checker types every node before a patch, and on a later
			// check trusts a call's type without visiting its arguments, so a
			// fault in one (an unknown name) would pass unseen: the type is
			// cleared, so the final check checks the call whole.
			n.SetNature(nature.Nature{})
		}
	case *ast.BinaryNode:
		switch {
		case walksOperands[n.Operator]:
			wrap(&n.Left, chargeFn)
			wrap(&n.Right, chargeFn)
		case n.Operator == "matches":
			insts := 1
			if pat, ok := n.Right.(*ast.StringNode); ok {
				if k, err := patternInsts(pat.Value); err == nil {
					insts = k
				}
			}
			wrap(&n.Left, chargeTimesFn, &ast.IntegerNode{Value: insts})
		case n.Operator == "in":
			wrap(&n.Right, chargeSeqFn)
		}
	}
	c.p.name, c.p.at = outer, nil
}

func wrap(arg *ast.Node, fn string, extra ...ast.Node) {
	if call, ok := (*arg).(*ast.CallNode); ok {
		if id, ok := call.Callee.(*ast.IdentifierNode); ok && isCharge(id.Value) {
			return
		}
	}
	loc := (*arg).Location()
	callee := &ast.IdentifierNode{Value: fn}
	callee.SetLocation(loc)
	m := &ast.IdentifierNode{Value: meterField}
	m.SetLocation(loc)
	for _, x := range extra {
		x.SetLocation(loc)
	}
	ast.Patch(arg, &ast.CallNode{Callee: callee, Arguments: append([]ast.Node{m, *arg}, extra...)})
}
