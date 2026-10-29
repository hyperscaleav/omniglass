package notifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fakeStore counts the rows a pass takes and records whether each row's
// context could be cancelled by the pass.
type fakeStore struct {
	sweepErr   error
	taken      int
	cancelable []bool
	// order records what a pass did, in order: "sweep" and "deliver".
	order []string
}

func (f *fakeStore) EndExpiredSnoozes(context.Context, int) (int, error) {
	f.order = append(f.order, "sweep")
	return 0, f.sweepErr
}

func (f *fakeStore) DeliverNext(ctx context.Context, _ storage.SendFunc) (bool, error) {
	f.taken++
	f.order = append(f.order, "deliver")
	_, hasDeadline := ctx.Deadline()
	f.cancelable = append(f.cancelable, hasDeadline)
	return true, nil
}

func (f *fakeStore) EgressPolicy(context.Context) (egress.Policy, error) { return egress.Policy{}, nil }

// The #1321 review's case: a pass deadline landing mid-send rolled the
// record back after the receiver had the message, and the next pass sent it
// again. A pass stops taking rows once one could outlive its deadline, and a
// row it does take runs on its own bound, detached from the pass's cancel.
func TestAPassTakesNoRowItCouldNotFinish(t *testing.T) {
	f := &fakeStore{}
	short, done := context.WithTimeout(context.Background(), RowBound-time.Second)
	defer done()
	if n, err := New(f, egress.Options{}).Pass(short, 10); err != nil || n != 0 || f.taken != 0 {
		t.Fatalf("a pass with less than a row's bound left took %d rows (%v), want none", f.taken, err)
	}
	long, done2 := context.WithTimeout(context.Background(), 3*RowBound)
	defer done2()
	pass, cancel := context.WithCancel(long)
	n, err := New(f, egress.Options{}).Pass(pass, 3)
	cancel()
	if err != nil || n != 3 {
		t.Fatalf("a pass with room took %d (%v), want 3", n, err)
	}
	for i, bounded := range f.cancelable {
		if !bounded {
			t.Fatalf("row %d ran with no bound of its own", i)
		}
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if n, _ := New(f, egress.Options{}).Pass(cancelled, 3); n != 0 {
		t.Fatalf("a cancelled pass took %d rows, want none", n)
	}
}

// TestAPassEndsExpiredSnoozesBeforeItDrains: the end-of-snooze sweep runs on
// the notifier's tick (#1318, ADR-1318) until the time primitive (#419), and
// first, so a condition announced again at its snooze's end is delivered in
// the same pass.
func TestAPassEndsExpiredSnoozesBeforeItDrains(t *testing.T) {
	f := &fakeStore{}
	ctx, done := context.WithTimeout(context.Background(), 3*RowBound)
	defer done()
	if _, err := New(f, egress.Options{}).Pass(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if len(f.order) < 2 || f.order[0] != "sweep" || f.order[1] != "deliver" {
		t.Fatalf("a pass did %v, want the sweep and then the drain", f.order)
	}
}

// TestAFailedSweepDoesNotStallTheDrain: a snooze left past its deadline waits
// for the next tick, and every other notification is still delivered.
func TestAFailedSweepDoesNotStallTheDrain(t *testing.T) {
	f := &fakeStore{sweepErr: errors.New("the database went away")}
	ctx, done := context.WithTimeout(context.Background(), 3*RowBound)
	defer done()
	if n, err := New(f, egress.Options{}).Pass(ctx, 2); err != nil || n != 2 {
		t.Fatalf("a pass after a failed sweep took %d (%v), want the drain to run", n, err)
	}
}
