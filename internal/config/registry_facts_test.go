package config_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/config"
	_ "github.com/hyperscaleav/omniglass/internal/config/settingsfacts"
)

// TestConfigFactsMatchesCommitted is the drift gate for the generated env-var
// reference: the committed docs/src/generated/config.json must equal a fresh
// render of the registry (and its family expansions, #2223). This test is in
// the external package so it can install the settings-facts seam without
// cycling through the internal/config import graph.
func TestConfigFactsMatchesCommitted(t *testing.T) {
	fresh, err := config.FactsJSON()
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
			Key    string `json:"key"`
			Family string `json:"family"`
		} `json:"vars"`
	}
	if err := json.Unmarshal(fresh, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}
	// The vars render as Registry plus the family expansion; the two sources
	// are disjoint (family rows carry a Family field, registry rows do not).
	hand, fam := 0, 0
	for _, v := range doc.Vars {
		if v.Family == "" {
			hand++
		} else {
			fam++
		}
	}
	if hand != len(config.Registry) {
		t.Errorf("facts carries %d hand-declared vars, registry %d", hand, len(config.Registry))
	}
	if fam == 0 {
		t.Errorf("facts carries no family-expanded vars; the settings-facts seam is not installed")
	}
	// Every family row names a declared family prefix.
	declared := map[string]bool{}
	for _, f := range config.Families {
		declared[f.Prefix] = true
	}
	for _, v := range doc.Vars {
		if v.Family == "" {
			continue
		}
		if !declared[v.Family] {
			t.Errorf("fact %s carries family %q, not a declared prefix", v.Key, v.Family)
		}
		if !strings.HasPrefix(v.Key, v.Family) {
			t.Errorf("fact %s does not start with its family %s", v.Key, v.Family)
		}
	}
}

// TestSettingsFamilyAccepted proves config.Get accepts a name under the
// OMNIGLASS_SETTING__ prefix without panicking: the family declaration
// covers it even though no Registry row names it individually.
func TestSettingsFamilyAccepted(t *testing.T) {
	// A read of an unset family-covered name is an empty string, not a panic.
	if got := config.Get("OMNIGLASS_SETTING__ASSISTANT__MODEL"); got != "" {
		t.Errorf("Get on unset family var = %q, want \"\"", got)
	}
}

// TestEnvironReadsOnlyItsFamily is the whole-family read the server's env
// layer and the lab take (#2223): only names under the prefix, and an
// undeclared prefix panics as an undeclared Get does.
func TestEnvironReadsOnlyItsFamily(t *testing.T) {
	t.Setenv("OMNIGLASS_SETTING__ASSISTANT__MODEL", "m")
	t.Setenv("OMNIGLASS_SETTINGX", "not-in-family")
	got := config.Environ("OMNIGLASS_SETTING__")
	if len(got) != 1 || got[0] != "OMNIGLASS_SETTING__ASSISTANT__MODEL=m" {
		t.Fatalf("Environ = %q, want only the one settings variable", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("Environ of an undeclared prefix did not panic")
		}
	}()
	config.Environ("OMNIGLASS_NOPE__")
}
