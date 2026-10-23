//go:build linux

package sim

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// An ssh session over the simulator's panel (#1032, the capability
// carve-out): the panel's ssh-console serves the NVP line protocol on its
// persistent shell. The collector's real ssh session authenticates as its
// auth step (#1049), pins the host key on first use, then polls, commands and
// hears pushes over the shell; it recovers on a dropped channel, reads down
// after the budget, and reads rejected for a wrong password (auth) or a
// changed host key (host-key).

func panelSSH(t *testing.T, ip, password string, pin *collection.HostKeyPin, dialer collection.SessionDialer, in collection.Inbound, arm []string) (*collection.Session, chan collection.SessionTransition) {
	t.Helper()
	states := make(chan collection.SessionTransition, 64)
	s := collection.StartSession(collection.SessionConfig{
		Target: net.JoinHostPort(ip, fmt.Sprint(22+syncPortShift)), Timeout: 2 * time.Second, Dialer: dialer,
		Auth:    &collection.SessionAuth{SSH: &collection.SSHAuth{Username: "admin", Password: password, Pin: pin}},
		Inbound: in, Arm: arm,
		Backoff:      func(int) time.Duration { return 20 * time.Millisecond },
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
	})
	t.Cleanup(s.Close)
	return s, states
}

func TestPanelSSHSessionOperatesRecoversAndReadsDown(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire ssh session test")
	}
	ip := "127.88.9.7"
	panel := servePanel(t, ip)
	var trustMu sync.Mutex
	var trusted []string
	pin := collection.NewHostKeyPin("")
	pin.OnTrust = func(fp string) {
		trustMu.Lock()
		trusted = append(trusted, fp)
		trustMu.Unlock()
	}
	in := &evtInbound{got: make(chan string, 256)}
	s, states := panelSSH(t, ip, "newtron", pin, collection.NewSSHSessionDialer(), in, []string{"SUBSCRIBE INPUT"})
	awaitSessionState(t, states, collection.SessionUp)
	if want := ssh.FingerprintSHA256(panel.SSHHostKey()); pin.Get() != want {
		t.Fatalf("pinned %q on first use, want the panel's key %q", pin.Get(), want)
	}

	shape := &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}
	if got, err := s.Exchange(context.Background(), "GET INPUT", shape, 2*time.Second); err != nil || got != "INPUT hdmi1" {
		t.Fatalf("poll over ssh = %q, %v", got, err)
	}
	if got, err := s.Exchange(context.Background(), "SET INPUT dp1", shape, 2*time.Second); err != nil || got != "INPUT dp1" {
		t.Fatalf("command over ssh = %q, %v", got, err)
	}
	select {
	case evt := <-in.got:
		if evt != "EVT INPUT dp1" {
			t.Fatalf("push = %q", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the subscribed ssh session heard no push")
	}

	// The channel drops (the listener and its connections die) and returns
	// with the same key: the session recovers onto the same pin.
	panel.StopSSH()
	awaitSessionState(t, states, collection.SessionRecovering)
	if err := panel.StartSSH(22+syncPortShift, "admin", "newtron", false); err != nil {
		t.Fatal(err)
	}
	awaitSessionState(t, states, collection.SessionUp)
	if got, err := s.Exchange(context.Background(), "GET INPUT", shape, 2*time.Second); err != nil || got != "INPUT dp1" {
		t.Fatalf("poll after the recover = %q, %v", got, err)
	}

	// Gone for good: down after the budget.
	panel.StopSSH()
	awaitSessionState(t, states, collection.SessionDown)
}

func TestPanelSSHWrongPasswordIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire ssh session test")
	}
	ip := "127.88.9.8"
	servePanel(t, ip)
	dialer := &countingDialer{StagedDialer: collection.NewSSHSessionDialer()}
	s, states := panelSSH(t, ip, "wrong", collection.NewHostKeyPin(""), dialer, nil, nil)
	tr := awaitSessionState(t, states, collection.SessionRejected)
	if !strings.HasPrefix(tr.Reason, "auth: the component refused the credential") || strings.Contains(tr.Reason, "wrong") {
		t.Fatalf("rejected reason = %q", tr.Reason)
	}
	time.Sleep(300 * time.Millisecond)
	if n := dialer.connects.Load(); n != 1 {
		t.Fatalf("connected %d times, want exactly 1", n)
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", nil, time.Second); err == nil {
		t.Fatal("a poll rode the rejected session")
	}
}

func TestPanelSSHHostKeyChangeIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire ssh session test")
	}
	ip := "127.88.9.9"
	panel := servePanel(t, ip)
	pin := collection.NewHostKeyPin("")
	_, states := panelSSH(t, ip, "newtron", pin, collection.NewSSHSessionDialer(), nil, nil)
	awaitSessionState(t, states, collection.SessionUp)
	pinned := pin.Get()

	// The panel is replaced: same address, new key.
	panel.StopSSH()
	if err := panel.RotateSSHHostKey(); err != nil {
		t.Fatal(err)
	}
	if err := panel.StartSSH(22+syncPortShift, "admin", "newtron", false); err != nil {
		t.Fatal(err)
	}
	tr := awaitSessionState(t, states, collection.SessionRejected)
	offered := ssh.FingerprintSHA256(panel.SSHHostKey())
	if !strings.HasPrefix(tr.Reason, "host-key: ") || !strings.Contains(tr.Reason, pinned) || !strings.Contains(tr.Reason, offered) {
		t.Fatalf("rejected reason = %q, want host-key naming %s and %s", tr.Reason, pinned, offered)
	}
	if pin.Get() != pinned {
		t.Fatal("a changed key replaced the pin")
	}
}
