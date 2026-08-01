package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRegistryShape pins the registry contract: every entry carries a key, a
// scope from the declared vocabulary, and operator doc; keys are unique; and
// the two audit-found gaps (the secret-key pair the container guide omitted)
// are declared.
func TestRegistryShape(t *testing.T) {
	scopes := map[string]bool{"server": true, "node": true, "cli": true, "test": true}
	seen := map[string]bool{}
	for _, v := range Registry {
		if v.Key == "" || v.Doc == "" {
			t.Errorf("registry entry %+v: key and doc are required", v)
		}
		if !scopes[v.Scope] {
			t.Errorf("registry entry %s: unknown scope %q", v.Key, v.Scope)
		}
		if seen[v.Key] {
			t.Errorf("registry entry %s: duplicate key", v.Key)
		}
		seen[v.Key] = true
		if !strings.HasPrefix(v.Key, "OMNIGLASS_") && v.Key != "DATABASE_URL" {
			t.Errorf("registry entry %s: unexpected key shape", v.Key)
		}
	}
	for _, want := range []string{"OMNIGLASS_SECRET_KEY", "OMNIGLASS_SECRET_KEY_FILE", "OMNIGLASS_DSN", "DATABASE_URL", "OMNIGLASS_SERVER", "OMNIGLASS_NODE_TOKEN"} {
		if !seen[want] {
			t.Errorf("registry missing %s", want)
		}
	}
}

// TestGetRefusesUnregistered is the gate that makes an undocumented variable
// impossible: reading a key the registry does not declare panics at the call
// site, in every environment, not just under test.
func TestGetRefusesUnregistered(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Get on an unregistered key did not panic")
		}
	}()
	Get("OMNIGLASS_NOT_A_REAL_VAR")
}

// TestGetReadsRegistered proves Get is a plain env read for declared keys.
func TestGetReadsRegistered(t *testing.T) {
	t.Setenv("OMNIGLASS_ADDR", ":9999")
	if got := Get("OMNIGLASS_ADDR"); got != ":9999" {
		t.Errorf("Get(OMNIGLASS_ADDR) = %q, want :9999", got)
	}
}

// TestConfigFactsMatchesCommitted is the drift gate for the generated env-var
// reference: the committed docs/src/generated/config.json must equal a fresh
// render of the registry.
func TestConfigFactsMatchesCommitted(t *testing.T) {
	fresh, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/config.json")
	if err != nil {
		t.Fatalf("read committed config.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/config.json drifted from the registry: run make gen and commit the result")
	}
	var doc struct {
		Vars []struct {
			Key   string `json:"key"`
			Scope string `json:"scope"`
		} `json:"vars"`
	}
	if err := json.Unmarshal(fresh, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}
	if len(doc.Vars) != len(Registry) {
		t.Errorf("facts carries %d vars, registry %d", len(doc.Vars), len(Registry))
	}
}
