package sandbox

import (
	"testing"
	"text/template"
)

// The invariants that let the function set be generated at all (#701), lifted
// with the engine (#618): a name is usable only if the FuncMap defines it AND
// the grammar admits it.

// TestAllowlistIsTheFuncMapPlusTheDocumentedBuiltins is the safety property
// ADR-0098 argues for, stated as a test rather than as a habit: a name is usable
// only if the FuncMap defines it AND the grammar admits it, and the two sets may
// differ only by the builtins the parser would accept whatever the FuncMap says.
//
// It is checked in BOTH directions, which the transcribed version never was. A
// FuncMap entry the allowlist omits parses nowhere (a function published and
// unreachable); an allowlist entry the FuncMap omits is a hole that widens the
// grammar the moment text/template grows a builtin by that name.
func TestAllowlistIsTheFuncMapPlusTheDocumentedBuiltins(t *testing.T) {
	builtins := map[string]bool{
		"and": true, "or": true, "not": true,
		"eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
	}
	funcs := New(nil).funcs

	for name := range funcs {
		if !allowedFuncs[name] {
			t.Errorf("%q is in the FuncMap but not in the AST allowlist: a rule naming it is refused by checkTree", name)
		}
	}
	for name := range allowedFuncs {
		if _, ours := funcs[name]; ours {
			continue
		}
		if !builtins[name] {
			t.Errorf("%q is in the AST allowlist but is neither a FuncMap entry nor one of the admitted builtins", name)
		}
	}
	for name := range builtins {
		if !allowedFuncs[name] {
			t.Errorf("builtin %q is documented as admitted but the allowlist refuses it", name)
		}
	}
}

// TestFuncNamesIsTheFuncMapsOwnKeys pins the other half of the derivation: the
// published order is a list, and a list can go stale against the map it claims
// to name. Order is a documentation choice and is not asserted here; membership
// is not.
func TestFuncNamesIsTheFuncMapsOwnKeys(t *testing.T) {
	funcs := New(nil).funcs
	published := FuncNames()
	if len(published) != len(funcs) {
		t.Fatalf("FuncNames() has %d names, the FuncMap has %d entries: %v vs %v", len(published), len(funcs), published, keysOf(funcs))
	}
	seen := map[string]bool{}
	for _, name := range published {
		if seen[name] {
			t.Errorf("FuncNames() publishes %q twice", name)
		}
		seen[name] = true
		if _, ok := funcs[name]; !ok {
			t.Errorf("FuncNames() publishes %q, which the FuncMap does not define", name)
		}
	}
}

func keysOf(m template.FuncMap) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
