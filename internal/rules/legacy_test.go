package rules

// The {op,value} criteria as they were before #957, kept verbatim as the
// oracle the lowering is proven against: every existing rule must decide
// every sample identically before and after the backfill.

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// legacyCriteria is the placeholder criterion until the expression engine (#524)
// lands: one comparison of the arriving sample's value against a threshold.
// A property compares text (eq, ne); a metric compares numbers with any
// operator. Stored as jsonb, so the expression string drops in later
// without a schema change.
type legacyCriteria struct {
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

var legacyOps = map[string]bool{"eq": true, "ne": true, "gt": true, "ge": true, "lt": true, "le": true}

// Validate refuses a criterion the lane cannot evaluate.
func (c legacyCriteria) Validate(lane Lane) error {
	if !legacyOps[c.Op] {
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

func (c legacyCriteria) text() (string, bool) {
	var s string
	if err := json.Unmarshal(c.Value, &s); err != nil {
		return "", false
	}
	return s, true
}

func (c legacyCriteria) number() (float64, bool) {
	var f float64
	if err := json.Unmarshal(c.Value, &f); err != nil {
		return 0, false
	}
	return f, true
}

// Eval reports whether a sample value meets the criterion, judging it by the
// lane's type: a text criterion takes only a string (a property's value); a
// numeric one takes a float64 (a metric's value) or numeric text. A number
// that is no number (NaN, infinity) is refused: it would meet neither a
// criterion nor its negation, so it could open nothing and clear nothing.
func (c legacyCriteria) Eval(value any) (bool, error) {
	if want, ok := c.text(); ok {
		got, isText := value.(string)
		if !isText {
			return false, fmt.Errorf("a text criterion compares text, not %T", value)
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
	if math.IsNaN(got) || math.IsInf(got, 0) {
		return false, fmt.Errorf("sample value %v is not a finite number", got)
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
var legacyNegations = map[string]string{"eq": "ne", "ne": "eq", "gt": "le", "le": "gt", "ge": "lt", "lt": "ge"}

// Negate is the criterion that holds exactly when c does not: a rule with no
// clear criterion clears on the negation of its fire criterion.
func (c legacyCriteria) Negate() legacyCriteria {
	return legacyCriteria{Op: legacyNegations[c.Op], Value: c.Value}
}

// Decide is what a sample means for a rule: Fire when it meets the fire
// criteria, else Clear when it meets the clear criteria (nil: the negation
// of fire), else Hold. Pure; sustains are the caller's concern.
func legacyDecide(fire legacyCriteria, clear *legacyCriteria, value any) (Action, error) {
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
