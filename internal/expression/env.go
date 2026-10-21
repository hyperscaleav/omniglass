package expression

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/expr-lang/expr/builtin"
)

// Result is the type a site pins for its expressions' value. A compiled
// expression of any other type is refused at compile time.
type Result int

const (
	// Bool is a criteria or guard: true or false.
	Bool Result = iota + 1
	// Float is a number (an int expression widens to float64).
	Float
	// Int is a whole number.
	Int
	// String is text.
	String
	// Any leaves the value's type to the expression.
	Any
)

func (r Result) String() string {
	switch r {
	case Bool:
		return "bool"
	case Float:
		return "float64"
	case Int:
		return "int"
	case String:
		return "string"
	case Any:
		return "any"
	}
	return fmt.Sprintf("Result(%d)", int(r))
}

// Env is a site's declared environment: the struct type whose fields are the
// bindings an expression may name, and the result type it must produce. Its
// identity (the Go type and the result) is the env shape the program cache
// keys on. The zero Env is not a site.
type Env struct {
	typ      reflect.Type
	run      reflect.Type // typ embedded with the run meter (budget.go)
	result   Result
	builtins string // the admitted builtins, sorted, comma-joined (comparable)
	budget   Budget
}

// Binding is one name an expression may use at a site, read from the env
// struct: the name from the field's `expr` tag (the field name if absent),
// the type from the field, the doc from its `doc` tag.
type Binding struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Doc  string `json:"doc,omitempty"`
}

// EnvOf declares a site's environment as the struct type T and pins its
// result type. T must carry data only: no func, channel, unsafe pointer or
// interface (any included) anywhere in it, and no type in it with methods (Expr lets an expression
// call exported methods, and a method is behavior, not data; bind a clock
// reading as a number, not a time.Time).
//
// A site admits no builtin unless it names it (WithBuiltins), and runs on
// CriteriaBudget() unless it asks for another (WithBudget).
func EnvOf[T any](result Result, opts ...Option) (Env, error) {
	return EnvFor(reflect.TypeFor[T](), result, opts...)
}

// EnvFor is EnvOf for a struct type built at run time (reflect.StructOf), for
// a site whose bindings are only known when an expression is authored: a calc
// rule binds each of its named inputs. The same rules hold (a data-only
// struct, a declared result), and the same fields give the same Env, so the
// program cache is shared across rules that declare the same inputs.
func EnvFor(t reflect.Type, result Result, opts ...Option) (Env, error) {
	if t == nil || t.Kind() != reflect.Struct {
		return Env{}, fmt.Errorf("an expression environment is a struct, not %s", t)
	}
	if result < Bool || result > Any {
		return Env{}, fmt.Errorf("unknown result type %d", int(result))
	}
	if err := dataOnly(t, t.String(), map[reflect.Type]bool{}); err != nil {
		return Env{}, err
	}
	run, err := runType(t)
	if err != nil {
		return Env{}, err
	}
	env := Env{typ: t, run: run, result: result, budget: CriteriaBudget()}
	for _, opt := range opts {
		if err := opt(&env); err != nil {
			return Env{}, err
		}
	}
	return env, nil
}

// MustEnv is EnvOf for a site declared at package init, where a bad
// declaration is a programming error.
func MustEnv[T any](result Result, opts ...Option) Env {
	env, err := EnvOf[T](result, opts...)
	if err != nil {
		panic(err)
	}
	return env
}

// MustEnvFor is EnvFor for a site declared at package init.
func MustEnvFor(t reflect.Type, result Result, opts ...Option) Env {
	env, err := EnvFor(t, result, opts...)
	if err != nil {
		panic(err)
	}
	return env
}

// Result is the type this site's expressions must produce.
func (e Env) Result() Result { return e.result }

// Budget is what one run at this site may spend.
func (e Env) Budget() Budget { return e.budget }

// Builtins lists the builtins this site admits, sorted.
func (e Env) Builtins() []string {
	if e.builtins == "" {
		return nil
	}
	return strings.Split(e.builtins, ",")
}

func (e Env) admits(name string) bool { return slices.Contains(e.Builtins(), name) }

// Option declares one more thing about a site.
type Option func(*Env) error

// WithBuiltins admits the named builtins at a site. Each must be vetted
// (listed in vetted): a builtin whose cost is linear in what the run is charged for and
// whose output is no larger than its inputs. A builtin is vetted
// deliberately, with its cost argument and its tests, when a site needs it.
func WithBuiltins(names ...string) Option {
	return func(e *Env) error {
		set := e.Builtins()
		for _, n := range names {
			if _, ok := vetted[n]; !ok {
				return fmt.Errorf("builtin %q is not vetted for expressions; vet it (a cost argument and tests) before a site admits it", n)
			}
			if !slices.Contains(set, n) {
				set = append(set, n)
			}
		}
		slices.Sort(set)
		e.builtins = strings.Join(set, ",")
		return nil
	}
}

// WithBudget sets a site's run budget, each part positive and no larger than
// CeilingBudget.
func WithBudget(b Budget) Option {
	return func(e *Env) error {
		c := CeilingBudget()
		if b.Iterations <= 0 || b.Memory <= 0 || b.Deadline <= 0 ||
			b.Iterations > c.Iterations || b.Memory > c.Memory || b.Deadline > c.Deadline {
			return fmt.Errorf("a run budget is positive and within the ceiling %+v, not %+v", c, b)
		}
		e.budget = b
		return nil
	}
}

// Budget is what one run may spend: Iterations is the walk budget (the
// summed length of everything a builtin or operator reads, at least one per
// step; budget.go), Memory is Expr's allocation budget, and Deadline is the
// wall-clock backstop on charged work.
type Budget struct {
	Iterations int
	Memory     int
	Deadline   time.Duration
}

// CriteriaBudget is the default: a criteria runs on the ingest path, per
// sample change, against a handful of bindings. A fresh value each call.
func CriteriaBudget() Budget {
	return Budget{Iterations: 10_000, Memory: 10_000, Deadline: 5 * time.Millisecond}
}

// CeilingBudget is the most any site may ask for (a calc reduce, off the
// ingest path), built from constants so it cannot be raised at run time.
func CeilingBudget() Budget {
	return Budget{Iterations: MaxRunIterations, Memory: MaxRunMemory, Deadline: MaxRunDuration}
}

// vetted is every builtin a site may admit, with the argument for why its
// cost is bounded. Every other Expr builtin is refused at compile time,
// including the ones that build (map, reduce, sortBy, groupBy, concat,
// flatten, uniq, repeat, replace, join, split, the range ..), encode (toJSON,
// fromJSON, base64), or read the clock (DisabledBuiltins): their cost is
// Go-native work that charging inputs cannot bound and the deadline cannot
// pre-empt.
var vetted = map[string]string{
	"len":        "constant time",
	"abs":        "a scalar in, a scalar out",
	"ceil":       "a scalar in, a scalar out",
	"floor":      "a scalar in, a scalar out",
	"round":      "a scalar in, a scalar out",
	"min":        "scalars only (an array argument is refused); linear in its arguments",
	"max":        "scalars only (an array argument is refused); linear in its arguments",
	"lower":      "linear in the charged string; output no longer than it",
	"upper":      "linear in the charged string; output no longer than it",
	"trim":       "linear in the charged string; output no longer than it",
	"trimPrefix": "linear in the charged strings; output no longer than the first",
	"trimSuffix": "linear in the charged strings; output no longer than the first",
	"all":        "one pass over the charged list; a predicate may not nest another",
	"any":        "one pass over the charged list; a predicate may not nest another",
	"none":       "one pass over the charged list; a predicate may not nest another",
	"one":        "one pass over the charged list; a predicate may not nest another",
	"count":      "one pass over the charged list; a predicate may not nest another",
	"filter":     "one pass over the charged list, output no longer than it; a predicate may not nest another",
}

// CriteriaBuiltins are the builtins a criteria site admits (today, all of
// vetted), as a fresh slice each call.
func CriteriaBuiltins() []string {
	return []string{
		"abs", "all", "any", "ceil", "count", "filter", "floor", "len", "lower", "max",
		"min", "none", "one", "round", "trim", "trimPrefix", "trimSuffix", "upper",
	}
}

// scalarOnly are the vetted builtins Expr would also let walk an array.
var scalarOnly = map[string]bool{"min": true, "max": true, "abs": true, "ceil": true, "floor": true, "round": true}

// Bindings lists the names an expression may use at this site, in field
// declaration order (embedded structs flattened in place, a nested struct's
// fields by their dotted path).
func (e Env) Bindings() []Binding {
	if e.typ == nil {
		return nil
	}
	return bindings(e.typ)
}

func bindings(t reflect.Type) []Binding { return bindingsAt(t, "") }

// bindingsAt lists t's fields under prefix: an embedded struct's in place, a
// named struct field's by their dotted path (component.name), the way an
// expression names them, and every other field as itself.
func bindingsAt(t reflect.Type, prefix string) []Binding {
	var out []Binding
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			out = append(out, bindingsAt(f.Type, prefix)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		name := f.Tag.Get("expr")
		if name == "" {
			name = f.Name
		}
		if f.Type.Kind() == reflect.Struct {
			out = append(out, bindingsAt(f.Type, prefix+name+".")...)
			continue
		}
		// A list of records: the list itself, then each record's fields by
		// their path under name[].
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct {
			out = append(out, Binding{Name: prefix + name, Type: "[]record", Doc: f.Tag.Get("doc")})
			out = append(out, bindingsAt(f.Type.Elem(), prefix+name+"[].")...)
			continue
		}
		out = append(out, Binding{Name: prefix + name, Type: f.Type.String(), Doc: f.Tag.Get("doc")})
	}
	return out
}

// dataOnly refuses a type that carries behavior: a func, a channel, an
// unsafe pointer, a type with exported methods, or any interface. An
// interface, even the empty one, is refused because the value behind it is
// chosen at run time: a caller could bind a func, a time.Time, or a Stringer,
// and an expression could then call it. A future site that binds a JSON
// payload needs a dedicated data-only value type (a closed sum of null, bool,
// number, string, list and object) rather than any.
func dataOnly(t reflect.Type, path string, seen map[reflect.Type]bool) error {
	if seen[t] {
		return nil
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Errorf("%s is a %s; an expression environment carries data only", path, t.Kind())
	case reflect.Interface:
		return fmt.Errorf("%s is the interface %s; an expression environment binds concrete data types only", path, t)
	}
	if t.NumMethod() > 0 || (t.Kind() != reflect.Pointer && reflect.PointerTo(t).NumMethod() > 0) {
		return fmt.Errorf("%s has methods (%s); an expression environment carries data only", path, t)
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return dataOnly(t.Elem(), path+"[]", seen)
	case reflect.Map:
		if err := dataOnly(t.Key(), path+" key", seen); err != nil {
			return err
		}
		return dataOnly(t.Elem(), path+"[]", seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() && !f.Anonymous {
				continue
			}
			if err := dataOnly(f.Type, path+"."+f.Name, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

// speak rewrites the Go type names a diagnostic carries (rules.Component)
// to the binding path an expression uses for them (component), so an
// author reads the language's names, not the engine's.
func (e Env) speak(msg string) string {
	var pairs []string
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() || f.Type.Kind() != reflect.Struct {
				continue
			}
			if f.Anonymous {
				walk(f.Type, prefix)
				continue
			}
			name := f.Tag.Get("expr")
			if name == "" {
				name = f.Name
			}
			if f.Type.Name() != "" {
				pairs = append(pairs, f.Type.String(), prefix+name)
			}
			walk(f.Type, prefix+name+".")
		}
	}
	walk(e.typ, "")
	if len(pairs) == 0 {
		return msg
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}

// MaxBindingName is the longest name a site may bind at authoring time.
const MaxBindingName = 64

// keywords are the words Expr's grammar reserves.
var keywords = map[string]bool{
	"true": true, "false": true, "nil": true, "and": true, "or": true, "not": true, "in": true,
	"matches": true, "contains": true, "startsWith": true, "endsWith": true, "let": true,
	"if": true, "else": true,
}

// BindableName reports whether name may be bound as an expression name by an
// author (a calc rule's input): an identifier (a letter or underscore, then
// letters, digits or underscores), at most MaxBindingName long, and neither
// a keyword nor the name of any Expr builtin, admitted at a site or not, so
// vetting a builtin later never changes what a stored expression means.
func BindableName(name string) error {
	if name == "" || len(name) > MaxBindingName {
		return fmt.Errorf("a name is 1 to %d characters, not %d", MaxBindingName, len(name))
	}
	for i, r := range name {
		letter := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return fmt.Errorf("%q is not an identifier: a letter or underscore, then letters, digits or underscores", name)
		}
	}
	if keywords[name] {
		return fmt.Errorf("%q is a keyword of the expression language", name)
	}
	if _, ok := builtin.Index[name]; ok {
		return fmt.Errorf("%q is the name of a builtin function", name)
	}
	return nil
}
