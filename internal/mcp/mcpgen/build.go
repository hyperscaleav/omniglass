// Package mcpgen builds the MCP tool catalog from the committed OpenAPI document
// (#1162, ADR-1162), the sibling of cligen: every operation stamped with the
// x-omniglass-mcp extension becomes one tool. cmd/mcpgen runs it under
// `make gen` and writes the Go catalog and the docs reference; the drift test in
// internal/mcp runs it again and compares, so the served catalog never differs
// from the spec.
package mcpgen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// methodOrder is the order a path's operations are visited in, so the catalog
// order is stable: sorted paths, then this.
var methodOrder = []string{"get", "post", "put", "patch", "delete"}

// refPrefix is the only reference shape openapigen writes.
const refPrefix = "#/components/schemas/"

// wholeBody is the one argument a body that is not an object of named
// properties travels in.
const wholeBody = "body"

type document struct {
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]map[string]any `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	OperationID string          `json:"operationId"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Permission  string          `json:"x-omniglass-permission"`
	Stamp       json.RawMessage `json:"x-omniglass-mcp"`
	Parameters  []struct {
		Name        string         `json:"name"`
		In          string         `json:"in"`
		Description string         `json:"description"`
		Required    bool           `json:"required"`
		Schema      map[string]any `json:"schema"`
	} `json:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Schema map[string]any `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
}

// Build reads an OpenAPI document and returns the tool for every stamped
// operation, in path then method order. What cannot be served honestly (an
// argument name used twice, a stamp the rule refuses, a required header, a
// dangling reference, no description) is an error naming the operation.
func Build(raw []byte) ([]tooldef.Def, error) {
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("mcpgen: parse spec: %w", err)
	}
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var defs []tooldef.Def
	for _, p := range paths {
		for _, m := range methodOrder {
			rawOp, ok := doc.Paths[p][m]
			if !ok {
				continue
			}
			var op operation
			if err := json.Unmarshal(rawOp, &op); err != nil {
				return nil, fmt.Errorf("mcpgen: %s %s: %w", strings.ToUpper(m), p, err)
			}
			if op.Stamp == nil {
				continue
			}
			d, err := buildOne(doc, p, strings.ToUpper(m), op)
			if err != nil {
				return nil, fmt.Errorf("mcpgen: %s: %w", op.OperationID, err)
			}
			defs = append(defs, d)
		}
	}
	return defs, nil
}

func buildOne(doc document, path, method string, op operation) (tooldef.Def, error) {
	if strings.TrimSpace(op.Description) == "" {
		return tooldef.Def{}, fmt.Errorf("a tool needs its operation's description, and it has none")
	}
	hints, err := tooldef.ParseHints(op.Stamp)
	if err != nil {
		return tooldef.Def{}, err
	}
	ann, err := tooldef.Annotate(method, op.Permission, hints)
	if err != nil {
		return tooldef.Def{}, err
	}
	d := tooldef.Def{
		Name: op.OperationID, Title: op.Summary, Description: op.Description,
		Method: method, Path: path, Permission: op.Permission, Annotations: ann,
	}
	props := map[string]any{}
	var required []string
	add := func(name string, schema any, req bool) error {
		if _, dup := props[name]; dup {
			return fmt.Errorf("argument %q is named twice (a path or query parameter and a body property share it)", name)
		}
		props[name] = schema
		if req {
			required = append(required, name)
		}
		return nil
	}
	for _, prm := range op.Parameters {
		switch prm.In {
		case "path", "query":
		case "header", "cookie":
			if prm.Required {
				return tooldef.Def{}, fmt.Errorf("required %s parameter %q cannot be an argument", prm.In, prm.Name)
			}
			continue
		default:
			return tooldef.Def{}, fmt.Errorf("parameter %q is in %q, which no tool argument carries", prm.Name, prm.In)
		}
		schema, err := inline(doc, prm.Schema, nil)
		if err != nil {
			return tooldef.Def{}, err
		}
		s, _ := schema.(map[string]any)
		if s == nil {
			s = map[string]any{}
		}
		if prm.Description != "" {
			s["description"] = prm.Description
		}
		if err := add(prm.Name, s, prm.Required || prm.In == "path"); err != nil {
			return tooldef.Def{}, err
		}
		if prm.In == "path" {
			d.PathParams = append(d.PathParams, prm.Name)
		} else {
			d.QueryParams = append(d.QueryParams, prm.Name)
		}
	}
	if op.RequestBody != nil {
		content, ok := op.RequestBody.Content["application/json"]
		if !ok {
			return tooldef.Def{}, fmt.Errorf("a request body that is not application/json cannot be an argument")
		}
		body, err := inline(doc, content.Schema, nil)
		if err != nil {
			return tooldef.Def{}, err
		}
		bodyMap, _ := body.(map[string]any)
		bodyProps, named := bodyMap["properties"].(map[string]any)
		if !named {
			if err := add(wholeBody, body, true); err != nil {
				return tooldef.Def{}, err
			}
			d.BodyParams = []string{wholeBody}
			d.BodyWhole = true
		} else {
			bodyRequired := map[string]bool{}
			if rs, ok := bodyMap["required"].([]any); ok {
				for _, r := range rs {
					if s, ok := r.(string); ok {
						bodyRequired[s] = true
					}
				}
			}
			for _, name := range sortedKeys(bodyProps) {
				if name == "$schema" {
					continue
				}
				if err := add(name, bodyProps[name], bodyRequired[name]); err != nil {
					return tooldef.Def{}, err
				}
				d.BodyParams = append(d.BodyParams, name)
			}
		}
	}
	sort.Strings(required)
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return tooldef.Def{}, err
	}
	d.InputSchema = string(b)
	return d, nil
}

// inline replaces every reference in a schema with a copy of its target, so a
// tool's schema stands alone, and drops a nested object's $schema property (the
// body echo Huma adds, never an input). A reference back into a schema already
// being inlined (a recursive type) becomes a plain object rather than looping.
func inline(doc document, v any, seen []string) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			name, found := strings.CutPrefix(ref, refPrefix)
			if !found {
				return nil, fmt.Errorf("reference %q is not to a component schema", ref)
			}
			for _, s := range seen {
				if s == name {
					return map[string]any{"type": "object"}, nil
				}
			}
			target, ok := doc.Components.Schemas[name]
			if !ok {
				return nil, fmt.Errorf("reference %q resolves to nothing", ref)
			}
			return inline(doc, target, append(seen, name))
		}
		out := make(map[string]any, len(x))
		for k, val := range x {
			if k == "properties" {
				if props, ok := val.(map[string]any); ok {
					cp := make(map[string]any, len(props))
					for pk, pv := range props {
						if pk == "$schema" {
							continue
						}
						iv, err := inline(doc, pv, seen)
						if err != nil {
							return nil, err
						}
						cp[pk] = iv
					}
					out[k] = cp
					continue
				}
			}
			iv, err := inline(doc, val, seen)
			if err != nil {
				return nil, err
			}
			out[k] = iv
		}
		if req, ok := out["required"].([]any); ok {
			kept := make([]any, 0, len(req))
			for _, r := range req {
				if r != "$schema" {
					kept = append(kept, r)
				}
			}
			out["required"] = kept
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			iv, err := inline(doc, val, seen)
			if err != nil {
				return nil, err
			}
			out[i] = iv
		}
		return out, nil
	default:
		return v, nil
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
