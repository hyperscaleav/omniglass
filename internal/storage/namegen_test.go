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

// TestPickOrdinalStemlessAllocates is the whole point of #681: a type with no
// stem names its rows by the ordinal alone ("1", "2"), which the old
// prefix-scan allocator could not do at all. With an empty stem its prefix was
// a bare "-", no sibling ever matched it, and it returned 1 forever, so the
// second row in the bucket collided with the first. Allocation now tests the
// name it would MINT for each candidate ordinal against the bucket, so an
// empty stem is not a special case, it is just a shorter mint.
func TestPickOrdinalStemlessAllocates(t *testing.T) {
	if got := pickOrdinal(nil, ""); got != 1 {
		t.Errorf("pickOrdinal(nil, \"\") = %d, want 1", got)
	}
	if got := pickOrdinal([]string{"1"}, ""); got != 2 {
		t.Errorf("pickOrdinal({1}, \"\") = %d, want 2 (a stem-less sibling must occupy its ordinal)", got)
	}
	if got := pickOrdinal([]string{"1", "3"}, ""); got != 2 {
		t.Errorf("pickOrdinal({1,3}, \"\") = %d, want 2 (lowest free, stem-less)", got)
	}
	// A stemmed sibling occupies nothing in the stem-less space: "display-1" is
	// not the name a stem-less mint would produce for any ordinal.
	if got := pickOrdinal([]string{"display-1"}, ""); got != 1 {
		t.Errorf("pickOrdinal({display-1}, \"\") = %d, want 1 (a stemmed sibling must not block a stem-less ordinal)", got)
	}
}

// TestPickOrdinalOperatorTypedGeneratedShapeBlocks proves an operator-typed
// name that happens to occupy the shape the generator mints is skipped rather
// than minted a second time. This is the case an ordinal-column-only allocator
// cannot see: that row has no ordinal of its own (the platform never named it,
// so name_generated is false and ordinal is NULL), yet it holds the NAME, and
// the scoped-name unique index is on the name. Reading only stored ordinals
// would pick 1 here and turn an ordinary operator workflow into a 23505.
func TestPickOrdinalOperatorTypedGeneratedShapeBlocks(t *testing.T) {
	if got := pickOrdinal([]string{"display-1"}, "display"); got != 2 {
		t.Errorf("pickOrdinal({display-1}, display) = %d, want 2 (a hand-typed name in the mint's shape still holds that name)", got)
	}
}

// TestMintName pins the one place the generated name's shape lives. A stem
// yields "<stem>-<n>"; no stem yields the ordinal alone, never a leading "-"
// (which validateEntityName refuses outright).
func TestMintName(t *testing.T) {
	cases := []struct {
		stem string
		n    int
		want string
	}{
		{"display", 1, "display-1"},
		{"display", 12, "display-12"},
		{"", 1, "1"},
		{"", 12, "12"},
	}
	for _, c := range cases {
		if got := mintName(c.stem, c.n); got != c.want {
			t.Errorf("mintName(%q, %d) = %q, want %q", c.stem, c.n, got, c.want)
		}
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
