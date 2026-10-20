package expression

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/conf"
)

// Expr's run budget (MaxRunMemory) counts what a run allocates, not what it
// iterates: all(items, all(items, ...)) over a bound list walks len(items)^2
// elements and charges nothing. So every builtin's collection argument, and
// the right side of every `in`, is patched to $charge($meter, arg): a
// function that returns arg unchanged and adds its length to the run's
// meter, refusing past MaxRunIterations. The meter is per run, carried in a
// hidden field of the struct the program is compiled against (the site's
// env embedded, plus the meter), so a cached program shared across
// goroutines still counts each run on its own.

const (
	chargeFn   = "$charge"
	meterField = "$meter"
)

// constantTime are the builtins that do not walk their first argument.
var constantTime = map[string]bool{"len": true, "type": true, "get": true, "first": true, "last": true}

// meter counts one run's iterations. It has no methods: it is data to Expr.
type meter struct{ used int }

func (m *meter) charge(n int) error {
	m.used += n
	if m.used > MaxRunIterations {
		return ErrRunBudget
	}
	return nil
}

// runType is the struct a site's programs compile against and run on: the
// site's env embedded (so its fields are the bindings) and the meter.
func runType(env reflect.Type) reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		{Name: "Site", Type: env, Anonymous: true, Tag: `expr:"$site"`},
		{Name: "Meter", Type: reflect.TypeFor[*meter](), Tag: reflect.StructTag(fmt.Sprintf("expr:%q", meterField))},
	})
}

func chargeOption() expr.Option {
	return func(c *conf.Config) {
		c.Functions[chargeFn] = &builtin.Function{
			Name: chargeFn,
			Func: func(args ...any) (any, error) {
				m, ok := args[0].(*meter)
				if !ok {
					return nil, errors.New("charge needs the run meter")
				}
				v := reflect.ValueOf(args[1])
				switch v.Kind() {
				case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
					if err := m.charge(v.Len()); err != nil {
						return nil, err
					}
				}
				return args[1], nil
			},
			Validate: func(args []reflect.Type) (reflect.Type, error) {
				if len(args) != 2 {
					return nil, errors.New("charge takes the meter and one argument")
				}
				if args[1] == nil {
					return reflect.TypeFor[any](), nil
				}
				return args[1], nil
			},
		}
		c.Visitors = append(c.Visitors, charger{})
	}
}

type charger struct{}

func (charger) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.BuiltinNode:
		if !constantTime[n.Name] && len(n.Arguments) > 0 {
			wrap(&n.Arguments[0])
		}
	case *ast.BinaryNode:
		if n.Operator == "in" || n.Operator == "not in" {
			wrap(&n.Right)
		}
	}
}

func wrap(arg *ast.Node) {
	if call, ok := (*arg).(*ast.CallNode); ok {
		if id, ok := call.Callee.(*ast.IdentifierNode); ok && id.Value == chargeFn {
			return
		}
	}
	loc := (*arg).Location()
	callee := &ast.IdentifierNode{Value: chargeFn}
	callee.SetLocation(loc)
	m := &ast.IdentifierNode{Value: meterField}
	m.SetLocation(loc)
	ast.Patch(arg, &ast.CallNode{Callee: callee, Arguments: []ast.Node{m, *arg}})
}
