package rules

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ErrMissingInput is an input with no current value on the owner: the rule
// cannot reduce, so it writes nothing (#525 decision 5) and says so.
var ErrMissingInput = errors.New("an input has no current value")

// MissingInputError names the input that had no value.
type MissingInputError struct {
	Alias, Key string
}

func (e *MissingInputError) Error() string {
	return fmt.Sprintf("input %s (%s) has no current value", e.Alias, e.Key)
}

func (e *MissingInputError) Unwrap() error { return ErrMissingInput }

// CalcValue reads a stored value as its value type: a metric's number, a
// property's text, a bool property's true or false.
func CalcValue(valueType, raw string) (any, error) {
	switch valueType {
	case ValueNumber:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return f, nil
	case ValueBool:
		switch raw {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a bool (true or false)", raw)
	case ValueText:
		return raw, nil
	}
	return nil, fmt.Errorf("unknown value type %q", valueType)
}

// FormatCalcValue writes a property output's value as the text a property
// stores (a bool as true or false).
func FormatCalcValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return fmt.Sprint(v)
}

// CalcMember is one role-bound member of a system as a system rule reads
// it: its id and name, its values of each input in the rule's order (one per
// instance, under the instance rules), and its own value of each input (its
// aggregate instance's; nil when it has none), which its record carries.
type CalcMember struct {
	ID, Name string
	Values   [][]any
	Record   []any
}

// ReduceSystem computes a system rule's output from its members: each input
// bound as every member's values, in member order (name, then id), and
// members as one record per member. An input no member holds a value of
// (every input, for a system with no members) is a MissingInputError. Pure.
func (c *CalcProgram) ReduceSystem(members []CalcMember) (any, error) {
	if c.ownerKind != OwnerSystem {
		return nil, fmt.Errorf("a %s rule reduces with Reduce", c.ownerKind)
	}
	members = slices.Clone(members)
	slices.SortFunc(members, func(a, b CalcMember) int {
		if n := strings.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	lists := make([][]any, len(c.inputs))
	for _, m := range members {
		if len(m.Values) != len(c.inputs) || (m.Record != nil && len(m.Record) != len(c.inputs)) {
			return nil, fmt.Errorf("member %s has %d inputs, the rule reads %d", m.ID, len(m.Values), len(c.inputs))
		}
		for i := range c.inputs {
			lists[i] = append(lists[i], m.Values[i]...)
		}
	}
	if err := c.checkLists(lists); err != nil {
		return nil, err
	}
	in := reflect.New(c.envType).Elem()
	if err := c.bindLists(in, lists); err != nil {
		return nil, err
	}
	recs := in.Field(len(c.inputs))
	rec := recs.Type().Elem()
	list := reflect.MakeSlice(recs.Type(), len(members), len(members))
	for j, m := range members {
		r := reflect.New(rec).Elem()
		r.Field(0).SetString(m.ID)
		r.Field(1).SetString(m.Name)
		for i, in := range c.inputs {
			if m.Record == nil || m.Record[i] == nil {
				continue
			}
			if !valueIs(in.ValueType, m.Record[i]) {
				return nil, fmt.Errorf("input %s: %v is not a %s", in.Alias, m.Record[i], in.ValueType)
			}
			r.Field(2).Field(i).Set(reflect.ValueOf(m.Record[i]))
			r.Field(3).Field(i).SetBool(true)
		}
		list.Index(j).Set(r)
	}
	recs.Set(list)
	return c.finish(c.Program.Run(in.Interface()))
}

// Reduce computes a component rule's output from its inputs' current
// values, one list per input in the rule's order (one value per instance),
// each value of the input's type (CalcValue). An input with no value is a
// MissingInputError; a number output must be finite. Pure.
func (c *CalcProgram) Reduce(values [][]any) (any, error) {
	if c.ownerKind == OwnerSystem {
		return nil, errors.New("a system rule reduces with ReduceSystem")
	}
	if len(values) != len(c.inputs) {
		return nil, fmt.Errorf("%d inputs given, the rule reads %d", len(values), len(c.inputs))
	}
	if err := c.checkLists(values); err != nil {
		return nil, err
	}
	in := reflect.New(c.envType).Elem()
	if err := c.bindLists(in, values); err != nil {
		return nil, err
	}
	return c.finish(c.Program.Run(in.Interface()))
}

// checkLists refuses an input with no value (missing) and a value of the
// wrong type.
func (c *CalcProgram) checkLists(lists [][]any) error {
	for i, vs := range lists {
		if len(vs) == 0 {
			return &MissingInputError{Alias: c.inputs[i].Alias, Key: c.inputs[i].Key}
		}
		for _, v := range vs {
			if !valueIs(c.inputs[i].ValueType, v) {
				return fmt.Errorf("input %s: %v is not a %s", c.inputs[i].Alias, v, c.inputs[i].ValueType)
			}
		}
	}
	return nil
}

// bindLists sets each input's list field (the env's first fields, in order).
func (c *CalcProgram) bindLists(in reflect.Value, lists [][]any) error {
	for i, vs := range lists {
		f := in.Field(i)
		l := reflect.MakeSlice(f.Type(), len(vs), len(vs))
		for j, v := range vs {
			l.Index(j).Set(reflect.ValueOf(v))
		}
		f.Set(l)
	}
	return nil
}

// finish refuses a number result that is not finite.
func (c *CalcProgram) finish(out any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if f, ok := out.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return nil, errors.New("the result is not a finite number")
	}
	return out, nil
}

func valueIs(valueType string, v any) bool {
	switch valueType {
	case ValueNumber:
		_, ok := v.(float64)
		return ok
	case ValueText:
		_, ok := v.(string)
		return ok
	case ValueBool:
		_, ok := v.(bool)
		return ok
	}
	return false
}
