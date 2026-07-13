package variable_test

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/variable"
)

func TestParseValueType(t *testing.T) {
	for _, s := range []string{"string", "int", "float", "bool", "json"} {
		vt, err := variable.ParseValueType(s)
		if err != nil {
			t.Errorf("ParseValueType(%q) errored: %v", s, err)
		}
		if string(vt) != s {
			t.Errorf("ParseValueType(%q) = %q", s, vt)
		}
	}
	if _, err := variable.ParseValueType("date"); err == nil {
		t.Error("ParseValueType(date) should error on an unknown type")
	}
	if _, err := variable.ParseValueType(""); err == nil {
		t.Error("ParseValueType(empty) should error")
	}
}

func TestValidateValue(t *testing.T) {
	cases := []struct {
		name string
		vt   variable.ValueType
		raw  string
		ok   bool
	}{
		// string
		{"string ok", variable.TypeString, `"HDMI1"`, true},
		{"string rejects number", variable.TypeString, `30`, false},
		{"string rejects bool", variable.TypeString, `true`, false},
		// int
		{"int ok", variable.TypeInt, `30`, true},
		{"int ok negative", variable.TypeInt, `-5`, true},
		{"int rejects fraction", variable.TypeInt, `1.5`, false},
		{"int rejects string", variable.TypeInt, `"30"`, false},
		// float
		{"float ok fraction", variable.TypeFloat, `1.5`, true},
		{"float ok integral", variable.TypeFloat, `30`, true},
		{"float rejects string", variable.TypeFloat, `"1.5"`, false},
		// bool
		{"bool ok true", variable.TypeBool, `true`, true},
		{"bool ok false", variable.TypeBool, `false`, true},
		{"bool rejects number", variable.TypeBool, `1`, false},
		// json
		{"json ok object", variable.TypeJSON, `{"a":1}`, true},
		{"json ok array", variable.TypeJSON, `[1,2,3]`, true},
		{"json ok scalar string", variable.TypeJSON, `"x"`, true},
		{"json rejects malformed", variable.TypeJSON, `{bad`, false},
		// empty raw is always invalid
		{"empty raw", variable.TypeString, ``, false},
		// malformed json for a scalar type
		{"malformed string", variable.TypeString, `"unterminated`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := variable.ValidateValue(c.vt, json.RawMessage(c.raw))
			if c.ok && err != nil {
				t.Errorf("ValidateValue(%s, %s) = %v, want ok", c.vt, c.raw, err)
			}
			if !c.ok && err == nil {
				t.Errorf("ValidateValue(%s, %s) = ok, want error", c.vt, c.raw)
			}
		})
	}
}

func TestValidateValueUnknownType(t *testing.T) {
	if err := variable.ValidateValue(variable.ValueType("date"), json.RawMessage(`"x"`)); err == nil {
		t.Error("ValidateValue with an unknown type should error")
	}
}
