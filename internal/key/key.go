// Package key is the canonical keyspace primitive: the shared name-format rule and
// the typed value validator that every registered key obeys. Pure, no I/O.
package key

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/variable"
	"gopkg.in/yaml.v3"
)

// MaxKeyLen bounds a canonical key name.
const MaxKeyLen = 128

// segment is one dot-separated component of a key: a lowercase identifier.
var segment = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateKey accepts a lowercase snake_case key, optionally dot-hierarchied
// (serial_number, interface.reachable). Each dot segment is a lowercase identifier
// (a leading letter, then lowercase letters, digits, or underscores).
func ValidateKey(name string) error {
	if name == "" {
		return fmt.Errorf("key: name is empty")
	}
	if len(name) > MaxKeyLen {
		return fmt.Errorf("key: name exceeds %d characters", MaxKeyLen)
	}
	for _, seg := range strings.Split(name, ".") {
		if !segment.MatchString(seg) {
			return fmt.Errorf("key: %q must be lowercase dot-separated identifiers (a leading letter, then lowercase letters, digits, or underscores)", name)
		}
	}
	return nil
}

// Canonicalize trims and lowercases a key name. No alias resolution this slice.
func Canonicalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// ValidateValue coerces raw to dataType (the base type) and validates it against
// the key's JSON Schema fragment (pattern/enum/minimum/maximum and, for a json key,
// nested object/array schemas). Validation runs through Huma's own validator (a core
// dependency); the stored schema is yaml-loaded because huma.Schema is yaml-tagged
// and JSON is a subset of YAML. No new dependency.
func ValidateValue(dataType string, raw, validation json.RawMessage) error {
	if err := variable.ValidateValue(variable.ValueType(dataType), raw); err != nil {
		return err
	}
	if len(bytes.TrimSpace(validation)) == 0 {
		return nil
	}
	schema, err := buildSchema(dataType, validation)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("key: value is not valid JSON: %w", err)
	}
	res := &huma.ValidateResult{}
	huma.Validate(
		huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer),
		schema, huma.NewPathBuffer([]byte("value"), 0), huma.ModeWriteToServer, value, res,
	)
	if len(res.Errors) > 0 {
		return fmt.Errorf("key: %v", res.Errors[0])
	}
	return nil
}

// buildSchema loads the stored JSON Schema fragment into a huma.Schema (via yaml,
// since huma.Schema is yaml-tagged and JSON is a YAML subset), pins the base type
// from dataType (json keeps whatever type the fragment declares), and precomputes
// the validator's cached messages.
func buildSchema(dataType string, validation json.RawMessage) (*huma.Schema, error) {
	schema := &huma.Schema{}
	if err := yaml.Unmarshal(validation, schema); err != nil {
		return nil, fmt.Errorf("key: invalid validation schema: %w", err)
	}
	if t := humaType(dataType); t != "" {
		schema.Type = t
	}
	schema.PrecomputeMessages()
	return schema, nil
}

// humaType maps a key data_type to the JSON Schema base type. json returns "" so the
// stored fragment's own type (object/array/...) governs.
func humaType(dataType string) string {
	switch dataType {
	case "int":
		return "integer"
	case "float":
		return "number"
	case "bool":
		return "boolean"
	case "json":
		return ""
	default:
		return "string"
	}
}
