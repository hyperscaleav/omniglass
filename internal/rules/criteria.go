// Package rules is the pure core of the event rule tier (#819): the criteria
// a rule fires and clears on, and (with the evaluator) the decision a sample
// makes against a rule. No I/O: the storage gateway reads and writes rules
// and alarms, and calls in here to decide.
package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Lane is which sample lane a rule's key reads: a property (a text value
// written on change) or a metric (a number).
type Lane string

const (
	LaneProperty Lane = "property"
	LaneMetric   Lane = "metric"
)

// Criteria is when a rule fires or clears: an expression (#957) over the
// criteria site's prepared input (judge.go), stored as {"expr": "..."}. The
// short form {op, value}, one comparison of the sample's value against a
// threshold, is still accepted on input (the API, the CLI, the console's
// builder) and lowered to its expression before it is stored or judged.
type Criteria struct {
	Expr  string          `json:"expr,omitempty"`
	Op    string          `json:"op,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// symbols is each short-form operator's expression operator.
var symbols = map[string]string{"eq": "==", "ne": "!=", "gt": ">", "ge": ">=", "lt": "<", "le": "<="}

// Lower turns a criterion into its expression form; an expression is
// returned as it is. Pure, and mirrored by the backfill migration that
// rewrote the stored rules (db/migrations/20261021040000), so the two
// produce the same source for every stored short form:
//
//   - a property compares text, eq or ne, against a JSON-quoted string
//     (value == "down"), which Expr reads back exactly;
//   - a bool property compares against true or false; a threshold that is
//     neither could never equal a parseable value, so eq lowers to false
//     and ne to true;
//   - a metric compares a number with any operator, against its JSON text
//     made a float literal (80 becomes 80.0), so Expr parses it with the same
//     correctly rounded ParseFloat the short form's json.Unmarshal used.
func Lower(c Criteria, lane Lane, dataType string) (Criteria, error) {
	if c.Expr != "" {
		if c.Op != "" || len(c.Value) > 0 {
			return Criteria{}, errors.New("a criterion is an expression (expr) or a comparison (op and value), not both")
		}
		return Criteria{Expr: c.Expr}, nil
	}
	sym, ok := symbols[c.Op]
	if !ok {
		return Criteria{}, fmt.Errorf("criteria op %q is not one of eq, ne, gt, ge, lt, le (or give an expression)", c.Op)
	}
	raw := bytes.TrimSpace(c.Value)
	if len(raw) == 0 || string(raw) == "null" {
		return Criteria{}, errors.New("criteria needs a value to compare against")
	}
	switch lane {
	case LaneProperty:
		if c.Op != "eq" && c.Op != "ne" {
			return Criteria{}, fmt.Errorf("a property compares text: only eq and ne, not %s", c.Op)
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return Criteria{}, errors.New("a property criterion compares against text")
		}
		if dataType == "bool" {
			if text != "true" && text != "false" {
				return Criteria{Expr: map[string]string{"eq": "false", "ne": "true"}[c.Op]}, nil
			}
			return Criteria{Expr: "value " + sym + " " + text}, nil
		}
		return Criteria{Expr: "value " + sym + " " + quote(text)}, nil
	case LaneMetric:
		var f float64
		if raw[0] == '"' || json.Unmarshal(raw, &f) != nil {
			return Criteria{Expr: ""}, errors.New("a metric criterion compares against a number")
		}
		num := string(raw)
		if !strings.ContainsAny(num, ".eE") {
			num += ".0"
		}
		return Criteria{Expr: "value " + sym + " " + num}, nil
	}
	return Criteria{}, fmt.Errorf("unknown lane %q", lane)
}

// quote is s as a JSON string literal, which Expr reads back as s: every
// escape JSON writes (\" \\ \n \r \t \b \f \uXXXX) is one Expr reads.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
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
