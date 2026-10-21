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
	"strconv"
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
// rewrote the stored rules (db/migrations/20261021040000): the two write
// byte-identical source for every stored short form (TestCriteriaBackfill).
//
//   - A property compares text, eq or ne, against a JSON-quoted string
//     (value == "down") escaped exactly as Postgres's to_json escapes it,
//     which Expr reads back exactly.
//   - A bool property compares against true or false; a threshold that is
//     neither could never equal a parseable value, so eq lowers to false
//     and ne to true. This is the lowering's one divergence from the
//     short form's old evaluator, by decision (definition decision 3 on
//     #524): a bool sample that is not exactly true or false now holds the
//     rule, where the old evaluator compared its text.
//   - A metric compares a number with any operator, against its JSON text
//     written as Postgres writes a jsonb number (plain decimal, no exponent:
//     1e21 is 1000000000000000000000) and made a float literal (80 is 80.0),
//     so Expr parses the exact decimal with the same correctly rounded
//     ParseFloat the short form's json.Unmarshal used.
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
		num := decimal(string(raw))
		if !strings.Contains(num, ".") {
			num += ".0"
		}
		return Criteria{Expr: "value " + sym + " " + num}, nil
	}
	return Criteria{}, fmt.Errorf("unknown lane %q", lane)
}

// quote is s as Postgres's to_json writes it (escape_json): \" and \\, the
// short escapes \b \f \n \r \t, any other control character as \u00xx,
// and everything else as itself (U+2028 included). Every escape is one Expr
// reads, so Expr reads back s.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// decimal writes a JSON number as Postgres writes it once it is a jsonb
// number (numeric_out after numeric_in): a plain decimal with no exponent,
// keeping the input's scale less its exponent (1.50e-1 is 0.150, 1.50E1 is
// 15.0 before the float suffix, 80.50 stays 80.50), no sign on zero.
func decimal(num string) string {
	sign := ""
	if strings.HasPrefix(num, "-") {
		sign, num = "-", num[1:]
	}
	exp := 0
	if i := strings.IndexAny(num, "eE"); i >= 0 {
		e, err := strconv.Atoi(strings.TrimPrefix(num[i+1:], "+"))
		if err != nil {
			return sign + num
		}
		exp, num = e, num[:i]
	}
	intPart, frac, _ := strings.Cut(num, ".")
	digits := intPart + frac
	point := len(intPart) + exp
	scale := max(0, len(frac)-exp)
	var whole, fraction string
	switch {
	case point <= 0:
		whole, fraction = "0", strings.Repeat("0", -point)+digits
	case point >= len(digits):
		whole = digits + strings.Repeat("0", point-len(digits))
	default:
		whole, fraction = digits[:point], digits[point:]
	}
	if len(fraction) > scale {
		fraction = fraction[:scale]
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if strings.Trim(whole+fraction, "0") == "" {
		sign = ""
	}
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
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
