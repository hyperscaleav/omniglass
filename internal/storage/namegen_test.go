package storage

import "testing"

// TestPickOrdinalEmptyScope proves the trivial case: no siblings at all, so
// the first ordinal is 1. Also the "ordinal always present" half of the
// rule: a lone component is stem-1, never a bare stem.
func TestPickOrdinalEmptyScope(t *testing.T) {
	if got := pickOrdinal(nil, "display"); got != 1 {
		t.Errorf("pickOrdinal(nil, display) = %d, want 1", got)
	}
	if got := pickOrdinal([]string{}, "display"); got != 1 {
		t.Errorf("pickOrdinal([], display) = %d, want 1", got)
	}
}

// TestPickOrdinalSkipsGap proves the smallest-unused rule over a gap: 1 and 3
// taken, 2 is free and wins over 4.
func TestPickOrdinalSkipsGap(t *testing.T) {
	if got := pickOrdinal([]string{"display-1", "display-3"}, "display"); got != 2 {
		t.Errorf("pickOrdinal({display-1,display-3}, display) = %d, want 2", got)
	}
}

// TestPickOrdinalIgnoresForeignStems proves a sibling with a different stem
// never blocks an ordinal: "mic-1" occupies nothing in "display"'s space.
func TestPickOrdinalIgnoresForeignStems(t *testing.T) {
	if got := pickOrdinal([]string{"mic-1", "mic-2"}, "display"); got != 1 {
		t.Errorf("pickOrdinal({mic-1,mic-2}, display) = %d, want 1 (foreign stem must not block)", got)
	}
}

// TestPickOrdinalIgnoresPrefixCollision proves a stem that is a PREFIX of
// another stem's names does not falsely claim its ordinals: "display-panel-1"
// starts with "display-" but is not of the form "display-<digits>", so it
// must not be read as display's ordinal 1.
func TestPickOrdinalIgnoresPrefixCollision(t *testing.T) {
	if got := pickOrdinal([]string{"display-panel-1"}, "display"); got != 1 {
		t.Errorf("pickOrdinal({display-panel-1}, display) = %d, want 1 (a foreign stem sharing a prefix must not collide)", got)
	}
}

// TestPickOrdinalOperatorNameNoOrdinalDoesNotBlock proves an operator's bare
// "display" (no ordinal suffix at all) never occupies an ordinal: it is not
// of the form "display-<digits>", so "display-1" is still free.
func TestPickOrdinalOperatorNameNoOrdinalDoesNotBlock(t *testing.T) {
	if got := pickOrdinal([]string{"display"}, "display"); got != 1 {
		t.Errorf("pickOrdinal({display}, display) = %d, want 1 (a name with no ordinal must not block one)", got)
	}
}

// TestPickOrdinalNonDigitSuffixIgnored proves a name that merely LOOKS close
// ("display-a") does not parse as an ordinal and does not block one.
func TestPickOrdinalNonDigitSuffixIgnored(t *testing.T) {
	if got := pickOrdinal([]string{"display-a"}, "display"); got != 1 {
		t.Errorf("pickOrdinal({display-a}, display) = %d, want 1", got)
	}
}

// TestPickOrdinalFillsFromOne proves a single existing ordinal 2 (no 1) still
// yields 1, not 3: the rule is smallest UNUSED, never "one past the max".
func TestPickOrdinalFillsFromOne(t *testing.T) {
	if got := pickOrdinal([]string{"display-2"}, "display"); got != 1 {
		t.Errorf("pickOrdinal({display-2}, display) = %d, want 1", got)
	}
}

// TestNameGenScopeKeyBuckets proves the three placement buckets produce
// distinct keys, and that a parent wins over a location when both happen to
// be supplied (mirroring ComponentNameTaken's own branch order): two
// generators in different buckets must never serialize against each other.
func TestNameGenScopeKeyBuckets(t *testing.T) {
	p := "parent-1"
	l := "loc-1"
	keys := map[string]string{
		"parent-only":   nameGenScopeKey(&p, nil),
		"location-only": nameGenScopeKey(nil, &l),
		"orphan":        nameGenScopeKey(nil, nil),
		"parent-wins":   nameGenScopeKey(&p, &l),
	}
	if keys["parent-only"] != keys["parent-wins"] {
		t.Errorf("a parent must win over a location exactly as ComponentNameTaken resolves placement: got %q vs %q", keys["parent-only"], keys["parent-wins"])
	}
	seen := map[string]bool{}
	for label, k := range keys {
		if label == "parent-wins" {
			continue // deliberately equal to parent-only, checked above
		}
		if seen[k] {
			t.Errorf("scope key %q reused across distinct buckets", k)
		}
		seen[k] = true
	}
}
