package expression

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/conf"
)

// Expr's run budget (MaxRunMemory) counts what a run allocates, not what it
// walks: all(items, all(items, ...)) over a bound list, a string compared or
// searched in a loop, or a string doubled by + charge it nothing. So the
// program is patched (charger) to route every value a walk reads through a
// charge on the run's meter, which adds the value's length (at least one) and
// refuses past MaxRunIterations:
//
//   - every argument of a builtin that walks (all but len, type, get, first,
//     last), except its predicate;
//   - both operands of +, the comparisons, contains, startsWith, endsWith;
//   - the subject of matches (the pattern is a constant, compiled once);
//   - the collection on the right of `in`, unless it is a map (a lookup).
//
// The charge returns its argument unchanged, so types and results do not
// move. The meter is per run, carried in a hidden field of the struct the
// program is compiled against (the site's env embedded, plus the meter), so
// a cached program shared across goroutines still counts each run on its own.
//
// The meter also holds a wall-clock deadline (MaxRunDuration) from the start
// of the run, checked at every charge. It is a backstop, not the budget: the
// iteration count is deterministic and is what bounds an expression; the
// deadline is the one limit that depends on the machine, there only so a
// walk a future Expr version adds without a charge is still bounded.

const (
	chargeFn    = "$charge"    // charges a collection, map, or string
	chargeSeqFn = "$chargeSeq" // as chargeFn, but a map is free (an `in` lookup)
	meterField  = "$meter"
)

// runDeadline is MaxRunDuration; a variable only so a test can force it.
var runDeadline = MaxRunDuration

// constantTime are the builtins that do not walk their arguments.
var constantTime = map[string]bool{"len": true, "type": true, "get": true, "first": true, "last": true}

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
	start    time.Time
	deadline time.Duration
}

func newMeter() *meter { return &meter{start: time.Now(), deadline: runDeadline} }

// charge adds n, at least 1: each charged step is work even when the value
// it reads is a number. The clock is read every clockEvery charges.
func (m *meter) charge(n int) error {
	m.used += max(n, 1)
	m.calls++
	if m.used > MaxRunIterations {
		return ErrRunBudget
	}
	if (m.calls%clockEvery == 0 || m.deadline <= 0) && time.Since(m.start) > m.deadline {
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

func chargeFunc(mapsFree bool) func(args ...any) (any, error) {
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
		if err := m.charge(n); err != nil {
			return nil, err
		}
		return args[1], nil
	}
}

func chargeValidate(args []reflect.Type) (reflect.Type, error) {
	if len(args) != 2 {
		return nil, errors.New("charge takes the meter and one argument")
	}
	if args[1] == nil {
		return reflect.TypeFor[any](), nil
	}
	return args[1], nil
}

func chargeOption() expr.Option {
	return func(c *conf.Config) {
		c.Functions[chargeFn] = &builtin.Function{Name: chargeFn, Func: chargeFunc(false), Validate: chargeValidate}
		c.Functions[chargeSeqFn] = &builtin.Function{Name: chargeSeqFn, Func: chargeFunc(true), Validate: chargeValidate}
		c.Visitors = append(c.Visitors, charger{})
	}
}

type charger struct{}

func (charger) Visit(node *ast.Node) {
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
	case *ast.BinaryNode:
		switch {
		case walksOperands[n.Operator]:
			wrap(&n.Left, chargeFn)
			wrap(&n.Right, chargeFn)
		case n.Operator == "matches":
			wrap(&n.Left, chargeFn)
		case n.Operator == "in":
			wrap(&n.Right, chargeSeqFn)
		}
	}
}

func wrap(arg *ast.Node, fn string) {
	if call, ok := (*arg).(*ast.CallNode); ok {
		if id, ok := call.Callee.(*ast.IdentifierNode); ok && (id.Value == chargeFn || id.Value == chargeSeqFn) {
			return
		}
	}
	loc := (*arg).Location()
	callee := &ast.IdentifierNode{Value: fn}
	callee.SetLocation(loc)
	m := &ast.IdentifierNode{Value: meterField}
	m.SetLocation(loc)
	ast.Patch(arg, &ast.CallNode{Callee: callee, Arguments: []ast.Node{m, *arg}})
}
