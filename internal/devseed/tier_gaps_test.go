package devseed_test

import (
	"regexp"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// TierGaps is the lab-level check's ledger of what the dev fleet does not
// carry yet (#1371). The live check fails a listed owner that now carries the
// tier (a stale entry); these tests hold the ledger itself to its shape, with
// no database: a gap is keyed by a product the fleet places (or the all-systems
// key), tied to an issue, and in a tier the check knows.

var issueRef = regexp.MustCompile(`^#[0-9]+$`)

func TestTierGapsNameAKnownTierAndAnIssue(t *testing.T) {
	t.Parallel()
	tiers := map[devseed.Tier]bool{devseed.TierBuiltIn: true, devseed.TierCollected: true, devseed.TierSystem: true}
	for tier, owners := range devseed.TierGaps {
		if !tiers[tier] {
			t.Errorf("gap list names an unknown tier %q", tier)
		}
		for owner, gap := range owners {
			if !issueRef.MatchString(gap.Issue) {
				t.Errorf("%s gap on %q: issue %q is not a #N reference", tier, owner, gap.Issue)
			}
		}
	}
}

func TestComponentTierGapsNameAProductTheFleetPlaces(t *testing.T) {
	t.Parallel()
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	placed := map[string]bool{}
	for _, c := range append(append([]devseed.Component(nil), doc.Components...), doc.SimComponents...) {
		placed[c.Product] = true
	}
	for _, tier := range []devseed.Tier{devseed.TierBuiltIn, devseed.TierCollected} {
		for product := range devseed.TierGaps[tier] {
			if !placed[product] {
				t.Errorf("%s gap names product %q, which the dev fleet does not place", tier, product)
			}
		}
	}
}

func TestSystemGapsAreKeyedByTheAllSystemsEntry(t *testing.T) {
	t.Parallel()
	for owner := range devseed.TierGaps[devseed.TierSystem] {
		if owner != devseed.AllSystems {
			t.Errorf("system gap keyed %q: systems carry no stable key yet, so the gap covers them all under %q", owner, devseed.AllSystems)
		}
	}
}

func TestTierGapsHoldOnlyTheOpenIssues(t *testing.T) {
	t.Parallel()
	// #1372, #1493 and #1494 closed their gaps; a leftover entry for them is
	// the stale one the check would fail on, caught here before a lab run.
	closed := map[string]bool{"#1372": true, "#1493": true, "#1494": true}
	for tier, owners := range devseed.TierGaps {
		for owner, gap := range owners {
			if closed[gap.Issue] {
				t.Errorf("%s gap on %q cites %s, which is closed", tier, owner, gap.Issue)
			}
		}
	}
}
