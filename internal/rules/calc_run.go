package rules

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
)

// ErrMissingInput is an input with no current value on the owner: the rule
// cannot reduce, so it writes nothing (#525 decision 5) and says so.
var ErrMissingInput = errors.New("an input has no current value")

// MissingInputError names the input that had no value.
type MissingInputError struct {
	As, Key string
}

func (e *MissingInputError) Error() string {
	return fmt.Sprintf("input %s (%s) has no current value", e.As, e.Key)
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

// Reduce computes the rule's output from its inputs' current values, one
// slice per input in the rule's order, each value of the input's type
// (CalcValue). A named reducer folds every value of its one input; a
// component expression reads each input's one value (its aggregate
// instance); a system expression reads each input as a list. An input with
// no value is a MissingInputError; a number output must be finite. Pure.
func (c *CalcProgram) Reduce(values [][]any) (any, error) {
	if len(values) != len(c.inputs) {
		return nil, fmt.Errorf("%d inputs given, the rule reads %d", len(values), len(c.inputs))
	}
	for i, vs := range values {
		if len(vs) == 0 {
			return nil, &MissingInputError{As: c.inputs[i].As, Key: c.inputs[i].Key}
		}
		for _, v := range vs {
			if !valueIs(c.inputs[i].ValueType, v) {
				return nil, fmt.Errorf("input %s: %v is not a %s", c.inputs[i].As, v, c.inputs[i].ValueType)
			}
		}
	}
	var out any
	var err error
	if c.Reducer != nil {
		out, err = fold(c.Reducer.Name, values[0])
	} else {
		out, err = c.run(values)
	}
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

func (c *CalcProgram) run(values [][]any) (any, error) {
	in := reflect.New(c.envType).Elem()
	for i, vs := range values {
		f := in.Field(i)
		if c.ownerKind == OwnerSystem {
			list := reflect.MakeSlice(f.Type(), len(vs), len(vs))
			for j, v := range vs {
				list.Index(j).Set(reflect.ValueOf(v))
			}
			f.Set(list)
			continue
		}
		if len(vs) != 1 {
			return nil, fmt.Errorf("input %s has %d values; an expression on a component reads one (fold instances with a reducer)", c.inputs[i].As, len(vs))
		}
		f.Set(reflect.ValueOf(vs[0]))
	}
	return c.Program.Run(in.Interface())
}

// fold is a named reducer over one input's values (non-empty, typed).
func fold(name string, vs []any) (any, error) {
	switch name {
	case "count":
		return float64(len(vs)), nil
	case "average", "min", "max", "sum":
		acc := vs[0].(float64)
		sum := 0.0
		for _, v := range vs {
			f := v.(float64)
			sum += f
			switch {
			case name == "min" && f < acc, name == "max" && f > acc:
				acc = f
			}
		}
		switch name {
		case "average":
			return sum / float64(len(vs)), nil
		case "sum":
			return sum, nil
		}
		return acc, nil
	case "any", "all":
		want := name == "any"
		for _, v := range vs {
			if v.(bool) == want {
				return want, nil
			}
		}
		return !want, nil
	case "majority":
		counts := map[string]int{}
		byText := map[string]any{}
		for _, v := range vs {
			k := FormatCalcValue(v)
			counts[k]++
			byText[k] = v
		}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		best := keys[0]
		for _, k := range keys[1:] {
			if counts[k] > counts[best] {
				best = k
			}
		}
		return byText[best], nil
	}
	return nil, fmt.Errorf("unknown reducer %q", name)
}
