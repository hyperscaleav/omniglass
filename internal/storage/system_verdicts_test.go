package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// The bulk health read (#653), and the two things it has to be true about:
// it must AGREE with the per-system report it replaces on a list, and it must
// cost the same for a large estate as for a small one. Either alone is
// worthless. A cheap read that disagrees with the panel a row opens into is a
// console contradicting itself; an agreeing read that costs one query per system
// is the defect it was built to remove, moved from the browser to the server.

// verdictEstate builds `systems` systems in one room, staffs each with the
// component named for it, and returns the gateway plus its counter. Every system
// is built to one standard carrying one role, so a raised alarm on a member
// degrades exactly its own system and leaves the rest healthy: the fixture has to
// contain more than one verdict or an oracle that always answered "healthy" would
// pass it.
func verdictEstate(t *testing.T, gw storage.Gateway, systems int) {
	t.Helper()
	ctx := context.Background()
	sc := scope.Set{All: true}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, sc); err != nil {
		t.Fatalf("create campus: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-r1", LocationType: "room", ParentName: strptr("hq")}, sc); err != nil {
		t.Fatalf("create room: %v", err)
	}
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "verdict-std", DisplayName: "Verdict"}); err != nil {
		t.Fatalf("upsert standard: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "verdict-std", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	std, bar := "verdict-std", "cisco-room-bar"
	for i := range systems {
		sys, comp := fmt.Sprintf("sys-%d", i), fmt.Sprintf("bar-%d", i)
		if _, err := gw.CreateSystem(ctx, "",
			storage.SystemSpec{Name: sys, StandardID: &std, LocationName: strptr("hq-r1")}, sc, sc); err != nil {
			t.Fatalf("create system %d: %v", i, err)
		}
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp, ProductName: &bar}, sc, sc, sc, sc); err != nil {
			t.Fatalf("create component %d: %v", i, err)
		}
		if err := gw.AssignRole(ctx, "", sys, "table-mic", comp, sc); err != nil {
			t.Fatalf("assign role %d: %v", i, err)
		}
		// Every third system's member is in trouble, so the answer is a mix.
		if i%3 == 1 {
			sev := "critical"
			if i%2 == 0 {
				sev = "warning"
			}
			if _, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{
				Severity: sev, Message: "fixture", DedupKey: "fixture",
			}); err != nil {
				t.Fatalf("raise alarm %d: %v", i, err)
			}
		}
	}
	// One system with NO standard, no role and no member: nothing has ever
	// recomputed it, so it has no recorded row at all, and the read has to answer
	// for it from the default rather than omit it. This is the row the whole
	// "coalesce to healthy" branch exists for.
	if _, err := gw.CreateSystem(ctx, "",
		storage.SystemSpec{Name: "sys-untouched", LocationName: strptr("hq-r1")}, sc, sc); err != nil {
		t.Fatalf("create untouched system: %v", err)
	}
}

// TestSystemVerdictsAgreeWithTheReport is the oracle gate. SystemHealth is the
// read the badge used to make per row, and it computes its verdict live from the
// roles; SystemVerdicts reads the recorded series and defaults what it does not
// find. Those are two different mechanisms answering one question, so the
// agreement is asserted rather than assumed, over an estate deliberately holding
// all three verdicts AND a system nothing has ever recomputed.
func TestSystemVerdictsAgreeWithTheReport(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	verdictEstate(t, gw, 7)
	all := scope.Set{All: true}

	bulk, err := gw.SystemVerdicts(ctx, all)
	if err != nil {
		t.Fatalf("SystemVerdicts: %v", err)
	}
	systems, err := gw.ListSystems(ctx, all)
	if err != nil {
		t.Fatalf("ListSystems: %v", err)
	}
	if len(bulk) != len(systems) {
		t.Fatalf("the bulk read answered for %d systems and the list shows %d: a list row with no verdict falls back to its own request, which is the defect this read exists to remove",
			len(bulk), len(systems))
	}
	byID := map[string]string{}
	for _, v := range bulk {
		byID[v.SystemID] = v.Verdict
	}
	seen := map[string]int{}
	for i := range systems {
		s := systems[i]
		rep, err := gw.SystemHealth(ctx, s.Name, 24*time.Hour, all)
		if err != nil {
			t.Fatalf("SystemHealth %s: %v", s.Name, err)
		}
		got, ok := byID[s.ID]
		if !ok {
			t.Errorf("the bulk read has no verdict for %s, which the list shows", s.Name)
			continue
		}
		if got != rep.Verdict {
			t.Errorf("system %s: the bulk read says %q and its own health report says %q", s.Name, got, rep.Verdict)
		}
		seen[got]++
	}
	// A fixture that produced one verdict for everything would let an oracle that
	// simply returned "healthy" pass, which is the shape of accident this repo has
	// shipped before.
	if len(seen) < 2 {
		t.Fatalf("every system in the fixture read %v: the comparison never distinguished a right answer from a constant one", seen)
	}
}

// TestSystemVerdictsCostIsFlatInEstateSize: one statement, whatever the estate.
// The point of the read is that it does not pay per system, on the server any
// more than in the browser.
func TestSystemVerdictsCostIsFlatInEstateSize(t *testing.T) {
	small, smallCounter := storagetest.NewCountingDB(t)
	large, largeCounter := storagetest.NewCountingDB(t)
	ctx := context.Background()
	for _, gw := range []storage.Gateway{small, large} {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	verdictEstate(t, small, 1)
	verdictEstate(t, large, widePage)

	read := func(gw storage.Gateway, c *querycount.Counter) reading {
		t.Helper()
		return measure(t, c, func() (int, error) {
			vs, err := gw.SystemVerdicts(ctx, all)
			return len(vs), err
		})
	}
	assertFlatCost(t, read(small, smallCounter), read(large, largeCounter), 1)
}

// TestSystemVerdictsScopeMatchesTheList: the bulk read answers for exactly the
// systems the list shows, under a narrowed scope as well as an all one. A read
// that answered for more would leak a verdict for a row the caller cannot see; a
// read that answered for fewer would send those rows back to fetching their own.
func TestSystemVerdictsScopeMatchesTheList(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	verdictEstate(t, gw, 4)
	all := scope.Set{All: true}

	systems, err := gw.ListSystems(ctx, all)
	if err != nil {
		t.Fatalf("ListSystems: %v", err)
	}
	if len(systems) < 3 {
		t.Fatalf("fixture built %d systems, too few to narrow a scope against", len(systems))
	}
	narrow := scope.Set{IDs: []string{systems[0].ID}}

	scoped, err := gw.SystemVerdicts(ctx, narrow)
	if err != nil {
		t.Fatalf("SystemVerdicts (narrow): %v", err)
	}
	listed, err := gw.ListSystems(ctx, narrow)
	if err != nil {
		t.Fatalf("ListSystems (narrow): %v", err)
	}
	if len(listed) >= len(systems) {
		t.Fatalf("the narrowed list returned %d of %d systems: the scope never narrowed, so nothing was compared", len(listed), len(systems))
	}
	want := map[string]bool{}
	for i := range listed {
		want[listed[i].ID] = true
	}
	for _, v := range scoped {
		if !want[v.SystemID] {
			t.Errorf("the bulk read answered for system %s, which this caller's list does not show", v.SystemID)
		}
		delete(want, v.SystemID)
	}
	for id := range want {
		t.Errorf("the list shows system %s and the bulk read has no verdict for it", id)
	}

	// An empty scope is an empty answer, not a refusal and not the whole estate.
	empty, err := gw.SystemVerdicts(ctx, scope.Set{})
	if err != nil {
		t.Fatalf("SystemVerdicts (empty scope): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("an empty read scope returned %d verdicts, want none", len(empty))
	}
}
