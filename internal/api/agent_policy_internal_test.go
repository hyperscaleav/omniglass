package api

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/mcp/mcpgen"
	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestOperationClassReadsTheRegisteredOperation: the class the agent policy
// applies comes from what the operation is registered as, its method, its gate,
// its second gate and its stamp, the same inputs the MCP catalog annotates from
// (#2063), so a tool's class and the server's refusal cannot disagree.
func TestOperationClassReadsTheRegisteredOperation(t *testing.T) {
	t.Parallel()
	ext := func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	for _, tc := range []struct {
		name string
		op   huma.Operation
		want tooldef.Class
	}{
		{"get", huma.Operation{Method: "GET", Extensions: ext("x-omniglass-permission", "component:read")}, tooldef.ClassRead},
		{"get with no gate", huma.Operation{Method: "GET"}, tooldef.ClassRead},
		{"post gated on read", huma.Operation{Method: "POST", Extensions: ext("x-omniglass-permission", "driver:read")}, tooldef.ClassRead},
		{"create", huma.Operation{Method: "POST", Extensions: ext("x-omniglass-permission", "component:create")}, tooldef.ClassWrite},
		{"stamped destructive post", huma.Operation{Method: "POST", Extensions: ext("x-omniglass-permission", "command:create", tooldef.Extension, tooldef.Hints{Destructive: true})}, tooldef.ClassDestructive},
		{"patch", huma.Operation{Method: "PATCH", Extensions: ext("x-omniglass-permission", "component:update")}, tooldef.ClassDestructive},
		{"delete", huma.Operation{Method: "DELETE", Extensions: ext("x-omniglass-permission", "component:delete")}, tooldef.ClassDestructive},
		{"second gate binds", huma.Operation{Method: "PATCH", Extensions: ext("x-omniglass-permission", "endpoint:update", "x-omniglass-conditional-permission", "secret:bind")}, tooldef.ClassCredentialDestination},
		{"unknown method", huma.Operation{Method: "TRACE"}, tooldef.ClassDestructive},
	} {
		if got := operationClass(&tc.op); got != tc.want {
			t.Errorf("%s: class %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestEveryToolsClassIsTheServersClass: the class a tool is listed with (the
// generated catalog's, built from the spec) is the class the server refuses it
// by (operationClass over the registered operation), for every tool, so an
// agent is never told always for a call the server then refuses.
func TestEveryToolsClassIsTheServersClass(t *testing.T) {
	t.Parallel()
	doc := specAPI(storage.UnimplementedGateway{}).OpenAPI()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := mcpgen.Build(raw)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]*huma.Operation{}
	for _, item := range doc.Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil {
				ops[op.OperationID] = op
			}
		}
	}
	if len(defs) == 0 {
		t.Fatal("the spec built no tools")
	}
	for _, d := range defs {
		op, ok := ops[d.Name]
		if !ok {
			t.Errorf("tool %s has no registered operation", d.Name)
			continue
		}
		if got := operationClass(op); got != d.Class {
			t.Errorf("tool %s is listed %s, refused as %s", d.Name, d.Class, got)
		}
	}
}

// Only an operation that would change the agent policy is refused to an agent
// regardless of class: the agent namespace's patch and delete, and the reset of
// every namespace.
func TestRewritesAgentPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		op, ns string
		want   bool
	}{
		{"patch-settings-namespace", "agent", true},
		{"delete-settings-namespace", "agent", true},
		{"restore-settings-defaults", "", true},
		{"patch-settings-namespace", "ui", false},
		{"delete-settings-namespace", "retention", false},
		{"get-settings", "", false},
		{"create-component", "agent", false},
	}
	for _, c := range cases {
		if got := rewritesAgentPolicy(c.op, c.ns); got != c.want {
			t.Errorf("rewritesAgentPolicy(%q, %q) = %v, want %v", c.op, c.ns, got, c.want)
		}
	}
}
