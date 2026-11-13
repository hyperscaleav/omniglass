package labfootprint

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// receiptsDB is a migrated database holding receipts placed around the
// window [from, to], and a connection to it.
func receiptsDB(t *testing.T) (context.Context, *pgx.Conn, time.Time, time.Time) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	from := time.Date(2026, 11, 12, 14, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	put := func(node, boot string, seq int, minted time.Time, ledgered bool) {
		t.Helper()
		ledger := "{1,0,null,null,null,null,null,null}"
		if !ledgered {
			ledger = ""
		}
		if _, err := conn.Exec(ctx, `insert into telemetry_receipt (batch_id, node, boot, seq, minted, ledger)
			values ($1, $2, $3, $4, $5, nullif($6, '')::bigint[])`,
			fmt.Sprintf("og.v1.telemetry.%s %s-%d", node, boot, seq), node, boot, seq, minted, ledger); err != nil {
			t.Fatalf("put %s %d: %v", boot, seq, err)
		}
	}
	put("a", "a0", 1, from.Add(-3*time.Hour), true)  // an earlier boot, ended before the window
	put("a", "a1", 1, from.Add(-2*time.Hour), true)  // below the lower neighbor
	put("a", "a1", 2, from.Add(-time.Hour), true)    // the lower neighbor
	put("a", "a1", 3, from.Add(time.Minute), true)   // the boot's first in the window
	put("a", "a1", 4, from.Add(2*time.Minute), true) // in the window
	put("a", "a1", 5, to.Add(time.Minute), true)     // after it: says the boot settled
	put("a", "a1", 6, to.Add(2*time.Minute), false)  // carried no ledger
	put("a", "a2", 1, to.Add(2*time.Minute), true)   // the node started again
	put("a", "a2", 2, to.Add(3*time.Minute), true)   // not that boot's first
	put("b", "b1", 1, to.Add(time.Minute), true)     // a node with nothing in the window
	put("c", "c1", 1, from.Add(30*time.Second), true)
	return ctx, conn, from, to
}

// The delivery read returns, for each boot with a batch in the window, its
// receipts from its first in the window on and its lower neighbor however
// long before, and the first receipt of each later boot of the same nodes;
// nothing of a node with nothing in the window, of an earlier boot, or
// without a ledger.
func TestIntegrationReceiptsQueryReadsTheWindowsBoots(t *testing.T) {
	t.Parallel()
	ctx, conn, from, to := receiptsDB(t)
	var lines []string
	rows, err := conn.Query(ctx, ReceiptsQuery(from, to))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rs, err := ParseReceipts(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rs {
		got = append(got, fmt.Sprintf("%s/%s/%d", r.Node, r.Boot, r.Seq))
	}
	want := []string{"a/a1/2", "a/a1/3", "a/a1/4", "a/a1/5", "a/a2/1", "c/c1/1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("read %v, want %v", got, want)
	}
}

// The delivery read scans the receipts once. A correlated subquery (a
// SubPlan run per row) rescans the table for every receipt it checks, so its
// cost grows with the square of the table: at 3,000 systems it ran for
// hours and starved the ingest it was measuring (#1841).
func TestIntegrationReceiptsQueryHasNoSubPlan(t *testing.T) {
	t.Parallel()
	ctx, conn, from, to := receiptsDB(t)
	var plan string
	if err := conn.QueryRow(ctx, "explain (format json) "+ReceiptsQuery(from, to)).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, `"Subplan Name"`) {
		t.Fatalf("the plan runs a subquery per row:\n%s", plan)
	}
}
