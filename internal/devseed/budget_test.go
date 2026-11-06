package devseed_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// This file sorts first on purpose. The tests in this package run in parallel,
// and Go releases parallel tests in source order, files by name. The large
// scenario is the one test that is real work rather than a clone of a seeded
// template (about two minutes of seeding, #1537), so it is released first and
// runs alongside everything else; released last it would start when the rest are
// done and add its whole length to the package's wall time.

// largeSeedBudget is the stated bound on seeding the large scenario on a fresh
// database, the number a throwaway stack's start-up pays.
const largeSeedBudget = 5 * time.Minute

// The large scenario seeds within its stated budget on a fresh database.
func TestRunScenarioLargeWithinBudget(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("seeds the full large fleet")
	}
	ctx, gw, conn := openSeededGateway(t)
	s, err := devseed.LookupScenario("large")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := devseed.RunScenario(ctx, gw, "", s, devseed.Options{}); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("large scenario seeded in %s", took.Round(time.Second))
	if took > largeSeedBudget {
		t.Errorf("large scenario took %s, budget %s", took, largeSeedBudget)
	}
	if got := countFleet(t, ctx, conn).components; got < largeFleetFloor {
		t.Errorf("large scenario landed %d components, want at least %d", got, largeFleetFloor)
	}
}
