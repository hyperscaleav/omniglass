//go:build linux

package sim

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Listeners over a held session (#817), proven against the simulator's line
// protocol (the capability carve-out): the panel's NVP port pushes
// "EVT INPUT <input>" to a subscribed session. The collector's real held
// session arms SUBSCRIBE INPUT, hears a change made elsewhere within a
// second with no poll involved, keeps a GET poll's answer apart from the
// pushes interleaved with it, and re-arms the subscription on recover.

type evtInbound struct {
	mu   sync.Mutex
	evts []string
	got  chan string
}

func (in *evtInbound) Claim(line string) bool {
	if !strings.HasPrefix(line, "EVT ") {
		return false
	}
	in.mu.Lock()
	in.evts = append(in.evts, line)
	in.mu.Unlock()
	select {
	case in.got <- line:
	default:
	}
	return true
}

func (in *evtInbound) Unclaimed(string) {}

func TestPanelListenerHearsPushesAndReArms(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire listener test")
	}
	e := newCatalogEngine(t)
	ip := "127.88.9.4"
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	panel := e.Devices()[0]
	target := net.JoinHostPort(ip, "51325")
	lx := collection.NewLineExchanger()
	set := func(input string) {
		t.Helper()
		if got, err := lx.Exchange(context.Background(), target, "SET INPUT "+input, 2*time.Second); err != nil || got != "INPUT "+input {
			t.Fatalf("SET INPUT %s from another connection = %q, %v", input, got, err)
		}
	}

	in := &evtInbound{got: make(chan string, 256)}
	var armMu sync.Mutex
	var arms []string
	states := make(chan collection.SessionState, 64)
	s := collection.StartSession(collection.SessionConfig{
		Target: target, Timeout: 2 * time.Second, Dialer: collection.NewLineSessionDialer(),
		Inbound: in, Arm: []string{"SUBSCRIBE INPUT"},
		OnArm: func(line, reply string, err error) {
			armMu.Lock()
			arms = append(arms, line+" -> "+reply)
			armMu.Unlock()
		},
		OnTransition: func(tr collection.SessionTransition) { states <- tr.State },
	})
	defer s.Close()
	await := func(want collection.SessionState) {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case st := <-states:
				if st == want {
					return
				}
			case <-deadline:
				t.Fatalf("session never read %s", want)
			}
		}
	}
	hear := func(want string) {
		t.Helper()
		deadline := time.After(time.Second)
		for {
			select {
			case line := <-in.got:
				if line == want {
					return
				}
			case <-deadline:
				in.mu.Lock()
				defer in.mu.Unlock()
				t.Fatalf("no %q pushed within a second; heard %v", want, in.evts)
			}
		}
	}
	await(collection.SessionUp)

	// A change made elsewhere reaches the listener, no poll involved.
	set("hdmi2")
	hear("EVT INPUT hdmi2")

	// A GET poll interleaved with pushes gets its own answer every time.
	reply := &driver.Reply{Prefix: "INPUT "}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		inputs := []string{"hdmi3", "dp1"}
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = lx.Exchange(context.Background(), target, "SET INPUT "+inputs[i%2], time.Second)
		}
	}()
	for i := 0; i < 30; i++ {
		got, err := s.Exchange(context.Background(), "GET INPUT", reply, 2*time.Second)
		if err != nil || !strings.HasPrefix(got, "INPUT ") {
			close(stop)
			<-done
			t.Fatalf("poll %d amid pushes = %q, %v; want its own INPUT answer", i, got, err)
		}
	}
	close(stop)
	<-done

	// Review of #817: the panel refuses an input it does not have with a
	// -ERR line; the seeded error shape answers the command as a refusal
	// carrying that text at once, and the session stays up.
	shaped := &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}
	start := time.Now()
	_, err := s.Exchange(context.Background(), "SET INPUT bogus", shaped, 2*time.Second)
	var refused *collection.RefusedError
	if !errors.As(err, &refused) || !strings.HasPrefix(refused.Line, "-ERR unknown command") {
		t.Fatalf("SET INPUT bogus = %v, want the panel's refusal", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the refusal took %v; it is an answer, not a timeout", took)
	}
	select {
	case st := <-states:
		t.Fatalf("a refusal moved the session to %s", st)
	default:
	}
	if st, _ := s.State(); st != collection.SessionUp {
		t.Fatalf("after a refusal the session reads %s", st)
	}

	// Recover re-arms: the listener dies and returns, the subscription is
	// sent again, and a later change is heard again.
	panel.StopTCPSession()
	await(collection.SessionRecovering)
	if err := panel.StartTCPSession(51325); err != nil {
		t.Fatalf("restart the panel's listener: %v", err)
	}
	await(collection.SessionUp)
	armMu.Lock()
	n := len(arms)
	armMu.Unlock()
	if n != 2 {
		t.Fatalf("arm conversation = %v, want SUBSCRIBE INPUT on both connections", arms)
	}
	for len(in.got) > 0 {
		<-in.got
	}
	set("usbc")
	hear("EVT INPUT usbc")
}
