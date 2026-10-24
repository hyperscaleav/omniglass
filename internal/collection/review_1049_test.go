package collection_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Regressions from the #1049 adversarial review, each failing before its fix.

// Finding 1: a component that answers with a terminal reply and hangs up
// at once must reject the session, never read as a drop to redial into.
func TestTerminalReplyThenHangUpRejects(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire test")
	}
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					rd := bufio.NewReader(c)
					if _, err := rd.ReadString('\n'); err == nil {
						_, _ = io.WriteString(c, "LOCKED account\r\n")
					}
					_ = c.Close()
				}(c)
			}
		}()
		states := make(chan collection.SessionTransition, 64)
		s := collection.StartSession(collection.SessionConfig{
			Target: ln.Addr().String(), Timeout: time.Second, Dialer: collection.NewLineSessionDialer(),
			Terminal: func(line string) (string, bool) {
				if strings.HasPrefix(line, "LOCKED") {
					return driver.ClassAuth, true
				}
				return "", false
			},
			OnTransition: func(tr collection.SessionTransition) { states <- tr },
		})
		awaitTransition(t, states, collection.SessionUp)
		_, err = s.Exchange(context.Background(), "GET INPUT", nil, time.Second)
		if _, ok := collection.AsTerminal(err); !ok {
			t.Fatalf("run %d: exchange = %v, want the terminal reply", i, err)
		}
		awaitTransition(t, states, collection.SessionRejected)
		time.Sleep(50 * time.Millisecond)
		if st, _ := s.State(); st != collection.SessionRejected {
			t.Fatalf("run %d: the session left rejected on its own: %s", i, st)
		}
		s.Close()
		_ = ln.Close()
	}
}

// Finding 2: a component that echoes what it is sent must never carry the
// credential into an error or a reason.
func TestLoginEchoNeverLeaksTheSecret(t *testing.T) {
	for _, script := range []string{
		"echo then drop",     // the ready never comes: a retryable error naming the last line
		"echo then reprompt", // the echo runs into the prompt: a terminal reason
	} {
		client, dev := net.Pipe()
		go func() {
			defer dev.Close()
			rd := bufio.NewReader(dev)
			_, _ = io.WriteString(dev, "login: ")
			u, _ := rd.ReadString('\n')
			_, _ = io.WriteString(dev, strings.TrimRight(u, "\r\n")+"\r\nPassword: ")
			p, _ := rd.ReadString('\n')
			p = strings.TrimRight(p, "\r\n")
			if script == "echo then drop" {
				_, _ = io.WriteString(dev, p+"\r\n")
				return
			}
			_, _ = io.WriteString(dev, p+"login: ")
			_, _ = io.ReadAll(rd)
		}()
		d := panelDialog()
		d.Steps[1].Send = "hunter2"
		err := collection.RunLogin(context.Background(), client, d, nil, time.Second)
		_ = client.Close()
		if err == nil {
			t.Fatalf("%s: the login passed", script)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("%s: the error carries the secret: %v", script, err)
		}
	}
}

// Finding 3: a banner arriving in pieces is judged as a line, not at
// whatever byte boundary a read returns: "Last login: " split from its date
// is not the login prompt again.
func TestLoginSplitBannerIsNotAReprompt(t *testing.T) {
	client, dev := net.Pipe()
	defer client.Close()
	go func() {
		defer dev.Close()
		rd := bufio.NewReader(dev)
		_, _ = io.WriteString(dev, "login: ")
		_, _ = rd.ReadString('\n')
		_, _ = io.WriteString(dev, "Password: ")
		_, _ = rd.ReadString('\n')
		_, _ = io.WriteString(dev, "\r\nLast login: ")
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(dev, "Thu Oct 22 09:00\r\nREADY\r\n")
		_, _ = io.ReadAll(rd)
	}()
	d := panelDialog()
	if err := collection.RunLogin(context.Background(), client, d, nil, 2*time.Second); err != nil {
		t.Fatalf("a split banner failed the login: %v", err)
	}
}

// Finding 4: a second retry while the first retry's attempt is in flight
// does not buy a third dial.
func TestRetryDuringARetryDoesNotDialTwice(t *testing.T) {
	d := &terminalDialer{refusal: authRefusal()}
	r, clock := newRecorder(), newFakeClock()
	s := startRejectingSession(t, d, r, clock, 0)
	r.waitStates(t, establishing, rejected)
	s.Retry()
	s.Retry()
	r.waitStates(t, establishing, rejected, rejected)
	clock.Advance(time.Hour, time.Minute)
	if n := d.dialCount(); n > 3 {
		t.Fatalf("dialed %d times", n)
	}
	if n := d.dialCount(); n != 2 {
		t.Fatalf("two quick retries dialed %d times, want 2 (one retry attempt)", n)
	}
}

// Finding 4: a retry whose attempt meets a transient failure leaves
// rejected, so the stale refusal is not reported while the session dials on
// its backoff.
func TestRetryMeetingATransientFailureLeavesRejected(t *testing.T) {
	d := &terminalDialer{refusal: authRefusal()}
	r, clock := newRecorder(), newFakeClock()
	s := startRejectingSession(t, d, r, clock, 0)
	r.waitStates(t, establishing, rejected)
	d.accept()
	d.setRefuse(true)
	s.Retry()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.State(); st != collection.SessionRejected {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a retry that met a transient failure still reads rejected with the old refusal")
}
