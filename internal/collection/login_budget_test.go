package collection_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The #1056 ruling: a component that hangs up after the final secret step
// and before ready, with no refusal text, charges a login budget of its own
// (three consecutive); exhausting it is an auth refusal. A drop before the
// secret step stays an ordinary failure, and a login that passes resets the
// budget.

// dropAfter plays a login that hangs up after answering steps answered.
func dropAfter(t *testing.T, answered int) error {
	t.Helper()
	client, dev := net.Pipe()
	defer client.Close()
	go func() {
		defer dev.Close()
		rd := bufio.NewReader(dev)
		_, _ = io.WriteString(dev, "login: ")
		if answered == 0 {
			return
		}
		_, _ = rd.ReadString('\n')
		_, _ = io.WriteString(dev, "Password: ")
		if answered == 1 {
			return
		}
		_, _ = rd.ReadString('\n')
	}()
	return collection.RunLogin(context.Background(), client, panelDialog(), nil, time.Second)
}

func TestLoginDropAfterTheFinalSecretIsMarked(t *testing.T) {
	err := dropAfter(t, 2)
	if !errors.Is(err, collection.ErrLoginDropped) {
		t.Fatalf("a drop after the password = %v, want ErrLoginDropped", err)
	}
	if _, ok := collection.AsTerminal(err); ok {
		t.Fatal("one drop is terminal; the budget decides that")
	}
	for _, answered := range []int{0, 1} {
		if err := dropAfter(t, answered); err == nil || errors.Is(err, collection.ErrLoginDropped) {
			t.Fatalf("a drop after %d steps = %v, want an ordinary failure", answered, err)
		}
	}
}

// dropDialer is a staged dialer whose auth step returns, in order, the
// scripted outcomes (nil passes), counting connections.
type dropDialer struct {
	fakeSessionDialer
	smu      sync.Mutex
	outcomes []error
	connects int
}

func (d *dropDialer) Connect(_ context.Context, target string, timeout time.Duration) (collection.PendingConn, error) {
	d.smu.Lock()
	d.connects++
	var out error
	if len(d.outcomes) > 0 {
		out, d.outcomes = d.outcomes[0], d.outcomes[1:]
	}
	d.smu.Unlock()
	return &dropPending{d: d, target: target, out: out, timeout: timeout}, nil
}

func (d *dropDialer) count() int {
	d.smu.Lock()
	defer d.smu.Unlock()
	return d.connects
}

type dropPending struct {
	d       *dropDialer
	target  string
	out     error
	timeout time.Duration
}

func (p *dropPending) Authenticate(context.Context, *collection.SessionAuth, collection.Classifier, time.Duration) error {
	return p.out
}

func (p *dropPending) Open(in collection.Inbound, _ collection.ChannelOptions) (collection.HeldConn, error) {
	c, err := p.d.fakeSessionDialer.Dial(context.Background(), p.target, p.timeout, in)
	if err != nil {
		return nil, err
	}
	// Each passing login's connection drops at once, so the next attempt
	// runs the login again.
	c.(*fakeHeldConn).kill(errors.New("EOF"))
	return c, nil
}

func (p *dropPending) Close() error { return nil }

func dropped() error {
	return &wrapped{collection.ErrLoginDropped}
}

type wrapped struct{ err error }

func (w *wrapped) Error() string { return "login did not complete: " + w.err.Error() }
func (w *wrapped) Unwrap() error { return w.err }

func startBudgetSession(t *testing.T, d collection.SessionDialer, r *recorder, clock *fakeClock) *collection.Session {
	t.Helper()
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition,
		Backoff: func(int) time.Duration { return 0 },
		After:   clock.After, Now: clock.Now,
		Auth: &collection.SessionAuth{Login: panelDialog()},
	})
	t.Cleanup(s.Close)
	return s
}

func TestThreeLoginDropsRejectAsAuth(t *testing.T) {
	d := &dropDialer{outcomes: []error{dropped(), dropped(), dropped(), dropped(), dropped()}}
	r, clock := newRecorder(), newFakeClock()
	startBudgetSession(t, d, r, clock)
	r.waitStates(t, establishing, rejected)
	tr := r.lastTransition()
	if !strings.HasPrefix(tr.Reason, driver.ClassAuth+": ") || !strings.Contains(tr.Reason, "hung up after the login 3 times") {
		t.Fatalf("rejected reason = %q", tr.Reason)
	}
	if n := d.count(); n != collection.LoginDropBudget {
		t.Fatalf("connected %d times, want %d", n, collection.LoginDropBudget)
	}
	clock.Advance(time.Hour, 30*time.Second)
	if n := d.count(); n != collection.LoginDropBudget {
		t.Fatalf("dialed %d times over an hour of rejection, want %d", n, collection.LoginDropBudget)
	}
}

func TestALoginThatPassesResetsTheDropBudget(t *testing.T) {
	d := &dropDialer{outcomes: []error{dropped(), dropped(), nil, dropped(), dropped(), nil}}
	r, clock := newRecorder(), newFakeClock()
	startBudgetSession(t, d, r, clock)
	deadline := time.Now().Add(3 * time.Second)
	for d.count() < 7 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	for _, st := range r.states() {
		if st == rejected {
			t.Fatalf("drops split by a passing login rejected the session: %v", r.states())
		}
	}
}
