package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/tail"
)

// changesSince reads the change notices the tail carries after cursor, as
// "<kind>:<id>" topics, and the cursor after them.
func changesSince(t *testing.T, gw *storage.PG, cursor string) ([]string, string) {
	t.Helper()
	commits, next, err := gw.ViewTail().Read(cursor, 0)
	if err != nil {
		t.Fatalf("read the tail: %v", err)
	}
	var out []string
	for _, c := range commits {
		for _, r := range c.Rows {
			if r.Lane == tail.LaneChange {
				out = append(out, r.OwnerKind+":"+r.OwnerID)
			}
		}
	}
	return out, next
}

// A write that recomputes health names the system and component it moved on
// the tail's change lane, once its transaction commits (#2731): an alarm on a
// member is a change to the member and to the system it staffs, and a slot
// assignment, which moves the system's slots and members, is a change to the
// system. A write that fails publishes nothing.
func TestHealthRecomputePublishesChanges(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	var sysID, barID string
	if err := f.conn.QueryRow(ctx, `select (select id::text from system where name = 'hq-huddle'), (select id::text from component where name = 'bar-1')`).Scan(&sysID, &barID); err != nil {
		t.Fatal(err)
	}
	has := func(got []string, want string) bool {
		for _, g := range got {
			if g == want {
				return true
			}
		}
		return false
	}

	cursor := f.gw.ViewTail().Cursor()
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "high", Message: "mic dead"}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	got, cursor := changesSince(t, f.gw, cursor)
	if !has(got, "system:"+sysID) || !has(got, "component:"+barID) {
		t.Fatalf("an alarm's changes = %v, want system:%s and component:%s", got, sysID, barID)
	}

	if err := f.gw.UnassignSlot(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	got, cursor = changesSince(t, f.gw, cursor)
	if !has(got, "system:"+sysID) {
		t.Fatalf("an unassign's changes = %v, want system:%s", got, sysID)
	}

	// Assigning a component that does not exist fails inside its transaction:
	// nothing it may have noted publishes.
	if err := f.gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "no-such-bar", f.all, f.all, f.all, f.all); err == nil {
		t.Fatal("assigning a missing component succeeded")
	}
	if got, _ = changesSince(t, f.gw, cursor); len(got) != 0 {
		t.Fatalf("a failed write published %v", got)
	}
}
