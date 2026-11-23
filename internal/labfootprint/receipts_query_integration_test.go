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
	put := func(outpost, boot string, seq int, minted time.Time, ledgered bool) {
		t.Helper()
		ledger := "{1,0,null,null,null,null,null,null}"
		if !ledgered {
			ledger = ""
		}
		if _, err := conn.Exec(ctx, `insert into telemetry_receipt (batch_id, outpost, boot, seq, minted, ledger)
			values ($1, $2, $3, $4, $5, nullif($6, '')::bigint[])`,
			fmt.Sprintf("og.v1.telemetry.%s %s-%d", outpost, boot, seq), outpost, boot, seq, minted, ledger); err != nil {
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
	put("a", "a2", 1, to.Add(2*time.Minute), true)   // the outpost started again
	put("a", "a2", 2, to.Add(3*time.Minute), true)   // not that boot's first
	put("b", "b1", 1, to.Add(time.Minute), true)     // an outpost with nothing in the window
	put("c", "c1", 1, from.Add(30*time.Second), true)
	return ctx, conn, from, to
}

// The delivery read returns, for each boot with a batch in the window, its
// receipts from its first in the window on and its lower neighbor however
// long before, and the first receipt of each later boot of the same outposts;
// nothing of an outpost with nothing in the window, of an earlier boot, or
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
		got = append(got, fmt.Sprintf("%s/%s/%d", r.Outpost, r.Boot, r.Seq))
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
	if strings.Contains(plan, `"Subplan Name": "SubPlan`) {
		t.Fatalf("the plan runs a subquery per row:\n%s", plan)
	}
}

// wholeTableReceiptsQuery is the query as it read before #1947: window
// functions over every ledgered receipt. It is the oracle the bounded query
// must return the same rows as.
func wholeTableReceiptsQuery(from, to time.Time) string {
	return fmt.Sprintf(`select row_to_json(r) from (
		with ledgered as (
			select outpost, boot, seq, minted, resolved, deliveries, ledger, outcome, outpost_loss,
				min(seq) filter (where minted > to_timestamp(%[1]s) and minted <= to_timestamp(%[2]s))
					over (partition by boot) as first_in,
				min(seq) over (partition by boot) as boot_first
			from telemetry_receipt where ledger is not null),
		placed as (
			select *, max(seq) filter (where seq < first_in) over (partition by boot) as below,
				bool_or(first_in is not null) over (partition by outpost) as outpost_in
			from ledgered)
		select outpost, boot, seq, minted, resolved, deliveries, ledger, outcome, outpost_loss from placed
		where (first_in is not null and (seq >= first_in or seq = below))
			or (first_in is null and outpost_in and seq = boot_first and minted > to_timestamp(%[1]s))
		order by outpost, boot, seq) r`,
		epoch(from), epoch(to))
}

func readReceiptLines(t *testing.T, ctx context.Context, conn *pgx.Conn, q string) []string {
	t.Helper()
	rows, err := conn.Query(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
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
	return lines
}

// The bounded read returns exactly what the whole-table read did, on the
// fixture's boots before, across and after the window, a restarted boot, a
// push-lane boot and a boot whose only receipt is after the window (#1947).
func TestIntegrationReceiptsQueryMatchesTheWholeTableRead(t *testing.T) {
	t.Parallel()
	ctx, conn, from, to := receiptsDB(t)
	for _, q := range []string{
		`insert into telemetry_receipt (batch_id, boot, seq, minted, ledger) values
			('p1', 'srv1', 1, $1, '{1,0,null,null,null,null,null,null}'),
			('p2', 'srv1', 2, $2, '{1,0,null,null,null,null,null,null}'),
			('p3', 'srv1', 3, $3, '{1,0,null,null,null,null,null,null}'),
			('p4', 'srv2', 1, $3, '{1,0,null,null,null,null,null,null}')`,
	} {
		if _, err := conn.Exec(ctx, q, from.Add(-time.Hour), from.Add(time.Minute), to.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	want := readReceiptLines(t, ctx, conn, wholeTableReceiptsQuery(from, to))
	got := readReceiptLines(t, ctx, conn, ReceiptsQuery(from, to))
	if len(want) == 0 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("bounded read:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The bounded read reaches the receipts by the minted and (boot, seq)
// indexes, never by a sequential scan of the table, however many receipts it
// holds (#1947). The table is grown and analyzed so the planner prefers the
// indexes the way it does on a soak.
func TestIntegrationReceiptsQueryUsesTheIndexes(t *testing.T) {
	t.Parallel()
	ctx, conn, from, to := receiptsDB(t)
	if _, err := conn.Exec(ctx, `insert into telemetry_receipt (batch_id, outpost, boot, seq, minted, ledger)
		select 'bulk-' || g, 'z', 'z' || (g / 100), g % 100 + 1, $1::timestamptz - (g || ' seconds')::interval,
			'{1,0,null,null,null,null,null,null}'::bigint[]
		from generate_series(1, 60000) g`, from.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A toy table lets the planner call a sequential scan cheap; forbidding it
	// asks whether the indexes can serve every arm of the read, which is what
	// a table of millions of receipts then makes the cheaper plan.
	for _, q := range []string{`analyze telemetry_receipt`, `set enable_seqscan = off`} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var plan string
	if err := conn.QueryRow(ctx, "explain (format json) "+ReceiptsQuery(from, to)).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, `"Seq Scan"`) {
		t.Fatalf("the plan scans the whole table:\n%s", plan)
	}
	for _, idx := range []string{"telemetry_receipt_ledgered_minted_idx", "telemetry_receipt_ledgered_boot_seq_idx"} {
		if !strings.Contains(plan, idx) {
			t.Fatalf("the plan does not use %s:\n%s", idx, plan)
		}
	}
}
