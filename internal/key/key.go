// Package key is the typed value validator that every registered key obeys. Pure,
// no I/O.
//
// The name-format rule used to live here too, and it is gone: it was one of four
// copies of the same character set, and a caller picked between them by hand. The
// rule now lives in storage.ValidateName, selected by the table's declared identity
// shape rather than by whoever wrote the call site. This package keeps only the
// value half, which is a different subject.
package key

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/variable"
	"gopkg.in/yaml.v3"
)

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

// ValidateSchema refuses a JSON Schema fragment that means less than it says: a
// required list naming a property absent from the fragment's properties block is
// silently unenforced (huma's validator consults required only while iterating a
// node's declared properties), so such a fragment would be stored and never
// enforce that name. Checked at every nesting level the validator recurses into
// (properties, items, allOf/anyOf/oneOf, not, and an object-valued
// additionalProperties), with the offending name in the
// error. An empty fragment is fine: no schema, nothing to lie.
func ValidateSchema(fragment json.RawMessage) error {
	if len(bytes.TrimSpace(fragment)) == 0 {
		return nil
	}
	schema := &huma.Schema{}
	if err := yaml.Unmarshal(fragment, schema); err != nil {
		return fmt.Errorf("key: invalid schema: %w", err)
	}
	if err := normalizeAdditional(schema); err != nil {
		return err
	}
	return checkRequiredDeclared(schema)
}

// checkRequiredDeclared walks the schema tree the same way the validator does. A
// node's required list is enforced only over that node's own properties, so a
// name required at one level and declared at another still enforces nothing and
// is refused where it stands.
func checkRequiredDeclared(s *huma.Schema) error {
	if s == nil {
		return nil
	}
	for _, name := range s.Required {
		if _, ok := s.Properties[name]; !ok {
			return fmt.Errorf("key: schema requires property %q but does not declare it under properties", name)
		}
	}
	for _, sub := range s.Properties {
		if err := checkRequiredDeclared(sub); err != nil {
			return err
		}
	}
	for _, list := range [][]*huma.Schema{s.AllOf, s.AnyOf, s.OneOf} {
		for _, sub := range list {
			if err := checkRequiredDeclared(sub); err != nil {
				return err
			}
		}
	}
	if err := checkRequiredDeclared(s.Items); err != nil {
		return err
	}
	// additionalProperties may itself be a schema (huma types it any, since JSON
	// Schema also allows a bare boolean there), and the validator recurses into
	// it, so the checker must too or a bare-required nested there slips through.
	if sub, ok := s.AdditionalProperties.(*huma.Schema); ok {
		if err := checkRequiredDeclared(sub); err != nil {
			return err
		}
	}
	return checkRequiredDeclared(s.Not)
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
	if err := normalizeAdditional(schema); err != nil {
		return nil, err
	}
	if t := humaType(dataType); t != "" {
		schema.Type = t
	}
	schema.PrecomputeMessages()
	return schema, nil
}

// normalizeAdditional converts an object-valued additionalProperties into a real
// sub-schema everywhere in the tree. huma types the field `any` (JSON Schema also
// allows a bare boolean there) and has no custom unmarshaler, so a STORED
// fragment's object lands as a plain map, and the validator's own
// `.(*Schema)` assertion then skips it silently: the stored constraint would be
// entirely inert at runtime. Re-parsing the subtree into a Schema makes the
// validator enforce it and makes the bare-required check reach it.
func normalizeAdditional(s *huma.Schema) error {
	if s == nil {
		return nil
	}
	if m, ok := s.AdditionalProperties.(map[string]any); ok {
		raw, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("key: invalid additionalProperties schema: %w", err)
		}
		sub := &huma.Schema{}
		if err := yaml.Unmarshal(raw, sub); err != nil {
			return fmt.Errorf("key: invalid additionalProperties schema: %w", err)
		}
		s.AdditionalProperties = sub
	}
	if sub, ok := s.AdditionalProperties.(*huma.Schema); ok {
		if err := normalizeAdditional(sub); err != nil {
			return err
		}
	}
	for _, sub := range s.Properties {
		if err := normalizeAdditional(sub); err != nil {
			return err
		}
	}
	for _, list := range [][]*huma.Schema{s.AllOf, s.AnyOf, s.OneOf} {
		for _, sub := range list {
			if err := normalizeAdditional(sub); err != nil {
				return err
			}
		}
	}
	if err := normalizeAdditional(s.Items); err != nil {
		return err
	}
	return normalizeAdditional(s.Not)
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
