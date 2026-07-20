package key

import (
	"encoding/json"
	"testing"
)

func TestValidateKey(t *testing.T) {
	cases := []struct {
		name, key string
		ok        bool
	}{
		{"flat", "serial_number", true},
		{"dotted", "interface.reachable", true},
		{"multi_dot", "icmp.rtt_avg", true},
		{"digits", "tcp_open2", true},
		{"empty", "", false},
		{"leading_dot", ".a", false},
		{"trailing_dot", "a.", false},
		{"double_dot", "a..b", false},
		{"upper", "Serial_Number", false},
		{"space", "serial number", false},
		{"hyphen", "serial-number", false},
		{"leading_digit_segment", "9lives", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateKey(c.key)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("want error for %q", c.key)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	j := func(s string) json.RawMessage { return json.RawMessage(s) }
	cases := []struct {
		name, dataType, value, schema string
		ok                            bool
	}{
		{"string_ok", "string", `"SN-1"`, ``, true},
		{"int_ok", "int", `30`, ``, true},
		{"int_from_quoted_fails", "int", `"30"`, ``, false},
		{"bool_ok", "bool", `true`, ``, true},
		{"json_ok", "json", `{"a":1}`, ``, true},
		{"pattern_ok", "string", `"AB-12"`, `{"pattern":"^[A-Z]+-[0-9]+$"}`, true},
		{"pattern_fail", "string", `"ab"`, `{"pattern":"^[A-Z]+-[0-9]+$"}`, false},
		{"enum_ok", "string", `"warn"`, `{"enum":["info","warn"]}`, true},
		{"enum_fail", "string", `"bad"`, `{"enum":["info","warn"]}`, false},
		{"min_ok", "int", `10`, `{"minimum":5}`, true},
		{"min_fail", "int", `2`, `{"minimum":5}`, false},
		{"max_fail", "int", `99`, `{"maximum":10}`, false},
		// Nested-json: declare properties. Huma enforces `required` only for
		// DECLARED properties (a required key with no properties block is skipped),
		// so a real object schema always lists its properties.
		{"json_nested_ok", "json", `{"in":1,"out":2}`, `{"type":"object","required":["in","out"],"properties":{"in":{"type":"integer"},"out":{"type":"integer"}}}`, true},
		{"json_nested_missing_required", "json", `{"in":1}`, `{"type":"object","required":["in","out"],"properties":{"in":{"type":"integer"},"out":{"type":"integer"}}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sch json.RawMessage
			if c.schema != "" {
				sch = j(c.schema)
			}
			err := ValidateValue(c.dataType, j(c.value), sch)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("want error for %s/%s", c.value, c.schema)
			}
		})
	}
}
