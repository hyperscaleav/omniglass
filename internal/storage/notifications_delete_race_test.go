package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A transition never waits on the notification lane (#1335). Deleting an
// action rule while the tick holds one of its notification rows mid-send
// leaves the DELETE waiting on that row for its SET NULL; a transition the
// rule matches would then queue a row whose foreign key check waits behind
// the DELETE, up to a send timeout. The queue skips a rule whose row is
// being changed instead of waiting on it.
func TestATransitionDoesNotWaitOnARuleBeingDeletedBehindASend(t *testing.T) {
	t.Parallel()
	f := newNotifyFixture(t)
	ctx := f.ctx

	// A second rule that matches too and is never touched.
	if _, err := f.gw.CreateActionRule(ctx, "", storage.ActionRuleSpec{Name: "page-all", On: []string{"raised"},
		When: "severity_rank >= 40", Targets: []string{"ops-chat"}}, f.all); err != nil {
		t.Fatal(err)
	}
	// A first raise gives the tick a row of page-high to be mid-send on.
	if _, err := f.gw.RaiseAlarm(ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"}); err != nil {
		t.Fatal(err)
	}

	// The tick, mid-send: it holds one of the rule's rows.
	tick, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer tick.Close(ctx)
	tickTx, err := tick.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tickTx.Rollback(ctx) }()
	var held string
	if err := tickTx.QueryRow(ctx, `select n.id::text from notification n join action_rule r on r.id = n.rule_id
		where r.name = 'page-high' order by n.id limit 1 for update of n`).Scan(&held); err != nil {
		t.Fatal(err)
	}

	// An operator deletes the rule: its SET NULL waits on the held row.
	del, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Close(ctx)
	delDone := make(chan error, 1)
	go func() {
		_, err := del.Exec(ctx, `delete from action_rule where name = 'page-high'`)
		delDone <- err
	}()
	waitForLockWait(t, ctx, f.dsn, "delete from action_rule")

	// A raise the deleted rule matches returns without waiting on the delete.
	raiseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	a, err := f.gw.RaiseAlarm(raiseCtx, "", "display-2", storage.AlarmSpec{Severity: "high", Message: "no audio"})
	if err != nil {
		t.Fatalf("the raise failed or waited out its deadline behind the delete: %v (after %s)", err, time.Since(start))
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the raise waited %s behind the delete", d)
	}
	// The other rule still notified; the rule mid-delete is skipped.
	ns := f.list(t, storage.NotificationFilter{AlarmID: a.ID})
	if len(ns) != 1 || ns[0].Rule != "page-all" {
		t.Fatalf("the raise queued %+v, want one row from page-all and none from the rule being deleted", ns)
	}

	// The send ends; the delete completes.
	if err := tickTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-delDone:
		if err != nil {
			t.Fatalf("the delete failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the delete never finished after the send ended")
	}
}

// waitForLockWait blocks until a backend running a statement that starts with
// prefix is waiting on a lock.
func waitForLockWait(t *testing.T, ctx context.Context, dsn, prefix string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from pg_stat_activity
			where wait_event_type = 'Lock' and query like $1 || '%'`, prefix).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no backend ever waited on a lock running %q", prefix)
}
