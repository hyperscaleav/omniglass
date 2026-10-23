package expression

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/expr-lang/expr/ast"
)

// The starter diagnostics (#1054), each a registry entry with its fixtures
// beside it. A rule is added the same way: a matcher, a message, an optional
// fix, and the examples that hold it to its word.

// The fixture environments: a criteria site's (a metric's value) and a calc
// site's (a system rule reading pts, a number input, and up, a bool input),
// shaped as those sites declare theirs (internal/rules), and named so the
// rules for each site run on them.
type criteriaShape struct {
	Value float64 `expr:"value" doc:"the sample's number"`
	Key   string  `expr:"key" doc:"the sample key"`
}

type calcMemberShape struct {
	Name  string `expr:"name"`
	Value struct {
		Pts float64 `expr:"pts"`
		Up  bool    `expr:"up"`
	} `expr:"value"`
	Has struct {
		Pts bool `expr:"pts"`
		Up  bool `expr:"up"`
	} `expr:"has"`
}

type calcShape struct {
	Pts     []float64         `expr:"pts"`
	Up      []bool            `expr:"up"`
	Members []calcMemberShape `expr:"members"`
}

var (
	criteriaFixture = MustEnv[criteriaShape](Bool, WithBuiltins(CriteriaBuiltins()...), WithSite("criteria"))
	calcBoolFixture = MustEnv[calcShape](Bool, calcFixtureOptions()...)
	calcNumFixture  = MustEnv[calcShape](Float, calcFixtureOptions()...)
)

func calcFixtureOptions() []Option {
	return []Option{WithBuiltins(CriteriaBuiltins()...), WithListFunctions(ListFunctions()...),
		WithBudget(CeilingBudget()), WithSite("calc"), WithInputs("pts", "up")}
}

// The registry, in the order the rules run.
var registry = mustRules(sumOfBools, floatEquality, countEqualsLen, unusedAlias)

// sumOfBools explains Expr's refusal of a sum over bools (its operator
// mismatch at whatever compares the sum, or the engine's own refusal of the
// list or predicate) as the mistake it is: counting with sum.
var sumOfBools = rule{
	code:     "sum-of-bools",
	severity: SeverityError,
	trigger:  triggerRefine,
	message:  "sum adds numbers; to count matches use count(...)",
	match:    matchSumOfBools,
	fixtures: []fixture{
		{env: calcBoolFixture, source: "sum(members, .has.pts) > 0", want: true, column: 1, fixed: "count(members, .has.pts) > 0"},
		{env: calcBoolFixture, source: "sum(up) >= 2", want: true, column: 1, fixed: "count(up) >= 2"},
		{env: calcBoolFixture, source: "len(pts) > 0 && sum(pts, # > 50) > 1", want: true, column: 17, fixed: "len(pts) > 0 && count(pts, # > 50) > 1"},
		{env: calcNumFixture, source: "sum(members, .has.up)", want: true, column: 1, fixed: "count(members, .has.up)"},
		{env: calcNumFixture, source: "sum(pts)"},
		{env: calcNumFixture, source: "sum(members, .value.pts)"},
		{env: calcBoolFixture, source: "count(members, .has.pts) > 0"},
		// Another refusal is left as it is.
		{env: calcBoolFixture, source: "len(pts) + up > 0"},
		{env: calcBoolFixture, source: "sum(pts) > 0 && nope"},
	},
}

// floatEquality flags == or != between a measured number and a decimal that
// is not a whole number: a float is seldom exactly a decimal, so the
// comparison is rarely what was meant.
var floatEquality = rule{
	code:     "float-equality",
	severity: SeverityWarning,
	trigger:  triggerLint,
	message:  "a measured number seldom equals a decimal exactly; compare a range instead, such as abs(value - 1.5) < 0.01",
	match:    matchFloatEquality,
	fixtures: []fixture{
		{env: criteriaFixture, source: "value == 1.5", want: true, column: 7},
		{env: criteriaFixture, source: "-0.1 != value", want: true, column: 6},
		{env: calcBoolFixture, source: "avg(pts) == 20.5", want: true, column: 10},
		{env: criteriaFixture, source: "value == 80.0"},
		{env: criteriaFixture, source: "value == 80"},
		{env: criteriaFixture, source: "value > 1.5"},
		{env: criteriaFixture, source: "abs(value - 1.5) < 0.01"},
		{env: criteriaFixture, source: "1.5 == 1.5"},
	},
}

// countEqualsLen hints that counting the elements a predicate holds for and
// comparing with the list's length is all.
var countEqualsLen = rule{
	code:     "count-equals-len",
	severity: SeverityHint,
	trigger:  triggerLint,
	message:  "count(xs, p) == len(xs) is all(xs, p)",
	match:    matchCountEqualsLen,
	fixtures: []fixture{
		{env: calcBoolFixture, source: "count(members, .has.pts) == len(members)", want: true, column: 1, fixed: "all(members, .has.pts)"},
		{env: calcBoolFixture, source: "len(pts) == count(pts, # > 20) && len(up) > 0", want: true, column: 13, fixed: "all(pts, # > 20) && len(up) > 0"},
		{env: calcBoolFixture, source: "count(pts, # > 20) == len(up)"},
		{env: calcBoolFixture, source: "count(pts, # > 20) == len(pts) - 1"},
		{env: calcBoolFixture, source: "all(pts, # > 20)"},
	},
}

// unusedAlias warns of a calc rule's input the expression never names: the
// rule reads it, and waits on it, for nothing.
var unusedAlias = rule{
	code:     "unused-alias",
	severity: SeverityWarning,
	trigger:  triggerLint,
	sites:    []string{"calc"},
	message:  "an input the expression never names is read for nothing; use it, or remove the input",
	match:    matchUnusedAlias,
	fixtures: []fixture{
		{env: calcNumFixture, source: "max(pts)", want: true},
		{env: calcNumFixture, source: "count(up) + max(pts)"},
		{env: calcBoolFixture, source: "all(members, .has.pts && .has.up)"},
		{env: calcBoolFixture, source: "len(pts) > 0 && members[0].value.up"},
	},
}

// nodes is every node under root, in walk order.
func nodes(root ast.Node) []ast.Node {
	var out []ast.Node
	ast.Walk(&root, visitFunc(func(n *ast.Node) { out = append(out, *n) }))
	return out
}

func boolKind(t reflect.Type) bool { return t != nil && t.Kind() == reflect.Bool }

func matchSumOfBools(s *subject) []finding {
	for _, n := range nodes(s.root) {
		var name ast.Node // the sum's name, where the finding and the fix sit
		msg := ""
		switch m := n.(type) {
		case *ast.BuiltinNode:
			if m.Name != "sum" || len(m.Arguments) != 2 {
				continue
			}
			p, ok := m.Arguments[1].(*ast.PredicateNode)
			if !ok || !boolKind(p.Node.Type()) {
				continue
			}
			name, msg = m, "sum adds numbers, and this predicate yields true or false; to count the matches use count(...)"
		case *ast.CallNode:
			id, ok := m.Callee.(*ast.IdentifierNode)
			if !ok || id.Value != sumList || len(m.Arguments) != 1 {
				continue
			}
			if el, ok := listElem(unwrapCharge(m.Arguments[0]).Type()); !ok || el.Kind() != reflect.Bool {
				continue
			}
			name, msg = id, "sum adds numbers, and this list holds true or false; to count the true ones use count(...)"
		default:
			continue
		}
		if !s.near(n) {
			continue
		}
		from := name.Location().From
		f := finding{from: from, message: msg}
		if s.env.admits("count") {
			f.fix = &edit{title: "Use count", from: from, to: from + len("sum"), replacement: "count"}
		}
		return []finding{f}
	}
	return nil
}

// decimal is a literal number that is not a whole one (1.5, -0.1), as its
// value; ok is false for anything else.
func decimal(n ast.Node) (float64, bool) {
	n = unwrapCharge(n)
	neg := false
	if u, ok := n.(*ast.UnaryNode); ok && (u.Operator == "-" || u.Operator == "+") {
		neg, n = u.Operator == "-", u.Node
	}
	f, ok := n.(*ast.FloatNode)
	if !ok || f.Value == math.Trunc(f.Value) {
		return 0, false
	}
	if neg {
		return -f.Value, true
	}
	return f.Value, true
}

func floatKind(t reflect.Type) bool {
	return t != nil && (t.Kind() == reflect.Float64 || t.Kind() == reflect.Float32)
}

func matchFloatEquality(s *subject) []finding {
	var out []finding
	for _, n := range nodes(s.root) {
		b, ok := n.(*ast.BinaryNode)
		if !ok || (b.Operator != "==" && b.Operator != "!=") {
			continue
		}
		lit, measured := b.Right, b.Left
		v, ok := decimal(lit)
		if !ok {
			lit, measured = b.Left, b.Right
			if v, ok = decimal(lit); !ok {
				continue
			}
		}
		if _, isLit := decimal(measured); isLit || !floatKind(unwrapCharge(measured).Type()) {
			continue
		}
		from, to := s.span(unwrapCharge(measured))
		num := strconv.FormatFloat(v, 'f', -1, 64)
		out = append(out, finding{from: b.Location().From, message: fmt.Sprintf(
			"a measured number seldom equals %s exactly; compare a range instead, such as abs(%s - %s) < 0.01",
			num, s.text(from, to), num)})
	}
	return out
}

// sameList reports whether two list operands are the same list, written the
// same way.
func sameList(a, b ast.Node) bool {
	return unwrapCharge(a).String() == unwrapCharge(b).String()
}

func matchCountEqualsLen(s *subject) []finding {
	if !s.env.admits("all") {
		return nil
	}
	var out []finding
	for _, n := range nodes(s.root) {
		b, ok := n.(*ast.BinaryNode)
		if !ok || b.Operator != "==" {
			continue
		}
		count, length := asBuiltin(b.Left, "count", 2), asBuiltin(b.Right, "len", 1)
		if count == nil || length == nil {
			count, length = asBuiltin(b.Right, "count", 2), asBuiltin(b.Left, "len", 1)
		}
		if count == nil || length == nil || !sameList(count.Arguments[0], length.Arguments[0]) {
			continue
		}
		if _, ok := count.Arguments[1].(*ast.PredicateNode); !ok {
			continue
		}
		from, to := s.span(b)
		_, end := s.span(count)
		out = append(out, finding{from: count.Location().From, fix: &edit{title: "Use all", from: from, to: to,
			replacement: "all" + s.text(count.Location().To, end)}})
	}
	return out
}

// asBuiltin is n (seen through a charge) when it is a call to the builtin
// name with args arguments, else nil.
func asBuiltin(n ast.Node, name string, args int) *ast.BuiltinNode {
	if b, ok := unwrapCharge(n).(*ast.BuiltinNode); ok && b.Name == name && len(b.Arguments) == args {
		return b
	}
	return nil
}

func matchUnusedAlias(s *subject) []finding {
	inputs := s.env.Inputs()
	if len(inputs) == 0 {
		return nil
	}
	named := map[string]bool{}
	for _, n := range nodes(s.root) {
		switch m := n.(type) {
		case *ast.IdentifierNode:
			named[m.Value] = true
		case *ast.MemberNode:
			// A member's own value of an input: members[0].value.t, .has.t.
			if p, ok := m.Property.(*ast.StringNode); ok {
				named[p.Value] = true
			}
		}
	}
	var out []finding
	for _, in := range inputs {
		if !named[in] {
			out = append(out, finding{from: -1, message: fmt.Sprintf(
				"the input %s is never named in the expression, so the rule reads it for nothing; use it, or remove the input", in)})
		}
	}
	return out
}
