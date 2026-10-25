package mcpgen_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/mcp/mcpgen"
	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// specDoc is a small OpenAPI document in the shape openapigen writes: one
// stamped list with a query parameter, one stamped write with a path parameter
// and a body by reference (whose property refers to a second schema), and one
// operation left unstamped.
const specDoc = `{
  "servers": [{"url": "/api/v1"}],
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "list-widgets", "summary": "List widgets", "description": "Every widget. Gated by widget:read.",
        "x-omniglass-permission": "widget:read", "x-omniglass-mcp": {},
        "parameters": [{"name": "limit", "in": "query", "description": "Max rows", "schema": {"type": "integer", "minimum": 1}}]
      },
      "post": {
        "operationId": "create-widget", "summary": "Create a widget", "description": "Not stamped, so not a tool.",
        "x-omniglass-permission": "widget:create"
      }
    },
    "/widgets/{name}": {
      "patch": {
        "operationId": "update-widget", "summary": "Update a widget", "description": "Edit one. Gated by widget:update.",
        "x-omniglass-permission": "widget:update", "x-omniglass-mcp": {},
        "parameters": [{"name": "name", "in": "path", "required": true, "description": "The widget's name", "schema": {"type": "string", "examples": ["dial-1"]}}],
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/UpdateWidgetBody"}}}}
      }
    }
  },
  "components": {"schemas": {
    "UpdateWidgetBody": {
      "type": "object", "additionalProperties": false, "required": ["label"],
      "properties": {
        "$schema": {"type": "string", "readOnly": true},
        "label": {"type": "string", "description": "The display label", "maxLength": 100},
        "dims": {"$ref": "#/components/schemas/Dims"}
      }
    },
    "Dims": {"type": "object", "properties": {"w": {"type": "integer"}, "h": {"type": "integer"}}}
  }}
}`

func build(t *testing.T, doc string) []tooldef.Def {
	t.Helper()
	defs, err := mcpgen.Build([]byte(doc))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return defs
}

// Only stamped operations become tools, in path then method order, each
// carrying its route, gate and annotations.
func TestBuildTakesOnlyStampedOperations(t *testing.T) {
	defs := build(t, specDoc)
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	if !reflect.DeepEqual(names, []string{"list-widgets", "update-widget"}) {
		t.Fatalf("tools = %v, want the two stamped operations", names)
	}
	list, upd := defs[0], defs[1]
	if list.Method != "GET" || list.Path != "/widgets" || list.Permission != "widget:read" || list.Title != "List widgets" {
		t.Errorf("list tool = %+v", list)
	}
	if !list.Annotations.ReadOnly || upd.Annotations.ReadOnly || !upd.Annotations.Destructive {
		t.Errorf("annotations: list %+v, update %+v", list.Annotations, upd.Annotations)
	}
}

// A tool's arguments are its path parameters, query parameters and body
// properties in one object, references inlined and the body's $schema dropped,
// and each argument is sorted onto the request part it came from.
func TestBuildMergesArgumentsIntoOneSchema(t *testing.T) {
	upd := build(t, specDoc)[1]
	if !reflect.DeepEqual(upd.PathParams, []string{"name"}) || !reflect.DeepEqual(upd.BodyParams, []string{"dims", "label"}) || len(upd.QueryParams) != 0 {
		t.Fatalf("parts: path %v query %v body %v", upd.PathParams, upd.QueryParams, upd.BodyParams)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(upd.InputSchema), &schema); err != nil {
		t.Fatalf("schema is not JSON: %v (%s)", err, upd.InputSchema)
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Errorf("schema root = %v, want a closed object", schema)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["$schema"]; ok {
		t.Error("the body's $schema leaked into the arguments")
	}
	name := props["name"].(map[string]any)
	if name["type"] != "string" || name["description"] != "The widget's name" {
		t.Errorf("path argument = %v", name)
	}
	dims := props["dims"].(map[string]any)
	if dims["type"] != "object" || dims["properties"] == nil {
		t.Errorf("a referenced body property was not inlined: %v", dims)
	}
	if strings.Contains(upd.InputSchema, "$ref") {
		t.Errorf("schema still carries a reference: %s", upd.InputSchema)
	}
	req := schema["required"].([]any)
	if !reflect.DeepEqual(req, []any{"label", "name"}) {
		t.Errorf("required = %v, want label and name", req)
	}
	list := build(t, specDoc)[0]
	if !reflect.DeepEqual(list.QueryParams, []string{"limit"}) {
		t.Errorf("list query params = %v", list.QueryParams)
	}
}

// What the generator cannot honestly turn into a tool is an error that names
// the operation, never a tool that half works.
func TestBuildRefusesWhatItCannotServe(t *testing.T) {
	for name, mutate := range map[string]func(string) string{
		"an argument name used twice": func(s string) string {
			return strings.Replace(s, `"label": {"type": "string"`, `"name": {"type": "string"`, 1)
		},
		"a destructive hint on a read": func(s string) string {
			return strings.Replace(s, `"x-omniglass-permission": "widget:read", "x-omniglass-mcp": {}`, `"x-omniglass-permission": "widget:read", "x-omniglass-mcp": {"destructive": true}`, 1)
		},
		"an unknown hint": func(s string) string {
			return strings.Replace(s, `"x-omniglass-mcp": {},`, `"x-omniglass-mcp": {"read_only": true},`, 1)
		},
		"a stamped operation with no description": func(s string) string {
			return strings.Replace(s, `"description": "Every widget. Gated by widget:read."`, `"description": ""`, 1)
		},
		"a required header parameter": func(s string) string {
			return strings.Replace(s, `{"name": "limit", "in": "query"`, `{"name": "X-Thing", "in": "header", "required": true`, 1)
		},
		"a reference that resolves to nothing": func(s string) string {
			return strings.Replace(s, `"#/components/schemas/Dims"`, `"#/components/schemas/Missing"`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := mcpgen.Build([]byte(mutate(specDoc))); err == nil {
				t.Errorf("Build accepted %s", name)
			}
		})
	}
}

// A body that is not an object of named properties (a free-form merge patch) is
// carried whole as one argument, body.
func TestBuildCarriesAFreeFormBodyWhole(t *testing.T) {
	doc := strings.Replace(specDoc, `"schema": {"$ref": "#/components/schemas/UpdateWidgetBody"}`, `"schema": {"type": "object", "additionalProperties": {}, "description": "A merge patch"}`, 1)
	upd := build(t, doc)[1]
	if !reflect.DeepEqual(upd.BodyParams, []string{"body"}) {
		t.Fatalf("body params = %v, want the whole body as one argument", upd.BodyParams)
	}
	if !strings.Contains(upd.InputSchema, `"body"`) {
		t.Errorf("schema = %s", upd.InputSchema)
	}
}

// The Go catalog renders as gofmt-clean source that names every tool, and the
// docs page lists every tool with its gate and what a client may assume of it.
func TestRenderGoAndDoc(t *testing.T) {
	defs := build(t, specDoc)
	src, err := mcpgen.RenderGo(defs)
	if err != nil {
		t.Fatalf("RenderGo: %v", err)
	}
	for _, want := range []string{"DO NOT EDIT", "package mcp", `Name:        "list-widgets"`, `Name:        "update-widget"`, `PathParams:  []string{"name"}`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("Go catalog lacks %q:\n%s", want, src)
		}
	}
	doc := string(mcpgen.RenderDoc(defs))
	for _, want := range []string{"title: MCP tools", "Do not edit by hand", "## `list-widgets`", "`GET /api/v1/widgets`", "`widget:update`", "| `label` | string | yes | The display label |", "read-only", "destructive"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs page lacks %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "create-widget") {
		t.Error("the docs page lists an unstamped operation")
	}
}

// A description carrying a pipe would split a table row; the page escapes it.
func TestRenderDocEscapesTableCells(t *testing.T) {
	doc := strings.Replace(specDoc, `"description": "The display label"`, `"description": "on | off"`, 1)
	page := string(mcpgen.RenderDoc(build(t, doc)))
	if !strings.Contains(page, `on \| off`) {
		t.Errorf("pipe not escaped:\n%s", page)
	}
}
