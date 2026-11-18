package fleet

import (
	"fmt"
	"strings"
	"testing"
)

func TestDisplayNamesUnique(t *testing.T) {
	gen := NewNameGenerator()
	seen := make(map[string]bool)
	// Generate more names than we'd typically need. Empty seed
	// triggers the internal sequence counter so each call still
	// returns a unique name (preserving the prior contract).
	for i := 0; i < 500; i++ {
		name := gen.Next("")
		if seen[name] {
			t.Errorf("duplicate display name at index %d: %s", i, name)
		}
		seen[name] = true
	}
}

func TestDisplayNamesSpreadAcrossAdjectives(t *testing.T) {
	// With seeded inputs the generator should NOT clump every name
	// into a single adjective bucket, that was the bug operators
	// hit at scale (every system started with "Boring *"). Hash
	// 1000 distinct seeds and confirm we touch a healthy number
	// of distinct adjectives.
	gen := NewNameGenerator()
	adj := map[string]int{}
	for i := 0; i < 1000; i++ {
		name := gen.Next(fmt.Sprintf("seed-%d", i))
		// adjective is everything before the first space
		// ("Affectionate Bose" -> "Affectionate")
		for j := 0; j < len(name); j++ {
			if name[j] == ' ' {
				adj[name[:j]]++
				break
			}
		}
	}
	if len(adj) < 50 {
		t.Errorf("expected at least 50 distinct adjectives across 1000 seeds, got %d", len(adj))
	}
}

func TestDisplayNamesAreHumanized(t *testing.T) {
	// Direct shape contract: each generated name must be
	// "Adjective Surname", capitalized first letter on each word,
	// single space separator, no underscore. Lab console, omniglass
	// UI, and any other consumer all see the same friendly form
	// because we humanize at the source.
	gen := NewNameGenerator()
	for i := 0; i < 50; i++ {
		name := gen.Next(fmt.Sprintf("seed-%d", i))
		if name == "" {
			t.Fatal("generator returned empty name")
		}
		if !strings.Contains(name, " ") {
			t.Errorf("expected space separator, got %q", name)
		}
		if strings.Contains(name, "_") {
			t.Errorf("did not expect underscore in humanized name, got %q", name)
		}
		if name[0] < 'A' || name[0] > 'Z' {
			t.Errorf("expected leading uppercase, got %q", name)
		}
	}
}
