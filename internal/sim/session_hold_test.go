//go:build linux

package sim

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// A session holds its connection (#816), proven against the simulator's line
// protocol (the capability carve-out: the state machine's fakes are
// necessary, this real wire closes the increment). The panel's NVP listener
// is the device; the collector's real held line session is the client. Polls
// over the held session reuse its one connection, and when the device's
// listener dies mid-session the session reads recovering, then up again once
// the listener returns.
func TestPanelHeldSessionSurvivesItsListenerDying(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire session test")
	}
	e := newCatalogEngine(t)
	ip := "127.88.9.3"
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "panel", Hostname: "panel", LoopbackIP: ip,
		ProfileName: "newtron-panel-7", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	var panel *Device
	for _, d := range e.Devices() {
		if d.DeviceID == "panel" {
			panel = d
		}
	}
	if panel == nil {
		t.Fatal("the panel is not served")
	}

	var mu sync.Mutex
	opens := 0
	nvp := faceOf(t, panel, "nvp")
	e.SetSessionPublisher(func(ev SessionEvent) {
		if ev.Kind == "open" && ev.Port == int(nvp.Addr.Port()) {
			mu.Lock()
			opens++
			mu.Unlock()
		}
	})
	openCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return opens
	}

	states := make(chan collection.SessionTransition, 64)
	s := collection.StartSession(collection.SessionConfig{
		Target:       nvp.Addr.String(),
		Timeout:      2 * time.Second,
		Dialer:       collection.NewLineSessionDialer(),
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
	})
	defer s.Close()
	await := func(want collection.SessionState) collection.SessionTransition {
		t.Helper()
		deadline := time.After(20 * time.Second)
		for {
			select {
			case tr := <-states:
				if tr.State == want {
					return tr
				}
			case <-deadline:
				t.Fatalf("session never read %s (now %v)", want, firstOf(s.State()))
			}
		}
	}
	await(collection.SessionEstablishing)
	await(collection.SessionUp)

	// Polls over the held session ride its one connection.
	for i := 0; i < 3; i++ {
		got, err := s.Exchange(context.Background(), "GET INPUT", nil, 2*time.Second)
		if err != nil || got != "INPUT hdmi1" {
			t.Fatalf("poll %d over the held session = %q, %v", i, got, err)
		}
	}
	if n := openCount(); n != 1 {
		t.Fatalf("the panel opened %d sessions for three polls, want the one held", n)
	}

	// Kill the device's listener mid-session: its accepted connections go
	// with it, and the session notices without a poll.
	panel.StopTCPSession()
	lost := await(collection.SessionRecovering)
	if lost.Reason == "" {
		t.Error("recovering carries no reason")
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", nil, 100*time.Millisecond); err == nil {
		t.Fatal("a poll over a recovering session answered")
	}

	// The listener returns; the session reconnects on its own.
	if err := panel.StartTCPSession(int(nvp.Addr.Port())); err != nil {
		t.Fatalf("restart the panel's listener: %v", err)
	}
	await(collection.SessionUp)
	got, err := s.Exchange(context.Background(), "GET INPUT", nil, 2*time.Second)
	if err != nil || got != "INPUT hdmi1" {
		t.Fatalf("poll after recovery = %q, %v", got, err)
	}
	if n := openCount(); n != 2 {
		t.Fatalf("the panel opened %d sessions, want two (the first, and the recovery)", n)
	}

	// Teardown closes the held connection on the device's side too.
	s.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if reg := panel.TCPSession(); reg != nil && reg.SessionCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the panel still holds a session after teardown")
}

func firstOf(st collection.SessionState, _ string) collection.SessionState { return st }
