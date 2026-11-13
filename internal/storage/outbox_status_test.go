package storage_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
)

// TestOutboxStatusQueuedDepthAndDueAge proves the outbox read the server's
// platform metrics take each window (#1628), against a real Postgres: an
// empty outbox reads zero and zero, a queued row not yet due counts toward
// the depth but not the age, and the oldest due row's wait is the age, by
// the same predicate DeliverNext takes rows by.
func TestOutboxStatusQueuedDepthAndDueAge(t *testing.T) {
	t.Parallel()
	f := newNotifyFixture(t)
	ctx := f.ctx

	st, err := f.gw.OutboxStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st != (storage.OutboxStatus{}) {
		t.Fatalf("an empty outbox reads %+v, want zero", st)
	}

	if _, err := f.gw.RaiseAlarm(ctx, "", "display-1", storage.AlarmSpec{Severity: "high", Message: "no video"}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// One row waits out a backoff an hour off; the other has been due for a
	// minute and a half.
	if _, err := conn.Exec(ctx, `update notification set next_attempt_at = now() + interval '1 hour'
		where id = (select id from notification order by id limit 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `update notification set next_attempt_at = now() - interval '90 seconds'
		where next_attempt_at <= now()`); err != nil {
		t.Fatal(err)
	}

	st, err = f.gw.OutboxStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Queued != 2 {
		t.Fatalf("queued = %d, want 2 (the due row and the one in backoff)", st.Queued)
	}
	if st.DueAge < 90*time.Second || st.DueAge > 2*time.Minute {
		t.Fatalf("due age = %v, want the due row's 90 s wait", st.DueAge)
	}
}
