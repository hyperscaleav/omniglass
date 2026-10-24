//go:build linux

package sim

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A telnet session over the simulator's panel (#1033, the capability
// carve-out): the panel's nvp-telnet face negotiates options, asks for a
// login with prompts that end no line, and serves the NVP line protocol
// behind it. The collector's real telnet session runs the shared login
// dialog as its auth step (#1049) on establish and on every recover, then
// polls, commands and hears pushes over it; a wrong password is rejected
// after exactly one attempt.

// countingDialer counts the connections a staged dialer makes.
type countingDialer struct {
	collection.StagedDialer
	connects atomic.Int32
}

func (d *countingDialer) Connect(ctx context.Context, target string, timeout time.Duration) (collection.PendingConn, error) {
	d.connects.Add(1)
	return d.StagedDialer.Connect(ctx, target, timeout)
}

func panelLogin(password string) *collection.SessionAuth {
	return &collection.SessionAuth{Login: &collection.LoginDialog{
		Steps: []collection.LoginPrompt{
			{Expect: driver.Match{Regex: `login: ?$`}, Send: "admin"},
			{Expect: driver.Match{Regex: `Password: ?$`}, Send: password},
		},
		Ready: driver.Match{Prefix: "Newtron Panel 7 NVP ready"},
	}}
}

func authRejected(line string) (string, bool) {
	if strings.HasPrefix(line, "auth rejected") {
		return driver.ClassAuth, true
	}
	return "", false
}

func servePanel(t *testing.T, ip string) *Device {
	t.Helper()
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return e.Devices()[0]
}

func awaitSessionState(t *testing.T, states <-chan collection.SessionTransition, want collection.SessionState) collection.SessionTransition {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case tr := <-states:
			if tr.State == want {
				return tr
			}
		case <-deadline:
			t.Fatalf("the session never read %s", want)
		}
	}
}

func TestPanelTelnetSessionLogsInOperatesAndRecovers(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire telnet session test")
	}
	panel := servePanel(t, "127.88.9.5")
	port := facePort(t, panel, "nvp-telnet")
	target := faceAddr(t, panel, "nvp-telnet")

	in := &evtInbound{got: make(chan string, 256)}
	var armMu sync.Mutex
	arms := 0
	states := make(chan collection.SessionTransition, 64)
	verdicts := make(chan collection.AuthVerdict, 8)
	s := collection.StartSession(collection.SessionConfig{
		Target: target, Timeout: 2 * time.Second, Dialer: collection.NewTelnetSessionDialer(),
		Auth: panelLogin("newtron"), Terminal: authRejected,
		Inbound: in, Arm: []string{"SUBSCRIBE INPUT"},
		OnArm: func(string, string, error) {
			armMu.Lock()
			arms++
			armMu.Unlock()
		},
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
		OnAuth:       func(v collection.AuthVerdict) { verdicts <- v },
	})
	defer s.Close()
	awaitSessionState(t, states, collection.SessionUp)
	if v := <-verdicts; !v.Authenticated {
		t.Fatalf("auth verdict = %+v, want authenticated", v)
	}

	// Poll and command over the logged-in session; the command's change is
	// pushed back to the subscribed session too.
	shape := &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}
	if got, err := s.Exchange(context.Background(), "GET INPUT", shape, 2*time.Second); err != nil || got != "INPUT hdmi1" {
		t.Fatalf("poll over telnet = %q, %v", got, err)
	}
	if got, err := s.Exchange(context.Background(), "SET INPUT hdmi2", shape, 2*time.Second); err != nil || got != "INPUT hdmi2" {
		t.Fatalf("command over telnet = %q, %v", got, err)
	}
	select {
	case evt := <-in.got:
		if evt != "EVT INPUT hdmi2" {
			t.Fatalf("push = %q", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the subscribed telnet session heard no push")
	}

	// The panel's telnet listener dies and returns: the session recovers,
	// logging in again before it re-arms and reads up.
	panel.StopTelnet()
	awaitSessionState(t, states, collection.SessionRecovering)
	if err := panel.StartTelnet(port, "admin", "newtron", false); err != nil {
		t.Fatalf("restart the panel's telnet: %v", err)
	}
	awaitSessionState(t, states, collection.SessionUp)
	armMu.Lock()
	n := arms
	armMu.Unlock()
	if n != 2 {
		t.Fatalf("armed %d times, want twice (establish and the recover)", n)
	}
	if got, err := s.Exchange(context.Background(), "GET INPUT", shape, 2*time.Second); err != nil || got != "INPUT hdmi2" {
		t.Fatalf("poll after the recover = %q, %v", got, err)
	}
}

func TestPanelTelnetWrongPasswordIsRejectedAfterOneAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire telnet session test")
	}
	panel := servePanel(t, "127.88.9.6")
	target := faceAddr(t, panel, "nvp-telnet")
	dialer := &countingDialer{StagedDialer: collection.NewTelnetSessionDialer()}
	states := make(chan collection.SessionTransition, 64)
	verdicts := make(chan collection.AuthVerdict, 8)
	s := collection.StartSession(collection.SessionConfig{
		Target: target, Timeout: 2 * time.Second, Dialer: dialer,
		Auth: panelLogin("wrong"), Terminal: authRejected, Arm: []string{"SUBSCRIBE INPUT"},
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
		OnAuth:       func(v collection.AuthVerdict) { verdicts <- v },
	})
	defer s.Close()
	tr := awaitSessionState(t, states, collection.SessionRejected)
	if tr.Reason != "auth: auth rejected" {
		t.Fatalf("rejected reason = %q, want the panel's own text", tr.Reason)
	}
	if v := <-verdicts; v.Authenticated {
		t.Fatalf("auth verdict = %+v, want refused", v)
	}
	time.Sleep(500 * time.Millisecond)
	if n := dialer.connects.Load(); n != 1 {
		t.Fatalf("connected %d times, want exactly 1", n)
	}
	if reg := panel.TelnetSession(); reg != nil && reg.SessionCount() != 0 {
		t.Fatal("a refused login holds a session on the panel")
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", nil, time.Second); err == nil {
		t.Fatal("a poll rode the rejected session")
	} else if _, ok := collection.AsTerminal(err); !ok {
		t.Fatalf("a poll over the rejected session = %v, want the refusal", err)
	}
}
