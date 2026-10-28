package notifier

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fakeStore counts the rows a pass takes and records whether each row's
// context could be cancelled by the pass.
type fakeStore struct {
	taken      int
	cancelable []bool
}

func (f *fakeStore) DeliverNext(ctx context.Context, _ storage.SendFunc) (bool, error) {
	f.taken++
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
