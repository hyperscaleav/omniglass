package expression

import (
	"fmt"
	"reflect"
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
	typ    reflect.Type
	run    reflect.Type // typ embedded with the run meter (budget.go)
	result Result
}

// Binding is one name an expression may use at a site, read from the env
// struct: the name from the field's `expr` tag (the field name if absent),
// the type from the field, the doc from its `doc` tag.
type Binding struct {
	Name string
	Type string
	Doc  string
}

// EnvOf declares a site's environment as the struct type T and pins its
// result type. T must carry data only: no func, channel, unsafe pointer or
// interface (any included) anywhere in it, and no type in it with methods (Expr lets an expression
// call exported methods, and a method is behavior, not data; bind a clock
// reading as a number, not a time.Time).
func EnvOf[T any](result Result) (Env, error) {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
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
	return Env{typ: t, run: run, result: result}, nil
}

// MustEnv is EnvOf for a site declared at package init, where a bad
// declaration is a programming error.
func MustEnv[T any](result Result) Env {
	env, err := EnvOf[T](result)
	if err != nil {
		panic(err)
	}
	return env
}

// Result is the type this site's expressions must produce.
func (e Env) Result() Result { return e.result }

// Bindings lists the names an expression may use at this site, in field
// declaration order (embedded structs flattened in place).
func (e Env) Bindings() []Binding {
	if e.typ == nil {
		return nil
	}
	return bindings(e.typ)
}

func bindings(t reflect.Type) []Binding {
	var out []Binding
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			out = append(out, bindings(f.Type)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		name := f.Tag.Get("expr")
		if name == "" {
			name = f.Name
		}
		out = append(out, Binding{Name: name, Type: f.Type.String(), Doc: f.Tag.Get("doc")})
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
