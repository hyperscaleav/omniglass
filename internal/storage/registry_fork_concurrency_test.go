package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The registry fork writes the whole mutable row back (ADR-0095 decision 2),
// so it has none of the lost-update immunity the column-wise coalesce write it
// replaced had. Two operators editing different fields of one shipped row at
// once each compute an image from the image they read, and whichever writes
// second overwrites the other's field. Nothing reports it: both writes
// succeeded, and both are in the audit log.
//
// These two tests drive exactly that, and they FAIL against the unlocked code
// rather than asserting that a lock exists. The interleaving needs no help: the
// two calls take the same path from the same starting gun, so both reads land
// before either commit, and both registries lost an edit inside two rounds on
// every run measured (#709 in the report; see the build-log entry).
//
// They are also the reason the lock is where it is. location_type shipped with
// `for update of lt` ON the resolving read in #703, and the location test below
// failed against it: at READ COMMITTED that statement's snapshot predates the
// wait, so the waiter serialised behind the winner and then read the shadow as
// it was BEFORE the winner wrote it. The test could not tell a lock from a
// working one, which is the only kind of concurrency test worth having.

// forkRounds is how many paired edits each test drives. One round is usually
// enough to show the defect; the repetition is what turns "it can fail" into a
// property that holds across interleavings rather than one lucky scheduling.
const forkRounds = 8

func newForkGateway(t *testing.T) *storage.PG {
	t.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw
}

func TestConcurrentComponentTypeForksKeepBothEdits(t *testing.T) {
	gw := newForkGateway(t)
	ctx := context.Background()

	for round := range forkRounds {
		name := fmt.Sprintf("Display %d", round)
		icon := fmt.Sprintf("icon-%d", round)
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, errs[0] = gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{DisplayName: &name})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errs[1] = gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{Icon: &icon})
		}()
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent fork %d: %v", round, i, err)
			}
		}
		ct, err := gw.GetComponentType(ctx, "display")
		if err != nil {
			t.Fatalf("round %d: read back: %v", round, err)
		}
		gotIcon := ""
		if ct.Icon != nil {
			gotIcon = *ct.Icon
		}
		if ct.DisplayName != name || gotIcon != icon {
			t.Fatalf("round %d: after two concurrent forks the row is display_name=%q icon=%q, want %q and %q: one edit was overwritten by the other's stale image",
				round, ct.DisplayName, gotIcon, name, icon)
		}
	}
}

func TestConcurrentLocationTypeForksKeepBothEdits(t *testing.T) {
	gw := newForkGateway(t)
	ctx := context.Background()

	for round := range forkRounds {
		name := fmt.Sprintf("Campus %d", round)
		icon := fmt.Sprintf("icon-%d", round)
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, errs[0] = gw.UpdateLocationType(ctx, "", "campus", storage.LocationTypePatch{DisplayName: &name})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errs[1] = gw.UpdateLocationType(ctx, "", "campus", storage.LocationTypePatch{Icon: &icon})
		}()
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent fork %d: %v", round, i, err)
			}
		}
		lt, err := gw.GetLocationType(ctx, "campus")
		if err != nil {
			t.Fatalf("round %d: read back: %v", round, err)
		}
		if lt.DisplayName != name || lt.Icon != icon {
			t.Fatalf("round %d: after two concurrent forks the row is display_name=%q icon=%q, want %q and %q: one edit was overwritten by the other's stale image",
				round, lt.DisplayName, lt.Icon, name, icon)
		}
	}
}

// TestConcurrentForkAndRestoreSeeEachOther is the other half of the same lock:
// restore READS the row to decide whether there is a fork and to record what it
// discarded, then writes (deletes) the shadow, which is the same read-then-write
// shape the patch path has.
//
// Both orderings are asserted, and which one happened is read off the end state
// rather than forced, so the test never depends on winning a race. The base fork
// each round sets an icon the edit does not touch, which is what makes the two
// orderings tell different stories: a restore that ran FIRST leaves the edit
// forked over the SHIPPED icon, and a stale read would leave it over the base
// fork's icon instead. A restore that ran SECOND must audit the image it
// actually discarded, the edit, not the base it read before the edit landed.
//
// component_type carries this one because the lock is the same lockRegistryRow
// call on both registries and the location half of the pair is covered by
// TestConcurrentLocationTypeForksKeepBothEdits.
func TestConcurrentForkAndRestoreSeeEachOther(t *testing.T) {
	gw := newForkGateway(t)
	ctx := context.Background()

	shipped, err := gw.GetComponentType(ctx, "display")
	if err != nil {
		t.Fatalf("read the shipped row: %v", err)
	}
	if shipped.Icon == nil {
		t.Fatalf("the shipped display type has no icon, so this test cannot tell the two orderings apart")
	}
	shippedIcon := *shipped.Icon

	restoreImage := func(round int) map[string]any {
		t.Helper()
		entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "component_type", Verb: "restore"})
		if err != nil {
			t.Fatalf("round %d: list audit: %v", round, err)
		}
		if len(entries) == 0 {
			t.Fatalf("round %d: the restore wrote no audit row", round)
		}
		var m map[string]any
		if err := json.Unmarshal(entries[0].Old, &m); err != nil {
			t.Fatalf("round %d: decode the restored image: %v (%s)", round, err, entries[0].Old)
		}
		return m
	}

	for round := range forkRounds {
		base := fmt.Sprintf("Base %d", round)
		baseIcon := fmt.Sprintf("base-icon-%d", round)
		edit := fmt.Sprintf("Edit %d", round)

		if _, err := gw.UpdateComponentType(ctx, "", "display",
			storage.ComponentTypePatch{DisplayName: &base, Icon: &baseIcon}); err != nil {
			t.Fatalf("round %d: stage the base fork: %v", round, err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var forkErr, restoreErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, forkErr = gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{DisplayName: &edit})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, restoreErr = gw.RestoreComponentType(ctx, "", "display")
		}()
		close(start)
		wg.Wait()
		if forkErr != nil {
			t.Fatalf("round %d: concurrent fork: %v", round, forkErr)
		}
		if restoreErr != nil {
			t.Fatalf("round %d: concurrent restore: %v (the row was forked when the round began)", round, restoreErr)
		}

		ct, err := gw.GetComponentType(ctx, "display")
		if err != nil {
			t.Fatalf("round %d: read back: %v", round, err)
		}
		got := restoreImage(round)
		if ct.Forked {
			// The restore ran first: it discarded the base fork, and the edit
			// then forked the SHIPPED row.
			if got["display_name"] != base {
				t.Fatalf("round %d: the restore ran first and audited display_name=%v, want %q", round, got["display_name"], base)
			}
			icon := ""
			if ct.Icon != nil {
				icon = *ct.Icon
			}
			if ct.DisplayName != edit || icon != shippedIcon {
				t.Fatalf("round %d: the restore ran first, so the surviving fork is display_name=%q icon=%q, want %q over the shipped icon %q: the edit was computed from an image the restore had already discarded",
					round, ct.DisplayName, icon, edit, shippedIcon)
			}
			// Leave the next round a clean slate.
			if _, err := gw.RestoreComponentType(ctx, "", "display"); err != nil {
				t.Fatalf("round %d: clear the surviving fork: %v", round, err)
			}
			continue
		}
		// The restore ran second: it discarded the EDIT, and must say so.
		if got["display_name"] != edit {
			t.Fatalf("round %d: the restore ran second and audited display_name=%v, want %q: it recorded an image that was already replaced",
				round, got["display_name"], edit)
		}
	}
}
