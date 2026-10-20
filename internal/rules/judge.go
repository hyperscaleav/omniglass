package rules

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync/atomic"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

// Component is the sample's owning component, as the criteria site binds it.
type Component struct {
	Name string `expr:"name" doc:"The component's name"`
	Type string `expr:"type" doc:"The component's type, by name; empty when its product names none"`
}

// MetricInput is the criteria site's prepared input on the metric lane.
type MetricInput struct {
	Value     float64   `expr:"value" doc:"The sample's value, typed by the key: a metric's number, a property's text, or a bool property's truth value"`
	Key       string    `expr:"key" doc:"The sample key the rule reads: a property or a metric, by name"`
	Instance  string    `expr:"instance" doc:"The sample's instance (an endpoint), empty when it has none"`
	Component Component `expr:"component" doc:"The component the sample is about"`
}

// TextInput is the prepared input for a property whose data type is string
// or json (json stays text for now).
type TextInput struct {
	Value     string    `expr:"value" doc:"The sample's value, typed by the key: a metric's number, a property's text, or a bool property's truth value"`
	Key       string    `expr:"key" doc:"The sample key the rule reads: a property or a metric, by name"`
	Instance  string    `expr:"instance" doc:"The sample's instance (an endpoint), empty when it has none"`
	Component Component `expr:"component" doc:"The component the sample is about"`
}

// BoolInput is the prepared input for a bool property.
type BoolInput struct {
	Value     bool      `expr:"value" doc:"The sample's value, typed by the key: a metric's number, a property's text, or a bool property's truth value"`
	Key       string    `expr:"key" doc:"The sample key the rule reads: a property or a metric, by name"`
	Instance  string    `expr:"instance" doc:"The sample's instance (an endpoint), empty when it has none"`
	Component Component `expr:"component" doc:"The component the sample is about"`
}

// The criteria site's environments: the criteria builtins, the criteria
// budget, a bool result.
var (
	metricEnv = expression.MustEnv[MetricInput](expression.Bool, expression.WithBuiltins(expression.CriteriaBuiltins()...))
	textEnv   = expression.MustEnv[TextInput](expression.Bool, expression.WithBuiltins(expression.CriteriaBuiltins()...))
	boolEnv   = expression.MustEnv[BoolInput](expression.Bool, expression.WithBuiltins(expression.CriteriaBuiltins()...))
)

// CriteriaEnv is the environment a criterion compiles against: the lane,
// and for a property its data type.
func CriteriaEnv(lane Lane, dataType string) expression.Env {
	switch {
	case lane == LaneMetric:
		return metricEnv
	case dataType == "bool":
		return boolEnv
	}
	return textEnv
}

// Sample is one sample as a rule judges it.
type Sample struct {
	Value     any
	Key       string
	Instance  string
	Component Component
}

// CriteriaError is a criterion refused at compile time, naming which
// (fire_criteria or clear_criteria); it wraps an *expression.Error, with
// its line and column, when the expression is what failed.
type CriteriaError struct {
	Field string
	// Short is true when the criterion was given as the short form {op,
	// value}, so the fault is the criteria's, not an expression's text.
	Short bool
	Err   error
}

func (e *CriteriaError) Error() string { return e.Field + ": " + e.Err.Error() }
func (e *CriteriaError) Unwrap() error { return e.Err }

// Judge is a rule's criteria compiled for its lane and data type: compile it
// once per rule version, then decide any number of samples.
type Judge struct {
	lane        Lane
	dataType    string
	fire, clear Criteria
	fireProg    *expression.Program
	clearProg   *expression.Program // nil: the negation of fire
}

var compiles atomic.Int64

// CompileCount is how many judges have been compiled, so a caller can prove
// it compiles once per rule and not per sample.
func CompileCount() int64 { return compiles.Load() }

// Compile lowers and compiles a rule's criteria. A nil clear clears on the
// negation of fire, !(fire).
func Compile(lane Lane, dataType string, fire Criteria, clear *Criteria) (*Judge, error) {
	compiles.Add(1)
	env := CriteriaEnv(lane, dataType)
	j := &Judge{lane: lane, dataType: dataType}
	var err error
	if j.fire, j.fireProg, err = compileOne(fire, lane, dataType, env, "fire_criteria"); err != nil {
		return nil, err
	}
	if clear != nil {
		if j.clear, j.clearProg, err = compileOne(*clear, lane, dataType, env, "clear_criteria"); err != nil {
			return nil, err
		}
	}
	return j, nil
}

func compileOne(c Criteria, lane Lane, dataType string, env expression.Env, field string) (Criteria, *expression.Program, error) {
	short := c.Expr == ""
	low, err := Lower(c, lane, dataType)
	if err != nil {
		return Criteria{}, nil, &CriteriaError{Field: field, Short: short, Err: err}
	}
	p, err := expression.Compile(low.Expr, env)
	if err != nil {
		return Criteria{}, nil, &CriteriaError{Field: field, Short: short, Err: err}
	}
	return low, p, nil
}

// Fire is the fire criterion, lowered.
func (j *Judge) Fire() Criteria { return j.fire }

// Clear is the clear criterion, lowered; nil clears on !(fire).
func (j *Judge) Clear() *Criteria {
	if j.clearProg == nil {
		return nil
	}
	c := j.clear
	return &c
}

// Decide is what a sample means for the rule: Fire when it meets the fire
// criteria, else Clear when it meets the clear criteria (absent: the
// negation of fire), else Hold. An error (a value its lane cannot read, a
// number that is no number, a run over its budget) is the caller's to hold
// on. Pure; sustains are the caller's concern.
func (j *Judge) Decide(s Sample) (Action, error) {
	in, err := j.input(s)
	if err != nil {
		return Hold, err
	}
	fires, err := run(j.fireProg, in)
	if err != nil {
		return Hold, err
	}
	if fires {
		return Fire, nil
	}
	if j.clearProg == nil {
		return Clear, nil
	}
	clears, err := run(j.clearProg, in)
	if err != nil {
		return Hold, err
	}
	if clears {
		return Clear, nil
	}
	return Hold, nil
}

func run(p *expression.Program, in any) (bool, error) {
	out, err := p.Run(in)
	if err != nil {
		return false, err
	}
	return out.(bool), nil
}

// input prepares a sample for the lane: a metric's value is a finite number
// (a number that is no number would meet neither a criterion nor its
// negation, so it holds); a bool property's is exactly true or false; any
// other property's is its text.
func (j *Judge) input(s Sample) (any, error) {
	switch {
	case j.lane == LaneMetric:
		f, err := number(s.Value)
		if err != nil {
			return nil, err
		}
		return MetricInput{Value: f, Key: s.Key, Instance: s.Instance, Component: s.Component}, nil
	case j.dataType == "bool":
		var b bool
		switch v := s.Value.(type) {
		case bool:
			b = v
		case string:
			switch v {
			case "true":
				b = true
			case "false":
			default:
				return nil, fmt.Errorf("sample value %q is not a bool (true or false)", v)
			}
		default:
			return nil, fmt.Errorf("sample value %v is not a bool", s.Value)
		}
		return BoolInput{Value: b, Key: s.Key, Instance: s.Instance, Component: s.Component}, nil
	}
	text, ok := s.Value.(string)
	if !ok {
		return nil, fmt.Errorf("a property compares text, not %T", s.Value)
	}
	return TextInput{Value: text, Key: s.Key, Instance: s.Instance, Component: s.Component}, nil
}

func number(v any) (float64, error) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case string:
		p, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, fmt.Errorf("sample value %q is not a number", x)
		}
		f = p
	default:
		return 0, fmt.Errorf("sample value %v is not a number", v)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("sample value is not a finite number")
	}
	return f, nil
}
