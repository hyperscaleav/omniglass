package storage

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

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

// TestSuppressesAgreesWithTheNameItWouldMint is the label side of the same
// guarantee (#693): the label map asks the mint whether a name shows its
// number, so an answer that disagreed with the name would put "Boardroom 1"
// beside a system called `boardroom`. Every case is asserted against the minted
// NAME rather than against a restatement of the rule, so the two are held
// together by the assertion and not only by both being read from the same
// fields.
func TestSuppressesAgreesWithTheNameItWouldMint(t *testing.T) {
	for _, c := range []struct {
		what string
		m    nameMint
		n    int
		want bool
	}{
		{"a component's first still carries its number", componentMint("display"), 1, false},
		{"a component's second", componentMint("display"), 2, false},
		{"the first of a system's stem", systemMint("boardroom"), 1, true},
		{"the second of a system's stem", systemMint("boardroom"), 2, false},
		{"the twelfth", systemMint("boardroom"), 12, false},
		{"a stem-less mint, whose ordinal IS its name", nameMint{bareFirst: true}, 1, false},
	} {
		got := c.m.suppresses(c.n)
		if got != c.want {
			t.Errorf("%s: suppresses(%d) = %v, want %v", c.what, c.n, got, c.want)
		}
		// The name is the arbiter: a suppressed ordinal is one the minted name
		// does not end in.
		suffix := "-" + strconv.Itoa(c.n)
		if bare := !strings.HasSuffix(c.m.name(c.n), suffix); c.m.stem != "" && bare != got {
			t.Errorf("%s: suppresses(%d) = %v but the name it mints is %q", c.what, c.n, got, c.m.name(c.n))
		}
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

// A location_type's name rule is a DECLARATION, and the cases below are what
// that buys over a template (#687, ADR-0102): its whole output space is
// enumerable, so "this rule can only mint legal names" is decidable when the
// rule is written rather than sampled.

// TestNameRuleIsItsMint proves the rule and the mint are the same value rather
// than two shapes kept in step by hand. That is what carries ADR-0101's
// guarantee onto this tier for free: pickOrdinal tests the mint, and the mint
// IS the stored rule, so a rule cannot mean one name to the allocator and
// another to the caller.
func TestNameRuleIsItsMint(t *testing.T) {
	cases := []struct {
		name    string
		rule    NameRule
		want    []string // the names at ordinals 1, 2, 3
		whyItIs string
	}{
		{
			name:    "positional",
			rule:    NameRule{},
			want:    []string{"1", "2", "3"},
			whyItIs: "an empty stem is the floor whose ordinal genuinely is its name",
		},
		{
			name:    "stemmed, counted from one",
			rule:    NameRule{Stem: "wing"},
			want:    []string{"wing-1", "wing-2", "wing-3"},
			whyItIs: "a stem with no suppression counts from 1, like a component",
		},
		{
			name:    "stemmed, first suppressed",
			rule:    NameRule{Stem: "wing", BareFirst: true},
			want:    []string{"wing", "wing-2", "wing-3"},
			whyItIs: "the per-type form of D3: the only wing in a building is just wing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.rule.normalized().mint()
			for i, want := range c.want {
				if got := m.name(i + 1); got != want {
					t.Errorf("rule %+v mints %q at ordinal %d, want %q (%s)", c.rule, got, i+1, want, c.whyItIs)
				}
			}
			// And the allocator agrees with it over its own output, which is
			// the property that stops the second create in a bucket from being
			// a 23505.
			if got := pickOrdinal(c.want[:2], m); got != 3 {
				t.Errorf("pickOrdinal over the rule's own first two names = %d, want 3: the allocator and the mint disagree", got)
			}
		})
	}
}

// TestNameRuleNormalizesTheIgnoredFlag proves a positional rule has exactly one
// spelling. bareFirst is meaningless without a stem (suppressing the ordinal of
// a name that IS its ordinal leaves nothing), so storing it true would give two
// rules that mint identically and compare unequal.
func TestNameRuleNormalizesTheIgnoredFlag(t *testing.T) {
	got := NameRule{Stem: "", BareFirst: true}.normalized()
	if got != (NameRule{}) {
		t.Errorf("NameRule{stem:\"\", bareFirst:true}.normalized() = %+v, want the zero rule", got)
	}
	// The normalization is cosmetic only where the mint is concerned, which is
	// the point: it changes the stored spelling, never the name.
	if name := (NameRule{Stem: "", BareFirst: true}).mint().name(1); name != "1" {
		t.Errorf("an unnormalized positional rule mints %q at ordinal 1, want 1", name)
	}
}

// TestValidateNameRuleRefusesWhatItCannotMint is the acceptance behavior "a
// rule that would render an illegal name is refused when the rule is edited,
// not when a location is created". It refuses by MINTING, so the check and the
// generator cannot disagree about what the rule produces.
func TestValidateNameRuleRefusesWhatItCannotMint(t *testing.T) {
	ok := []NameRule{
		{},                              // positional
		{Stem: "wing"},                  // counted
		{Stem: "wing", BareFirst: true}, // suppressed
		{Stem: "b1"},                    // digits are legal in a stem
		{Stem: stemOfLen(90)},           // the longest stem that still leaves room for the ordinal
	}
	for _, r := range ok {
		if err := validateNameRule(&r); err != nil {
			t.Errorf("validateNameRule(%+v) = %v, want nil", r, err)
		}
	}
	bad := []struct {
		rule NameRule
		why  string
	}{
		{NameRule{Stem: "Wing"}, "an upper-case stem mints an illegal name"},
		{NameRule{Stem: "wing_1"}, "an underscore is not in the name character set"},
		{NameRule{Stem: "-wing"}, "a name may not start with a hyphen"},
		{NameRule{Stem: "wing floor"}, "a space is not in the name character set"},
		{
			NameRule{Stem: stemOfLen(95)},
			"a 95-character stem mints legally at ordinal 1 and illegally further up, which is exactly the case a check that only tried ordinal 1 would wave through",
		},
	}
	for _, c := range bad {
		err := validateNameRule(&c.rule)
		if err == nil {
			t.Errorf("validateNameRule(stem %d chars, %q) = nil, want a refusal (%s)", len(c.rule.Stem), c.rule.Stem, c.why)
			continue
		}
		if !errors.Is(err, ErrInvalidNameRule) {
			t.Errorf("validateNameRule(%q) = %v, want ErrInvalidNameRule (%s)", c.rule.Stem, err, c.why)
		}
	}
	// The 95-character stem is legal AS A NAME, which is what makes it the
	// interesting case: the rule is refused for what it would MINT, not for
	// what it is.
	if err := validateEntityName(stemOfLen(95)); err != nil {
		t.Fatalf("precondition: a 95-character stem is itself an illegal name (%v), so the case above proves nothing", err)
	}
	// nil is the opt-out, not a broken rule.
	if err := validateNameRule(nil); err != nil {
		t.Errorf("validateNameRule(nil) = %v, want nil: no rule is the opt-out", err)
	}
}

// stemOfLen builds a legal stem of exactly n characters, for the length cases
// above: the rule's ceiling is about how long the MINTED name gets, so the stem
// itself has to be legal for those cases to be testing what they claim.
func stemOfLen(n int) string { return "w" + strings.Repeat("a", n-1) }

// TestNameRuleExamplesAreWhatItMints is the property the console's naming
// summary rests on (#710): the strings a surface SHOWS for a rule are produced
// by the same nameMint a create allocates from, so the two cannot describe
// different names. #695 closed the naming half of exactly this duplication by
// having the server return the drafted name; this is the same answer for a rule
// that is about to be saved rather than a row about to be created.
//
// It asserts the values a reader can check by eye AND that they are the mint's
// own output at 1 and 2, which is what makes a future change to the mint
// (another suppression rule, a separator) reach the console without anyone
// editing TypeScript.
func TestNameRuleExamplesAreWhatItMints(t *testing.T) {
	for _, c := range []struct {
		name string
		rule NameRule
		want []string
	}{
		{"positional", NameRule{}, []string{"1", "2"}},
		{"stemmed, counted from one", NameRule{Stem: "wing"}, []string{"wing-1", "wing-2"}},
		{"stemmed, first suppressed", NameRule{Stem: "wing", BareFirst: true}, []string{"wing", "wing-2"}},
		{"positional, ignoring a suppression it cannot honour", NameRule{BareFirst: true}, []string{"1", "2"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := c.rule.Examples()
			if len(got) != len(c.want) {
				t.Fatalf("Examples() = %v, want %v", got, c.want)
			}
			m := c.rule.normalized().mint()
			for i, want := range c.want {
				if got[i] != want {
					t.Errorf("Examples()[%d] = %q, want %q", i, got[i], want)
				}
				if got[i] != m.name(i+1) {
					t.Errorf("Examples()[%d] = %q, but the mint produces %q at ordinal %d: a surface would show a name no create makes", i, got[i], m.name(i+1), i+1)
				}
			}
		})
	}
}
