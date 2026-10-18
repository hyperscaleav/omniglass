// Package rules is the pure core of the event rule tier (#819): the criteria
// a rule fires and clears on, and (with the evaluator) the decision a sample
// makes against a rule. No I/O: the storage gateway reads and writes rules
// and alarms, and calls in here to decide.
package rules

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Lane is which sample lane a rule's key reads: a property (a text value
// written on change) or a metric (a number).
type Lane string

const (
	LaneProperty Lane = "property"
	LaneMetric   Lane = "metric"
)

// Criteria is the placeholder criterion until the expression engine (#524)
// lands: one comparison of the arriving sample's value against a threshold.
// A property compares text (eq, ne); a metric compares numbers with any
// operator. Stored as jsonb, so the expression string drops in later
// without a schema change.
type Criteria struct {
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

var ops = map[string]bool{"eq": true, "ne": true, "gt": true, "ge": true, "lt": true, "le": true}

// Validate refuses a criterion the lane cannot evaluate.
func (c Criteria) Validate(lane Lane) error {
	if !ops[c.Op] {
		return fmt.Errorf("criteria op %q is not one of eq, ne, gt, ge, lt, le", c.Op)
	}
	if len(c.Value) == 0 || string(c.Value) == "null" {
		return fmt.Errorf("criteria needs a value to compare against")
	}
	switch lane {
	case LaneProperty:
		if c.Op != "eq" && c.Op != "ne" {
			return fmt.Errorf("a property compares text: only eq and ne, not %s", c.Op)
		}
		if _, ok := c.text(); !ok {
			return fmt.Errorf("a property criterion compares against text")
		}
	case LaneMetric:
		if _, ok := c.number(); !ok {
			return fmt.Errorf("a metric criterion compares against a number")
		}
	default:
		return fmt.Errorf("unknown lane %q", lane)
	}
	return nil
}

func (c Criteria) text() (string, bool) {
	var s string
	if err := json.Unmarshal(c.Value, &s); err != nil {
		return "", false
	}
	return s, true
}

func (c Criteria) number() (float64, bool) {
	var f float64
	if err := json.Unmarshal(c.Value, &f); err != nil {
		return 0, false
	}
	return f, true
}

// Eval reports whether a sample value meets the criterion: a string for a
// property, a float64 (or a numeric string) for a metric.
func (c Criteria) Eval(value any) (bool, error) {
	if want, ok := c.text(); ok {
		got, isText := value.(string)
		if !isText {
			got = fmt.Sprint(value)
		}
		switch c.Op {
		case "eq":
			return got == want, nil
		case "ne":
			return got != want, nil
		}
		return false, fmt.Errorf("a text criterion has no %s", c.Op)
	}
	want, ok := c.number()
	if !ok {
		return false, fmt.Errorf("criteria value is neither text nor a number")
	}
	var got float64
	switch v := value.(type) {
	case float64:
		got = v
	case int:
		got = float64(v)
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return false, fmt.Errorf("sample value %q is not a number", v)
		}
		got = f
	default:
		return false, fmt.Errorf("sample value %v is not a number", value)
	}
	switch c.Op {
	case "eq":
		return got == want, nil
	case "ne":
		return got != want, nil
	case "gt":
		return got > want, nil
	case "ge":
		return got >= want, nil
	case "lt":
		return got < want, nil
	case "le":
		return got <= want, nil
	}
	return false, fmt.Errorf("unknown op %q", c.Op)
}

// negations maps each operator to the one that holds exactly when it does not.
var negations = map[string]string{"eq": "ne", "ne": "eq", "gt": "le", "le": "gt", "ge": "lt", "lt": "ge"}

// Negate is the criterion that holds exactly when c does not: a rule with no
// clear criterion clears on the negation of its fire criterion.
func (c Criteria) Negate() Criteria {
	return Criteria{Op: negations[c.Op], Value: c.Value}
}

// Action is what one sample means for a rule's alarm on its owner.
type Action int

const (
	// Hold changes nothing: the sample meets neither fire nor clear.
	Hold Action = iota
	// Fire opens the alarm (a no-op when it is already open).
	Fire
	// Clear clears the alarm (a no-op when none is open).
	Clear
)

// Decide is what a sample means for a rule: Fire when it meets the fire
// criteria, else Clear when it meets the clear criteria (nil: the negation
// of fire), else Hold. Pure; sustains are the caller's concern.
func Decide(fire Criteria, clear *Criteria, value any) (Action, error) {
	fires, err := fire.Eval(value)
	if err != nil {
		return Hold, err
	}
	if fires {
		return Fire, nil
	}
	c := fire.Negate()
	if clear != nil {
		c = *clear
	}
	clears, err := c.Eval(value)
	if err != nil {
		return Hold, err
	}
	if clears {
		return Clear, nil
	}
	return Hold, nil
}
