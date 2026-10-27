package db

import (
	"strings"
	"testing"
	"time"
)

// The fixture trees the version rules are held to. Each is a list of file
// names, the only fact either rule reads.
var (
	baseTree = []string{
		"20260101090000_init.sql",
		"20260102090000_widget.sql",
	}
	// cleanTree adds one migration stamped after the base's newest.
	cleanTree = append(append([]string{}, baseTree...), "20260103090000_gadget.sql")
)

// clock is the fixtures' now: a day after the clean tree's newest migration.
var clock = time.Date(2026, 1, 4, 9, 0, 0, 0, time.UTC)

func TestVersion(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"20261026090000_webhook_ingress.sql", "20261026090000", true},
		{"20261026090000_a_b_c.sql", "20261026090000", true},
		{"2026102609_short.sql", "", false},
		{"202610260900001_long.sql", "", false},
		{"20261026090000.sql", "", false},
		{"2026102609000a_not_digits.sql", "", false},
		{"init.sql", "", false},
	} {
		got, err := Version(tc.name)
		if tc.ok && (err != nil || got != tc.want) {
			t.Errorf("Version(%q) = %q, %v; want %q", tc.name, got, err, tc.want)
		}
		if !tc.ok && err == nil {
			t.Errorf("Version(%q) = %q; want an error (not a dbmate new stamp)", tc.name, got)
		}
	}
}

func TestCheckUnique(t *testing.T) {
	if errs := CheckUnique(cleanTree); len(errs) != 0 {
		t.Errorf("a clean tree: %v", errs)
	}

	dup := append(append([]string{}, cleanTree...), "20260102090000_sprocket.sql")
	errs := CheckUnique(dup)
	if len(errs) != 1 {
		t.Fatalf("a duplicate version: %d errors %v, want 1", len(errs), errs)
	}
	for _, want := range []string{"20260102090000", "20260102090000_widget.sql", "20260102090000_sprocket.sql"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("the duplicate error %q does not name %s", errs[0], want)
		}
	}

	if errs := CheckUnique([]string{"20260101090000_init.sql", "0001_hand_numbered.sql"}); len(errs) != 1 ||
		!strings.Contains(errs[0].Error(), "0001_hand_numbered.sql") {
		t.Errorf("a name with no dbmate new stamp: %v, want one error naming it", errs)
	}
}

func TestCheckAgainstBase(t *testing.T) {
	for _, tc := range []struct {
		what  string
		head  []string
		now   time.Time
		wants [][]string // one error per entry, each containing every string
	}{
		{"a valid tree: the added migration is newer than the base's newest", cleanTree, clock, nil},
		{"no migration added", baseTree, clock, nil},
		{
			"an added migration older than the base's newest (a branch cut before it landed)",
			append(append([]string{}, baseTree...), "20260101120000_gadget.sql"),
			clock, [][]string{{"20260101120000_gadget.sql", "20260102090000_widget.sql"}},
		},
		{
			"an added migration that takes the base's newest version",
			append(append([]string{}, baseTree...), "20260102090000_gadget.sql"),
			clock, [][]string{{"20260102090000_gadget.sql", "20260102090000_widget.sql"}},
		},
		{
			"each added migration is judged on its own",
			append(append([]string{}, cleanTree...), "20260101120000_older.sql"),
			clock, [][]string{{"20260101120000_older.sql"}},
		},
		{
			"an added migration stamped later than the clock",
			cleanTree,
			time.Date(2026, 1, 3, 8, 0, 0, 0, time.UTC), [][]string{{"20260103090000_gadget.sql", "later than the clock"}},
		},
		{
			"the base's newest is stamped ahead of the clock, and the remedy says when a fresh stamp clears it",
			append(append([]string{}, baseTree...), "20260102080000_gadget.sql"),
			time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC),
			[][]string{{"20260102080000_gadget.sql", "once the clock passes 20260102090000 UTC"}},
		},
		{
			"a stamp within the clock-skew allowance",
			cleanTree,
			time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC).Add(-ClockSkew), nil,
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			errs := CheckAgainstBase(baseTree, tc.head, tc.now)
			if len(errs) != len(tc.wants) {
				t.Fatalf("%d errors %v, want %d", len(errs), errs, len(tc.wants))
			}
			for i, wants := range tc.wants {
				for _, want := range wants {
					if !strings.Contains(errs[i].Error(), want) {
						t.Errorf("error %q does not name %s", errs[i], want)
					}
				}
			}
		})
	}

	if errs := CheckAgainstBase(nil, cleanTree, clock); len(errs) != 0 {
		t.Errorf("an empty base: %v", errs)
	}
}
