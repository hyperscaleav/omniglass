package storage_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The label recompute's row scan carries `for update`, and the column lists it
// reads carry correlated subqueries for the names around each row (a
// component's location_name and product_handle, a system's standard_handle,
// a location's parent_name). #709 proved, on the registry fork, that a lock
// placed inside a resolving read does not do what it looks like it does: at
// READ COMMITTED the statement's snapshot is taken before it blocks, and when
// the lock is released only the locked row is re-read, not the subqueries
// already evaluated against the stale snapshot. #731 asked whether the same
// mechanism reaches a label.
//
// It does not, and this test is the witness. A label is rendered from two
// kinds of input only: the locked row's own columns (name, ordinal, and the
// classifier and placement ids), which the re-read after the wait refreshes,
// and facts resolved by SEPARATE statements issued after the scan returns
// (componentPlacements, componentLabelChain, globalLabelRule and their system
// and location counterparts), each of which takes its own snapshot after the
// wait is over. No label key reads a column the scan's subqueries fill;
// TestNoLabelKeyReadsAScanSubqueryColumn holds that half by construction.
//
// The interleaving is FORCED rather than raced for. #709's test could rely on
// two identical calls from one starting gun both reading before either wrote,
// but the window here is a recompute blocked mid-scan while a third writer
// commits, and three racing goroutines hit that window too rarely to prove
// anything. So a raw transaction holds the component's row lock (standing in
// for a concurrent single-row stamp), and the test waits until each actor is
// observably blocked in pg_stat_activity before it moves the next one:
//
//  1. the holder updates the component and holds its row lock;
//  2. the bulk recompute takes the operation lock and blocks IN ITS SCAN;
//  3. a control statement, shaped exactly like the scan but reading the
//     product's label through a correlated subquery, blocks behind it;
//  4. a location label edit (the cascade) blocks on the operation lock the bulk
//     recompute holds;
//  5. the product is renamed and commits, inside the window;
//  6. the holder commits, and all three proceed.
//
// The control must come back with the OLD product label: that is the #709
// mechanism, live in this very schema, and it is what proves the window was
// real (a test whose interleaving silently did not happen would pass for the
// wrong reason). The recompute must come back with the NEW one. Both are
// asserted, so the test fails if the window closes or if the render ever starts
// reading through it; a mutation that rendered ProductName from a scan
// subquery failed it at round 0.
func TestALabelRecomputeWaitingOnARowLockRendersTheNamesCommittedDuringTheWait(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	room, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-a", LocationType: "room", ParentName: strptr("hq")}, all)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	product := storagetest.MintProduct(t, ctx, gw, "display")
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		ProductName: strptr(product.Name), LocationName: strptr("room-a"),
	}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.ProductName}} at {{.LocationLabel}}"); err != nil {
		t.Fatalf("set rule: %v", err)
	}
	// Settle the rule onto the row so each round starts from a label that agrees
	// with it, and the only drift is what the round itself commits.
	if _, err := gw.RecomputeLabels(ctx, "", "component", all, all); err != nil {
		t.Fatalf("settle the rule: %v", err)
	}

	connect := func() *pgx.Conn {
		t.Helper()
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	}
	holder, control, watcher := connect(), connect(), connect()

	// waitBlocked returns once n backends in this database are waiting on a
	// lock: a row lock, a tuple lock or the advisory lock, which is every wait
	// this test arranges.
	waitBlocked := func(round, n int, who string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			var got int
			if err := watcher.QueryRow(ctx, `select count(*) from pg_stat_activity
				where datname = current_database() and wait_event_type = 'Lock'`).Scan(&got); err != nil {
				t.Fatalf("round %d: watch locks: %v", round, err)
			}
			if got >= n {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("round %d: %s never blocked (%d of %d waiters): the interleaving this test exists to force did not happen", round, who, got, n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// What a rule reads the room as before the first round: its label if the
	// location rule rendered one, else its name.
	prevProduct, prevRoom := product.Label, room.Label
	if prevRoom == "" {
		prevRoom = room.Name
	}
	for round := range 3 {
		newProduct := fmt.Sprintf("Edge %d", round)
		newRoom := fmt.Sprintf("Room %d", round)

		// 1. A concurrent writer holds the component's row lock.
		tx, err := holder.Begin(ctx)
		if err != nil {
			t.Fatalf("round %d: begin holder: %v", round, err)
		}
		if _, err := tx.Exec(ctx, `update component set updated_at = now() where id = $1`, comp.ID); err != nil {
			t.Fatalf("round %d: hold the row: %v", round, err)
		}

		var wg sync.WaitGroup
		var (
			bulk                []storage.LabelChange
			bulkErr, cascadeErr error
			controlErr          error
			controlRead         string
		)
		// 2. The bulk recompute takes the operation lock and blocks in its scan.
		wg.Add(1)
		go func() {
			defer wg.Done()
			bulk, bulkErr = gw.RecomputeLabels(ctx, "", "component", all, all)
		}()
		waitBlocked(round, 1, "the bulk recompute")

		// 3. The control: the scan's own shape, with the name a label would read
		// fetched by a correlated subquery instead of a later statement.
		wg.Add(1)
		go func() {
			defer wg.Done()
			controlErr = control.QueryRow(ctx, `
				select (select coalesce(pr.label, '') from product pr where pr.id = component.product_id)
				from component where id = $1 order by id for update`, comp.ID).Scan(&controlRead)
		}()
		waitBlocked(round, 2, "the control statement")

		// 4. The cascade: a location edit whose restamp queues on the operation
		// lock the bulk recompute holds.
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cascadeErr = gw.UpdateLocation(ctx, "", "room-a", storage.LocationPatch{Label: &newRoom}, all, all)
		}()
		waitBlocked(round, 3, "the location cascade")

		// 5. The rename commits inside the window. A product edit takes neither
		// the operation lock nor the component's row, so nothing holds it back.
		if _, err := gw.UpdateProduct(ctx, "", product.ID, storage.ProductPatch{Label: &newProduct}); err != nil {
			t.Fatalf("round %d: rename the product: %v", round, err)
		}

		// 6. Release everyone.
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("round %d: commit holder: %v", round, err)
		}
		wg.Wait()
		for what, err := range map[string]error{"bulk recompute": bulkErr, "location cascade": cascadeErr, "control": controlErr} {
			if err != nil {
				t.Fatalf("round %d: %s: %v", round, what, err)
			}
		}

		if controlRead != prevProduct {
			t.Fatalf("round %d: the control read product label %q, want the pre-rename %q: the scan's snapshot did not predate the rename, so this round proved nothing",
				round, controlRead, prevProduct)
		}
		// The bulk recompute ran before the cascade committed, so it reads the
		// room as it was; it must read the product as it IS.
		want := newProduct + " at " + prevRoom
		var got *storage.LabelChange
		for i := range bulk {
			if bulk[i].ID == comp.ID {
				got = &bulk[i]
			}
		}
		if got == nil || got.To != want {
			t.Fatalf("round %d: the bulk recompute rendered %+v, want To=%q: it composed the label from the names its scan's snapshot saw before the wait, not the ones committed during it",
				round, got, want)
		}
		after, err := gw.GetComponent(ctx, comp.Name, all)
		if err != nil {
			t.Fatalf("round %d: read back: %v", round, err)
		}
		if final := newProduct + " at " + newRoom; after.Label != final {
			t.Fatalf("round %d: the stored label is %q, want %q", round, after.Label, final)
		}
		prevProduct, prevRoom = newProduct, newRoom
	}
}
