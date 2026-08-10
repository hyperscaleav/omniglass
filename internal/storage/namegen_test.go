package storage

import "testing"

// display is the component mint every allocation case below shares: a stem, no
// suppression, the shape #681 already mints thousands of names in.
var display = componentMint("display")

// TestPickOrdinalEmptyScope proves the trivial case: no siblings at all, so
// the first ordinal is 1. Also the "ordinal always present" half of the
// rule: a lone component is stem-1, never a bare stem.
func TestPickOrdinalEmptyScope(t *testing.T) {
	if got := pickOrdinal(nil, display); got != 1 {
		t.Errorf("pickOrdinal(nil, display) = %d, want 1", got)
	}
	if got := pickOrdinal([]string{}, display); got != 1 {
		t.Errorf("pickOrdinal([], display) = %d, want 1", got)
	}
}

// TestPickOrdinalSkipsGap proves the smallest-unused rule over a gap: 1 and 3
// taken, 2 is free and wins over 4.
func TestPickOrdinalSkipsGap(t *testing.T) {
	if got := pickOrdinal([]string{"display-1", "display-3"}, display); got != 2 {
		t.Errorf("pickOrdinal({display-1,display-3}, display) = %d, want 2", got)
	}
}

// TestPickOrdinalIgnoresForeignStems proves a sibling with a different stem
// never blocks an ordinal: "mic-1" occupies nothing in "display"'s space.
func TestPickOrdinalIgnoresForeignStems(t *testing.T) {
	if got := pickOrdinal([]string{"mic-1", "mic-2"}, display); got != 1 {
		t.Errorf("pickOrdinal({mic-1,mic-2}, display) = %d, want 1 (foreign stem must not block)", got)
	}
}

// TestPickOrdinalIgnoresPrefixCollision proves a stem that is a PREFIX of
// another stem's names does not falsely claim its ordinals: "display-panel-1"
// starts with "display-" but is not of the form "display-<digits>", so it
// must not be read as display's ordinal 1.
func TestPickOrdinalIgnoresPrefixCollision(t *testing.T) {
	if got := pickOrdinal([]string{"display-panel-1"}, display); got != 1 {
		t.Errorf("pickOrdinal({display-panel-1}, display) = %d, want 1 (a foreign stem sharing a prefix must not collide)", got)
	}
}

// TestPickOrdinalOperatorNameNoOrdinalDoesNotBlock proves an operator's bare
// "display" (no ordinal suffix at all) never occupies an ordinal: it is not
// of the form "display-<digits>", so "display-1" is still free.
func TestPickOrdinalOperatorNameNoOrdinalDoesNotBlock(t *testing.T) {
	if got := pickOrdinal([]string{"display"}, display); got != 1 {
		t.Errorf("pickOrdinal({display}, display) = %d, want 1 (a name with no ordinal must not block one)", got)
	}
}

// TestPickOrdinalNonDigitSuffixIgnored proves a name that merely LOOKS close
// ("display-a") does not parse as an ordinal and does not block one.
func TestPickOrdinalNonDigitSuffixIgnored(t *testing.T) {
	if got := pickOrdinal([]string{"display-a"}, display); got != 1 {
		t.Errorf("pickOrdinal({display-a}, display) = %d, want 1", got)
	}
}

// TestPickOrdinalFillsFromOne proves a single existing ordinal 2 (no 1) still
// yields 1, not 3: the rule is smallest UNUSED, never "one past the max".
func TestPickOrdinalFillsFromOne(t *testing.T) {
	if got := pickOrdinal([]string{"display-2"}, display); got != 1 {
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
	stemless := componentMint("")
	if got := pickOrdinal(nil, stemless); got != 1 {
		t.Errorf("pickOrdinal(nil, \"\") = %d, want 1", got)
	}
	if got := pickOrdinal([]string{"1"}, stemless); got != 2 {
		t.Errorf("pickOrdinal({1}, \"\") = %d, want 2 (a stem-less sibling must occupy its ordinal)", got)
	}
	if got := pickOrdinal([]string{"1", "3"}, stemless); got != 2 {
		t.Errorf("pickOrdinal({1,3}, \"\") = %d, want 2 (lowest free, stem-less)", got)
	}
	// A stemmed sibling occupies nothing in the stem-less space: "display-1" is
	// not the name a stem-less mint would produce for any ordinal.
	if got := pickOrdinal([]string{"display-1"}, stemless); got != 1 {
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
	if got := pickOrdinal([]string{"display-1"}, display); got != 2 {
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
		if got := componentMint(c.stem).name(c.n); got != c.want {
			t.Errorf("componentMint(%q).name(%d) = %q, want %q", c.stem, c.n, got, c.want)
		}
	}
}

// TestMintNameSuppressesTheFirstOrdinal is D3 (ADR-0101) at the level the rule
// actually lives: a mint that suppresses gives the first of its stem the bare
// stem and every later one the ordinal. "br" then "br-2", and "br-1" is a name
// this mint never produces at any ordinal.
func TestMintNameSuppressesTheFirstOrdinal(t *testing.T) {
	br := systemMint("br")
	cases := []struct {
		n    int
		want string
	}{
		{1, "br"},
		{2, "br-2"},
		{12, "br-12"},
	}
	for _, c := range cases {
		if got := br.name(c.n); got != c.want {
			t.Errorf("systemMint(\"br\").name(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	for n := 1; n <= 20; n++ {
		if got := br.name(n); got == "br-1" {
			t.Errorf("systemMint(\"br\").name(%d) = %q: a suppressed mint must never produce the ordinal-1 form", n, got)
		}
	}
}

// TestMintNameStemlessIgnoresSuppression proves suppression cannot empty a
// name: a stem-less mint's ordinal IS its name (a floor called "1"), so
// suppressing it would leave nothing at all, and validateEntityName refuses
// "". The stem-less branch wins over bareFirst for exactly that reason.
func TestMintNameStemlessIgnoresSuppression(t *testing.T) {
	m := nameMint{stem: "", bareFirst: true}
	if got := m.name(1); got != "1" {
		t.Errorf("a suppressed stem-less mint at ordinal 1 = %q, want \"1\"", got)
	}
	if got := m.name(2); got != "2" {
		t.Errorf("a suppressed stem-less mint at ordinal 2 = %q, want \"2\"", got)
	}
}

// TestPickOrdinalSuppressedMintAgreesWithItself is the constraint that makes a
// suppressed first name possible without a second parser: allocation tests the
// name the mint WOULD produce, so an empty bucket allocates ordinal 1 (the bare
// "br"), a bucket already holding "br" allocates 2 ("br-2"), and a bucket
// holding only "br-2" allocates 1 again.
//
// The last case is the order dependence D3 accepts, stated as an assertion
// rather than as prose: deleting the only "br" and creating another yields "br"
// while "br-2" still exists.
func TestPickOrdinalSuppressedMintAgreesWithItself(t *testing.T) {
	br := systemMint("br")
	cases := []struct {
		name     string
		existing []string
		want     int
		wantName string
	}{
		{"empty bucket", nil, 1, "br"},
		{"the bare name taken", []string{"br"}, 2, "br-2"},
		{"only the second taken", []string{"br-2"}, 1, "br"},
		{"both taken", []string{"br", "br-2"}, 3, "br-3"},
		{"the ordinal-1 form is not this mint's", []string{"br-1"}, 1, "br"},
	}
	for _, c := range cases {
		got := pickOrdinal(c.existing, br)
		if got != c.want {
			t.Errorf("%s: pickOrdinal(%v, br) = %d, want %d", c.name, c.existing, got, c.want)
		}
		if name := br.name(got); name != c.wantName {
			t.Errorf("%s: the allocated ordinal %d mints %q, want %q", c.name, got, name, c.wantName)
		}
	}
}

// TestPickOrdinalSuppressedYieldsToAHandTypedName proves the acceptance case
// that would otherwise be a 23505: an operator typed "br" themselves, so the
// generator's first candidate is taken and the next generated system is "br-2".
// The hand-typed row holds the NAME and no ordinal, which is exactly the row an
// ordinal-reading allocator cannot see (ADR-0097).
func TestPickOrdinalSuppressedYieldsToAHandTypedName(t *testing.T) {
	br := systemMint("br")
	got := pickOrdinal([]string{"br"}, br)
	if name := br.name(got); name != "br-2" {
		t.Errorf("beside a hand-typed \"br\", the generator mints %q, want \"br-2\"", name)
	}
}

// TestNameScopeBuckets proves the placement buckets produce distinct lock keys
// per kind, that a parent wins over a location where both exist (mirroring
// ComponentNameTaken's own branch order), and that the same parent id under two
// different entity kinds is two different buckets: two generators that cannot
// collide must never serialize against each other.
func TestNameScopeBuckets(t *testing.T) {
	p, l := "parent-1", "loc-1"
	keys := map[string]string{
		"component parent":   componentNameScope(&p, nil).lockKey(),
		"component location": componentNameScope(nil, &l).lockKey(),
		"component orphan":   componentNameScope(nil, nil).lockKey(),
		"system parent":      systemNameScope(&p, nil).lockKey(),
		"system location":    systemNameScope(nil, &l).lockKey(),
		"system orphan":      systemNameScope(nil, nil).lockKey(),
		"location parent":    locationNameScope(&p).lockKey(),
		"location root":      locationNameScope(nil).lockKey(),
	}
	seen := map[string]string{}
	for label, k := range keys {
		if prior, dup := seen[k]; dup {
			t.Errorf("lock key %q is shared by %q and %q: two buckets that cannot collide would serialize", k, prior, label)
		}
		seen[k] = label
	}
	if got, want := componentNameScope(&p, &l).lockKey(), keys["component parent"]; got != want {
		t.Errorf("a parent must win over a location, as ComponentNameTaken resolves placement: got %q, want %q", got, want)
	}
}

// TestTwoStemsCanMintOneName is why the lock key above carries no stem, and it
// is a fact about suppression rather than a preference: stem "wall" at ordinal
// 2 and stem "wall-2" at ordinal 1 are the SAME NAME, and both stems pass the
// rule CreateSystemType validates a stem with.
//
// Before suppression the two spaces were provably disjoint ("wall-2" needs
// digits after the dash, "wall-2-1" is not a name "wall" can mint), which is
// what made a stem-keyed lock sound. Keyed on the stem now, those two creates
// in one bucket take different locks, read the same siblings, mint the same
// name, and the loser gets a 23505 on a create that supplied no name at all.
func TestTwoStemsCanMintOneName(t *testing.T) {
	if a, b := systemMint("wall").name(2), systemMint("wall-2").name(1); a != b {
		t.Fatalf("systemMint(\"wall\").name(2) = %q and systemMint(\"wall-2\").name(1) = %q: this test is asserting an overlap that no longer exists, so re-check whether lockKey may carry the stem again", a, b)
	}
	if err := validateEntityName("wall-2"); err != nil {
		t.Fatalf("validateEntityName(\"wall-2\") = %v: a stem of this shape must be reachable for the overlap to matter", err)
	}
	// So the key's composition is pinned by value: the table and the bucket,
	// nothing narrower. Any allocation input finding its way back in here has to
	// come past this assertion and past the overlap above.
	l := "loc-1"
	if got, want := systemNameScope(nil, &l).lockKey(), "system_name/location/loc-1"; got != want {
		t.Errorf("lock key = %q, want %q: the key is the table and the bucket, because the bucket is the only partition of the name space a mint cannot cross", got, want)
	}
	// Dropping the stem widens the lock to the bucket and no further: the table
	// stays in the key, so a component and a system sharing a parent id still
	// do not serialize against each other.
	p := "parent-1"
	if componentNameScope(&p, nil).lockKey() == systemNameScope(&p, nil).lockKey() {
		t.Error("a component and a system sharing a parent id share a lock key: they cannot collide and must not serialize")
	}
}

// TestLocationNameScopeHasTwoBuckets pins the asymmetry the scope value exists
// to make unmistakable: a location is bucketed by its parent, else it is at the
// root (location_parent_name_key and location_root_name_key), because a
// location has no located-at column. The three-way shape component and system
// share is not a value a location's constructor can produce, so the filter it
// builds never names a column the table does not have.
func TestLocationNameScopeHasTwoBuckets(t *testing.T) {
	p := "parent-1"
	if got, want := locationNameScope(&p).where, "parent_id = $1"; got != want {
		t.Errorf("a parented location filters on %q, want %q", got, want)
	}
	if got, want := locationNameScope(nil).where, "parent_id is null"; got != want {
		t.Errorf("a root location filters on %q, want %q (a location has no location_id to bucket by)", got, want)
	}
	if got := locationNameScope(nil).arg; got != nil {
		t.Errorf("the root bucket binds %v, want nothing", got)
	}
}
