// Package variable is the pure core of the variable primitive: a variable is a
// typed, cascade-resolved free value (a macro). This file carries the value-type
// vocabulary and the application-level typing that validates a jsonb value
// against its declared type, with no I/O so it is unit-testable in isolation. The
// storage layer owns the cascade and the owner arc; this package owns "is this
// value the right shape for its type".
package variable

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ValueType is a variable's declared scalar type. The value itself is stored as
// jsonb; the type constrains what JSON shape is legal, validated on write.
type ValueType string

const (
	TypeString ValueType = "string"
	TypeInt    ValueType = "int"
	TypeFloat  ValueType = "float"
	TypeBool   ValueType = "bool"
	TypeJSON   ValueType = "json" // any valid JSON (object, array, or scalar)
)

// ValueTypes is the ordered set of legal value types, for validation and the
// create form's picker.
var ValueTypes = []ValueType{TypeString, TypeInt, TypeFloat, TypeBool, TypeJSON}

// Valid reports whether vt is a known value type.
func (vt ValueType) Valid() bool {
	for _, k := range ValueTypes {
		if vt == k {
			return true
		}
	}
	return false
}

// ParseValueType parses s into a ValueType or errors if it is not one of the
// known types.
func ParseValueType(s string) (ValueType, error) {
	vt := ValueType(s)
	if !vt.Valid() {
		return "", fmt.Errorf("variable: unknown value_type %q", s)
	}
	return vt, nil
}

// ValidateValue reports whether raw (a jsonb value) is well-formed for vt. A
// string must be a JSON string, an int a JSON integer (no fractional part), a
// float any JSON number, a bool a JSON boolean, and json any valid JSON. An
// empty or malformed value is always invalid.
func ValidateValue(vt ValueType, raw json.RawMessage) error {
	if !vt.Valid() {
		return fmt.Errorf("variable: unknown value_type %q", vt)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("variable: empty value for %s", vt)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("variable: value is not valid JSON")
	}
	switch vt {
	case TypeJSON:
		return nil // any valid JSON is legal
	case TypeString:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("variable: value is not a string")
		}
	case TypeBool:
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("variable: value is not a bool")
		}
	case TypeInt, TypeFloat:
		// Decode into any with UseNumber: a JSON number yields json.Number, a JSON
		// string yields a Go string, so a quoted "30" is correctly rejected.
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		if err := d.Decode(&v); err != nil {
			return fmt.Errorf("variable: value is not a number")
		}
		n, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("variable: value is not a number")
		}
		if vt == TypeInt {
			if _, err := n.Int64(); err != nil {
				return fmt.Errorf("variable: value is not an integer")
			}
		} else if _, err := n.Float64(); err != nil {
			return fmt.Errorf("variable: value is not a number")
		}
	}
	return nil
}
