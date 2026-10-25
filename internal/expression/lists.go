package expression

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
)

// The list functions (#1018): each folds one list into one value, the way a
// calc rule reduces an input bound as the list of its values. A site admits
// them by name (WithListFunctions), as it admits builtins, and each is vetted
// here with its cost argument: one pass over a list that a charge has already
// paid for, output one scalar.
//
// Three are the engine's own functions, installed only at a site that admits
// them: avg and majority, which Expr has no builtin for, and max and min,
// which replace Expr's builtins there so that one list of numbers is a typed
// argument (Expr types max over a list as unknown) while scalars still work.
// any, all and sum keep Expr's predicate forms (any(list, predicate)); their
// one-list forms (any(flags), sum(nums)) are told apart lexically, by a call
// with one argument, and renamed in place to the engine's own functions of
// the same length ($an, $al, $su), so every position in the source stays
// where the author wrote it.
//
// A list of records with a value field (a windowed calc input's samples,
// #1142) folds by that field: avg over samples is the mean of their values.
// Each function reads its list's fold type (foldElem) and its values
// (asList) through the record's value, so no function says it twice.
//
// A function's argument types are checked on the typed tree (listCheck),
// placed on the argument at fault, rather than by Expr's validation, which
// can only place a refusal on the call.

// listFunc is one vetted list function.
type listFunc struct {
	// cost is why one call is bounded.
	cost string
	// fn is the engine's own implementation; nil for sum (Expr's builtin)
	// and for any and all (whose one-list forms are oneList's).
	fn *builtin.Function
	// deep charges a list of text its strings' bytes too, for a function
	// that hashes or compares them.
	deep bool
}

// The one-list forms' engine names: the same length as any, all and sum, so the
// rename moves nothing; a name with $ cannot be written (inspect refuses
// one the rename did not place).
const (
	anyList = "$an"
	allList = "$al"
	sumList = "$su"
)

var oneListName = map[string]string{"any": anyList, "all": allList, "sum": sumList}

var listFuncs = map[string]listFunc{
	"max": {cost: "numbers, or one list of numbers: one pass over the charged list, output a number", fn: extremeFunc("max", 1)},
	"min": {cost: "numbers, or one list of numbers: one pass over the charged list, output a number", fn: extremeFunc("min", -1)},
	"avg": {cost: "one list of numbers: one pass over the charged list, output a number", fn: &builtin.Function{
		Name: "avg", Func: avgFunc, Validate: oneArg("avg", reflect.TypeFor[float64]())}},
	"sum": {cost: "one list of numbers, or a list and a predicate yielding one: one pass over the charged list, output a number; a predicate may not nest another", fn: &builtin.Function{
		Name: sumList, Func: sumFunc, Validate: sumValidate}},
	"majority": {cost: "one list of text or bools: one pass over the charged list, its strings charged their bytes, counting into a map no larger than the list, output one of its values", fn: &builtin.Function{
		Name: "majority", Func: majorityFunc, Validate: majorityValidate}, deep: true},
	"any": {cost: "one list of bools: one pass over the charged list, output a bool (the predicate form is the builtin's)", fn: &builtin.Function{
		Name: anyList, Func: anyAllFunc(true), Validate: oneArg("any", reflect.TypeFor[bool]())}},
	"all": {cost: "one list of bools: one pass over the charged list, output a bool (the predicate form is the builtin's)", fn: &builtin.Function{
		Name: allList, Func: anyAllFunc(false), Validate: oneArg("all", reflect.TypeFor[bool]())}},
}

// ListFunctions names every vetted list function, sorted, a fresh slice each
// call.
func ListFunctions() []string {
	out := make([]string, 0, len(listFuncs))
	for n := range listFuncs {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// ListFunctionCost is a list function's cost argument; empty for a name that
// is not one.
func ListFunctionCost(name string) string { return listFuncs[name].cost }

// WithListFunctions admits the named list functions at a site. Each must be
// vetted (listed in listFuncs).
func WithListFunctions(names ...string) Option {
	return func(e *Env) error {
		set := e.ListFunctions()
		for _, n := range names {
			if _, ok := listFuncs[n]; !ok {
				return fmt.Errorf("list function %q is not vetted for expressions", n)
			}
			if !slices.Contains(set, n) {
				set = append(set, n)
			}
		}
		slices.Sort(set)
		e.lists = strings.Join(set, ",")
		return nil
	}
}

// ListFunctions lists the list functions this site admits, sorted.
func (e Env) ListFunctions() []string {
	if e.lists == "" {
		return nil
	}
	return strings.Split(e.lists, ",")
}

func (e Env) admitsList(name string) bool { return slices.Contains(e.ListFunctions(), name) }

// functions are the engine functions a site installs, by the name a call
// uses: the admitted list functions with an implementation, the one-list any
// and all under their engine names.
func (e Env) functions() map[string]*builtin.Function {
	out := map[string]*builtin.Function{}
	for _, n := range e.ListFunctions() {
		if f := listFuncs[n].fn; f != nil {
			out[f.Name] = f
		}
	}
	return out
}

// spoken is the name an author wrote for an engine function's name.
func spoken(name string) string {
	switch name {
	case anyList:
		return "any"
	case allList:
		return "all"
	case sumList:
		return "sum"
	}
	return name
}

// oneListForms renames each one-argument call of any, all or sum, at a site that
// admits its list form, to the engine's one-list function, in place, and
// returns the rune offsets it renamed. A call through the pipe (x | any(...))
// or as a member (a.any(...)) is left alone, as is a source that does not
// lex (the parse reports it).
func oneListForms(source string, env Env) (string, map[int]bool) {
	if !env.admitsList("any") && !env.admitsList("all") && !env.admitsList("sum") {
		return source, nil
	}
	if !strings.Contains(source, "any") && !strings.Contains(source, "all") && !strings.Contains(source, "sum") {
		return source, nil
	}
	toks, err := lexer.Lex(file.NewSource(source))
	if err != nil {
		return source, nil
	}
	var runes []rune
	renamed := map[int]bool{}
	for i, t := range toks {
		eng, ok := oneListName[t.Value]
		if !ok || t.Kind != lexer.Identifier || !env.admitsList(t.Value) || i+1 >= len(toks) || !toks[i+1].Is(lexer.Bracket, "(") {
			continue
		}
		if i > 0 && toks[i-1].Is(lexer.Operator, ".", "?.", "|") {
			continue
		}
		if !oneArgument(toks[i+2:]) {
			continue
		}
		if runes == nil {
			runes = []rune(source)
		}
		copy(runes[t.From:t.From+len(eng)], []rune(eng))
		renamed[t.From] = true
	}
	if runes == nil {
		return source, nil
	}
	return string(runes), renamed
}

// oneArgument reports whether the tokens after a call's opening bracket hold
// exactly one argument before the bracket that closes it: something inside,
// and no comma at the call's own depth.
func oneArgument(toks []lexer.Token) bool {
	depth, any := 1, false
	for _, t := range toks {
		switch {
		case t.Kind == lexer.Bracket && (t.Value == "(" || t.Value == "[" || t.Value == "{"):
			depth++
		case t.Kind == lexer.Bracket:
			depth--
			if depth == 0 {
				return any
			}
		case t.Kind == lexer.Operator && t.Value == "," && depth == 1:
			return false
		case t.Kind == lexer.EOF:
			return false
		}
		any = true
	}
	return false
}

// oneArg validates a call that takes one argument and yields result; the
// argument's type is listCheck's.
func oneArg(name string, result reflect.Type) func([]reflect.Type) (reflect.Type, error) {
	return func(args []reflect.Type) (reflect.Type, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s takes one list, not %d arguments", name, len(args))
		}
		return result, nil
	}
}

func majorityValidate(args []reflect.Type) (reflect.Type, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("majority takes one list of text or bools, not %d arguments", len(args))
	}
	if el, ok := foldElem(args[0]); ok && (el.Kind() == reflect.String || el.Kind() == reflect.Bool) {
		return el, nil
	}
	return reflect.TypeFor[any](), nil
}

// extremeFunc is max (sign 1) or min (sign -1): numbers, or one list of
// numbers; an int result when every number is an int.
func extremeFunc(name string, sign int) *builtin.Function {
	return &builtin.Function{
		Name: name,
		Func: func(args ...any) (any, error) {
			vals := args
			if len(args) == 1 {
				if l, ok := asList(args[0]); ok {
					vals = l
				}
			}
			if len(vals) == 0 {
				return nil, fmt.Errorf("%s of an empty list has no value", name)
			}
			// Ints are compared as ints, so one past 2^53 keeps its value; a
			// float among them compares every value as a float64.
			if ints, ok := allInts(vals); ok {
				best := ints[0]
				for _, n := range ints[1:] {
					if (sign > 0 && n > best) || (sign < 0 && n < best) {
						best = n
					}
				}
				return best, nil
			}
			best := 0.0
			for i, v := range vals {
				f, _, ok := number(v)
				if !ok {
					return nil, fmt.Errorf("%s takes numbers, not %T", name, v)
				}
				if i == 0 || (sign > 0 && f > best) || (sign < 0 && f < best) {
					best = f
				}
			}
			return best, nil
		},
		Validate: func(args []reflect.Type) (reflect.Type, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("%s takes numbers, or one list of numbers", name)
			}
			ts := args
			if len(args) == 1 {
				if el, ok := foldElem(args[0]); ok {
					ts = []reflect.Type{el}
				}
			}
			for _, t := range ts {
				if t == nil || !isIntKind(t.Kind()) {
					return reflect.TypeFor[float64](), nil
				}
			}
			return reflect.TypeFor[int](), nil
		},
	}
}

// sumValidate types sum over one list: an int sum of ints, else a number.
func sumValidate(args []reflect.Type) (reflect.Type, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("sum takes one list, or a list and a predicate, not %d arguments", len(args))
	}
	if el, ok := foldElem(args[0]); ok && isIntKind(el.Kind()) {
		return reflect.TypeFor[int](), nil
	}
	return reflect.TypeFor[float64](), nil
}

// sumFunc totals a list: an int total of ints, wrapping on overflow as
// Expr's int arithmetic does (exact past 2^53, where a float64 is not), else
// a float64.
func sumFunc(args ...any) (any, error) {
	l, _ := asList(args[0])
	if ints, ok := allInts(l); ok && len(l) > 0 {
		total := 0
		for _, n := range ints {
			total += n
		}
		return total, nil
	}
	sum := 0.0
	for _, v := range l {
		f, _, ok := number(v)
		if !ok {
			return nil, fmt.Errorf("sum takes numbers, not %T", v)
		}
		sum += f
	}
	return sum, nil
}

// allInts is a list's values as ints when every one is an int.
func allInts(vals []any) ([]int, bool) {
	out := make([]int, len(vals))
	for i, v := range vals {
		rv := reflect.ValueOf(v)
		if !rv.IsValid() || !isIntKind(rv.Kind()) {
			return nil, false
		}
		out[i] = int(rv.Int())
	}
	return out, true
}

func avgFunc(args ...any) (any, error) {
	l, _ := asList(args[0])
	if len(l) == 0 {
		return nil, errors.New("avg of an empty list has no value")
	}
	sum := 0.0
	for _, v := range l {
		f, _, ok := number(v)
		if !ok {
			return nil, fmt.Errorf("avg takes numbers, not %T", v)
		}
		sum += f
	}
	return sum / float64(len(l)), nil
}

// majorityFunc is the most common value; a tie goes to the value whose text
// sorts first (false before true). It finds the best in one pass over the
// counts, no sort.
func majorityFunc(args ...any) (any, error) {
	l, _ := asList(args[0])
	if len(l) == 0 {
		return nil, errors.New("majority of an empty list has no value")
	}
	counts := map[string]int{}
	byText := map[string]any{}
	for _, v := range l {
		var k string
		switch x := v.(type) {
		case string:
			k = x
		case bool:
			k = fmt.Sprint(x)
		default:
			return nil, fmt.Errorf("majority takes text or bools, not %T", v)
		}
		counts[k]++
		byText[k] = v
	}
	best, n := "", -1
	for k, c := range counts {
		if c > n || (c == n && k < best) {
			best, n = k, c
		}
	}
	return byText[best], nil
}

func anyAllFunc(want bool) func(args ...any) (any, error) {
	return func(args ...any) (any, error) {
		l, _ := asList(args[0])
		for _, v := range l {
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("takes bools, not %T", v)
			}
			if b == want {
				return want, nil
			}
		}
		return !want, nil
	}
}

// asList is a list's values, each record with a value field read as its
// value.
func asList(v any) ([]any, bool) {
	var out []any
	if l, ok := v.([]any); ok {
		out = slices.Clone(l)
	} else {
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return nil, false
		}
		out = make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
	}
	for i, x := range out {
		rv := reflect.ValueOf(x)
		if rv.Kind() != reflect.Struct {
			continue
		}
		if f, ok := valueField(rv.Type()); ok {
			out[i] = rv.Field(f).Interface()
		}
	}
	return out, true
}

// valueField is the index of a record type's value field (tagged
// expr:"value"), the field a list of the records folds by.
func valueField(t reflect.Type) (int, bool) {
	if t.Kind() != reflect.Struct {
		return 0, false
	}
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("expr") == "value" {
			return i, true
		}
	}
	return 0, false
}

// foldElem is the type a list function folds a list type by: its element
// type, or a record element's value field's type.
func foldElem(t reflect.Type) (reflect.Type, bool) {
	el, ok := listElem(t)
	if !ok {
		return nil, false
	}
	if f, ok := valueField(el); ok {
		return el.Field(f).Type, true
	}
	return el, true
}

func number(v any) (float64, bool, bool) {
	rv := reflect.ValueOf(v)
	switch {
	case !rv.IsValid():
		return 0, false, false
	case isIntKind(rv.Kind()):
		return float64(rv.Int()), true, true
	case rv.Kind() == reflect.Float32 || rv.Kind() == reflect.Float64:
		return rv.Float(), false, true
	}
	return 0, false, false
}

func isIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

func isNumberKind(k reflect.Kind) bool {
	return isIntKind(k) || k == reflect.Float32 || k == reflect.Float64
}

// listElem is a list type's element type.
func listElem(t reflect.Type) (reflect.Type, bool) {
	if t == nil || (t.Kind() != reflect.Slice && t.Kind() != reflect.Array) {
		return nil, false
	}
	return t.Elem(), true
}

// typeWords names a type the way an author reads it.
func typeWords(t reflect.Type) string {
	if t == nil {
		return "nil"
	}
	if el, ok := listElem(t); ok {
		switch {
		case isNumberKind(el.Kind()):
			return "a list of numbers"
		case el.Kind() == reflect.String:
			return "a list of text"
		case el.Kind() == reflect.Bool:
			return "a list of bools"
		case el.Kind() == reflect.Struct:
			return "a list of records"
		case el.Kind() == reflect.Interface:
			return "a list"
		}
		return "a list of lists"
	}
	switch {
	case isNumberKind(t.Kind()):
		return "a number"
	case t.Kind() == reflect.String:
		return "text"
	case t.Kind() == reflect.Bool:
		return "a bool"
	case t.Kind() == reflect.Struct:
		return "a record"
	case t.Kind() == reflect.Map:
		return "a map"
	}
	return t.String()
}

// unknownElem is a list whose elements' type is known only when it runs (a
// list literal of mixed values): accepted at compile time, checked by the
// function when it runs.
func unknownElem(t reflect.Type) bool {
	el, ok := listElem(t)
	return ok && el.Kind() == reflect.Interface
}

// listCheck refuses a list function's argument of the wrong type, on the
// argument (typedCheck's part for the list functions).
func listCheck(node ast.Node, fail func(n ast.Node, format string, args ...any)) {
	var name string
	var args []ast.Node
	switch n := node.(type) {
	case *ast.CallNode:
		id, ok := n.Callee.(*ast.IdentifierNode)
		if !ok || isCharge(id.Value) {
			return
		}
		name, args = spoken(id.Value), n.Arguments
	case *ast.BuiltinNode:
		if n.Name != "sum" {
			return
		}
		name, args = n.Name, n.Arguments
	default:
		return
	}
	typ := func(a ast.Node) reflect.Type { return unwrapCharge(a).Type() }
	listOf := func(a ast.Node, ok func(reflect.Kind) bool, want string) {
		t := typ(a)
		el, isList := foldElem(t)
		if !isList {
			fail(unwrapCharge(a), "%s takes %s, not %s", name, want, typeWords(t))
			return
		}
		if !unknownElem(t) && !ok(el.Kind()) {
			fail(unwrapCharge(a), "%s takes %s, not %s", name, want, typeWords(t))
		}
	}
	switch name {
	case "max", "min":
		if len(args) == 1 {
			if t := typ(args[0]); t != nil && !isNumberKind(t.Kind()) && t.Kind() != reflect.Interface {
				listOf(args[0], isNumberKind, "numbers, or one list of numbers")
			}
			return
		}
		for _, a := range args {
			if t := typ(a); t == nil || (!isNumberKind(t.Kind()) && t.Kind() != reflect.Interface) {
				fail(unwrapCharge(a), "%s takes numbers, or one list of numbers, not %s among several", name, typeWords(t))
				return
			}
		}
	case "avg":
		listOf(args[0], isNumberKind, "one list of numbers")
	case "majority":
		listOf(args[0], func(k reflect.Kind) bool { return k == reflect.String || k == reflect.Bool }, "one list of text or bools")
	case "any", "all":
		listOf(args[0], func(k reflect.Kind) bool { return k == reflect.Bool }, "one list of bools, or a list and a predicate")
	case "sum":
		if len(args) == 2 {
			if p, ok := args[1].(*ast.PredicateNode); ok {
				if t := p.Node.Type(); t == nil || (!isNumberKind(t.Kind()) && t.Kind() != reflect.Interface) {
					fail(p.Node, "sum's predicate yields %s; it must yield a number", typeWords(t))
				}
			}
			return
		}
		listOf(args[0], isNumberKind, "a list of numbers")
	}
}

// wholeListFolds are the calls that read a list only as a whole, into one
// value: the one-list functions and the folds without a predicate.
var wholeListFolds = map[string]bool{"max": true, "min": true, "avg": true, "majority": true,
	anyList: true, allList: true, sumList: true, "len": true, "count": true}

// WholeListsOnly reports whether the program reads each of the named
// bindings only as a whole list folded into one value (the one argument of
// max, min, avg, sum, majority, any, all, len or count), never an element of
// it by position, through a predicate, or as a value of its own. A caller
// that may show an expression's result over a list some of whose elements
// the reader may not see uses it to tell an aggregate from a read of one
// element.
func (p *Program) WholeListsOnly(names ...string) bool {
	watched := map[string]bool{}
	for _, n := range names {
		watched[n] = true
	}
	root := p.prog.Node()
	folded := map[ast.Node]bool{}
	ast.Walk(&root, visitFunc(func(node *ast.Node) {
		var name string
		var args []ast.Node
		switch n := (*node).(type) {
		case *ast.CallNode:
			if id, ok := n.Callee.(*ast.IdentifierNode); ok {
				name, args = id.Value, n.Arguments
			}
		case *ast.BuiltinNode:
			name, args = n.Name, n.Arguments
		}
		if wholeListFolds[name] && len(args) == 1 {
			if id, ok := unwrapCharge(args[0]).(*ast.IdentifierNode); ok {
				folded[id] = true
			}
		}
	}))
	whole := true
	ast.Walk(&root, visitFunc(func(node *ast.Node) {
		if id, ok := (*node).(*ast.IdentifierNode); ok && watched[id.Value] && !folded[id] {
			whole = false
		}
	}))
	return whole
}
