package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBodyFieldsJSON asserts that a body property that is not a plain scalar (an
// untyped `any` value, an object) is marked JSON so its flag string is parsed as
// JSON before it enters the request body, while a scalar (string) passes through.
// This is the fix that lets `variable create --value 30` send the number 30 and
// `secret create --fields '{...}'` send the object, not their quoted-string forms.
func TestBodyFieldsJSON(t *testing.T) {
	const docRaw = `{"components":{"schemas":{"CreateBody":{
	  "required":["name","value"],
	  "properties":{
	    "name":{"type":"string"},
	    "value":{"description":"the value, any shape"},
	    "fields":{"type":"object"},
	    "propagates":{"type":"boolean"}
	  }}}}}`
	const opRaw = `{"operationId":"create-thing","requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/CreateBody"}}}}}`
	var doc spec
	if err := json.Unmarshal([]byte(docRaw), &doc); err != nil {
		t.Fatalf("doc: %v", err)
	}
	var op operation
	if err := json.Unmarshal([]byte(opRaw), &op); err != nil {
		t.Fatalf("op: %v", err)
	}

	byName := map[string]bodyField{}
	for _, f := range bodyFields(doc, op) {
		byName[f.Name] = f
	}
	if byName["name"].JSON {
		t.Errorf("string field name should not be JSON: %+v", byName["name"])
	}
	if !byName["value"].JSON {
		t.Errorf("untyped value field should be JSON: %+v", byName["value"])
	}
	if !byName["fields"].JSON {
		t.Errorf("object field should be JSON: %+v", byName["fields"])
	}
	if !byName["propagates"].JSON {
		t.Errorf("boolean field should be JSON so it serializes as a bool, not a string: %+v", byName["propagates"])
	}

	// The rendered source parses the JSON fields and passes the scalar through.
	cmd := buildCommand(doc, "/api/v1", "/things", "post", op)
	out, err := render(group([]command{cmd}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	src := string(out)
	for _, want := range []string{
		`body["value"] = jsonOrString(fValue)`,
		`body["fields"] = jsonOrString(fFields)`,
		`body["propagates"] = jsonOrString(fPropagates)`,
		`body["name"] = fName`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q", want)
		}
	}
}

// TestBuildCommandPathParams asserts that the generated request path substitutes
// path parameters as %s slots and binds them as positional args, in path order,
// for regular item paths, nested collections, and AIP :verb custom methods (the
// last is the regression: a {id}:archive segment must still bind the id).
func TestBuildCommandPathParams(t *testing.T) {
	const base = "/api/v1"
	cases := []struct {
		name     string
		path     string
		method   string
		wantPath string
		wantArgs []string
	}{
		{"item get", "/principals/{id}", "get", "/api/v1/principals/%s", []string{"id"}},
		{"collection list", "/principals", "get", "/api/v1/principals", nil},
		{"nested collection", "/principals/{id}/grants", "get", "/api/v1/principals/%s/grants", []string{"id"}},
		{"custom method archive", "/principals/{id}:archive", "post", "/api/v1/principals/%s:archive", []string{"id"}},
		{"custom method resetPassword", "/principals/{id}:resetPassword", "post", "/api/v1/principals/%s:resetPassword", []string{"id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := buildCommand(spec{}, base, tc.path, tc.method, operation{})
			if cmd.APIPath != tc.wantPath {
				t.Errorf("APIPath = %q, want %q", cmd.APIPath, tc.wantPath)
			}
			if strings.Contains(cmd.APIPath, "{") {
				t.Errorf("APIPath %q still holds a literal path parameter", cmd.APIPath)
			}
			if len(cmd.Args) != len(tc.wantArgs) {
				t.Fatalf("Args = %v, want %v", cmd.Args, tc.wantArgs)
			}
			for i, a := range tc.wantArgs {
				if cmd.Args[i] != a {
					t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], a)
				}
			}
		})
	}
}

// TestBuildCommandQueryParams asserts that an operation's OpenAPI query
// parameters become optional cobra flags: snake_case names map to kebab-case
// flags, the OpenAPI type picks the flag setter, and a required param is marked
// required. The principal-list op (kind + include_archived) is the worked case.
func TestBuildCommandQueryParams(t *testing.T) {
	op := operation{Parameters: []param{
		{Name: "kind", In: "query", Description: "Optionally filter by principal kind", Schema: paramSchema{Type: "string"}},
		{Name: "include_archived", In: "query", Description: "Include archived principals", Schema: paramSchema{Type: "boolean"}},
		{Name: "page", In: "query", Required: true, Schema: paramSchema{Type: "integer"}},
		{Name: "id", In: "path", Schema: paramSchema{Type: "string"}}, // must be ignored
	}}
	cmd := buildCommand(spec{}, "/api/v1", "/principals", "get", op)

	if len(cmd.Query) != 3 {
		t.Fatalf("Query = %+v, want 3 query fields (path param excluded)", cmd.Query)
	}
	byFlag := map[string]queryField{}
	for _, q := range cmd.Query {
		byFlag[q.Flag] = q
	}

	kind, ok := byFlag["kind"]
	if !ok {
		t.Fatalf("missing --kind flag, got %+v", cmd.Query)
	}
	if kind.Name != "kind" || kind.FlagFunc != "StringVar" || kind.GoType != "string" || kind.Required {
		t.Errorf("kind flag = %+v, want string StringVar not required", kind)
	}

	ia, ok := byFlag["include-archived"]
	if !ok {
		t.Fatalf("missing --include-archived flag, got %+v", cmd.Query)
	}
	if ia.Name != "include_archived" || ia.FlagFunc != "BoolVar" || ia.GoType != "bool" {
		t.Errorf("include-archived flag = %+v, want bool BoolVar", ia)
	}

	page, ok := byFlag["page"]
	if !ok {
		t.Fatalf("missing --page flag, got %+v", cmd.Query)
	}
	if page.FlagFunc != "IntVar" || page.GoType != "int" || !page.Required {
		t.Errorf("page flag = %+v, want int IntVar required", page)
	}
}

// TestRenderQueryFlags asserts the generated Go source for an op with a query
// parameter both declares the flag and appends the set value to the request
// query string.
func TestRenderQueryFlags(t *testing.T) {
	op := operation{
		OperationID: "list-principals",
		Summary:     "List principals",
		Parameters: []param{
			{Name: "include_archived", In: "query", Description: "Include archived principals", Schema: paramSchema{Type: "boolean"}},
		},
	}
	cmd := buildCommand(spec{}, "/api/v1", "/principals", "get", op)
	out, err := render(group([]command{cmd}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	src := string(out)
	for _, want := range []string{
		`cmd.Flags().BoolVar(&qIncludeArchived, "include-archived", false,`,
		`if cmd.Flags().Changed("include-archived") {`,
		`q.Set("include_archived", fmt.Sprintf("%v", qIncludeArchived))`,
		`path += "?" + enc`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q\n---\n%s", want, src)
		}
	}
}
