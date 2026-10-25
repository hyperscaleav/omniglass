package tooldef_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// Annotate is the one rule that says what an MCP client may assume about a tool
// (#1162): read-only from the HTTP method and the gate's action, destructive from
// an overwrite or a delete, idempotent where a repeat changes nothing more. A
// stamp's hints may only add caution.
func TestAnnotate(t *testing.T) {
	ro := tooldef.Annotations{ReadOnly: true, Idempotent: true}
	for _, c := range []struct {
		name       string
		method     string
		permission string
		hints      tooldef.Hints
		want       tooldef.Annotations
	}{
		{"a get is a read", "GET", "component:read", tooldef.Hints{}, ro},
		{"an authn-only get is a read", "GET", "", tooldef.Hints{}, ro},
		{"a get gated by another action still writes nothing", "GET", "principal:revoke-session", tooldef.Hints{}, ro},
		{"a post gated by a read is a read (a preview, a validate)", "POST", "rule:read", tooldef.Hints{}, ro},
		{"an admin-tier read post is a read", "POST", "secret:read:admin", tooldef.Hints{}, ro},
		{"a create adds and destroys nothing", "POST", "component:create", tooldef.Hints{}, tooldef.Annotations{}},
		{"an authn-only post is a write", "POST", "", tooldef.Hints{}, tooldef.Annotations{}},
		{"a patch overwrites", "PATCH", "endpoint:update", tooldef.Hints{}, tooldef.Annotations{Destructive: true}},
		{"a put overwrites and repeats to the same end", "PUT", "component:update", tooldef.Hints{}, tooldef.Annotations{Destructive: true, Idempotent: true}},
		{"a delete destroys and repeats to the same end", "DELETE", "endpoint:delete", tooldef.Hints{}, tooldef.Annotations{Destructive: true, Idempotent: true}},
		{"a stamp marks an acting post destructive", "POST", "command:issue", tooldef.Hints{Destructive: true}, tooldef.Annotations{Destructive: true}},
		{"a stamp marks a read that touches the network open-world", "GET", "node:read", tooldef.Hints{OpenWorld: true}, tooldef.Annotations{ReadOnly: true, Idempotent: true, OpenWorld: true}},
		{"a patch read-gated is still a write", "PATCH", "rule:read", tooldef.Hints{}, tooldef.Annotations{Destructive: true}},
		{"a delete read-gated is still a write", "DELETE", "rule:read", tooldef.Hints{}, tooldef.Annotations{Destructive: true, Idempotent: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := tooldef.Annotate(c.method, c.permission, c.hints)
			if err != nil {
				t.Fatalf("Annotate(%s, %q, %+v) error: %v", c.method, c.permission, c.hints, err)
			}
			if got != c.want {
				t.Errorf("Annotate(%s, %q, %+v) = %+v, want %+v", c.method, c.permission, c.hints, got, c.want)
			}
		})
	}
}

// A hint that contradicts what the method already says is refused, rather than
// silently ignored: a destructive read is a confusion in the stamp.
func TestAnnotateRefusesAContradiction(t *testing.T) {
	if _, err := tooldef.Annotate("GET", "component:read", tooldef.Hints{Destructive: true}); err == nil {
		t.Error("a destructive hint on a read was accepted")
	}
	if _, err := tooldef.Annotate("TRACE", "", tooldef.Hints{}); err == nil {
		t.Error("an unknown method was accepted")
	}
}

// A stamp is parsed strictly: the only keys are the caution hints, so a stamp
// cannot declare a mutating operation read-only, or anything else the rule does
// not know.
func TestParseHints(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want tooldef.Hints
		ok   bool
	}{
		{`{}`, tooldef.Hints{}, true},
		{`{"destructive":true}`, tooldef.Hints{Destructive: true}, true},
		{`{"open_world":true}`, tooldef.Hints{OpenWorld: true}, true},
		{`{"read_only":true}`, tooldef.Hints{}, false},
		{`{"destructive":false}`, tooldef.Hints{}, false},
		{`true`, tooldef.Hints{}, false},
	} {
		got, err := tooldef.ParseHints([]byte(c.raw))
		if (err == nil) != c.ok {
			t.Errorf("ParseHints(%s) err = %v, want ok=%v", c.raw, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseHints(%s) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}
