package collection_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A raw tcp device with a password prompt, over a real socket (the
// capability carve-out for the login dialog, #1049): the session runs the
// declared login as its auth step on every connection before anything
// rides it, a wrong password rejects the session after exactly one attempt,
// and the right one serves polls.

// promptServer greets "login: " and "Password: " without a line terminator,
// answers READY or "auth rejected" (then hangs up), and after login answers
// GET INPUT. password is read per connection, so a test can fix it.
type promptServer struct {
	addr     string
	accepts  atomic.Int32
	mu       sync.Mutex
	password string
	got      []string // every line the device was sent, logins included
}

func (p *promptServer) setPassword(pw string) {
	p.mu.Lock()
	p.password = pw
	p.mu.Unlock()
}

func (p *promptServer) lines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.got...)
}

func newPromptServer(t *testing.T, password string) *promptServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p := &promptServer{addr: ln.Addr().String(), password: password}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			p.accepts.Add(1)
			go p.serve(c)
		}
	}()
	return p
}

func (p *promptServer) serve(c net.Conn) {
	defer func() { _ = c.Close() }()
	rd := bufio.NewReader(c)
	read := func() (string, bool) {
		l, err := rd.ReadString('\n')
		if err != nil {
			return "", false
		}
		l = strings.TrimRight(l, "\r\n")
		p.mu.Lock()
		p.got = append(p.got, l)
		p.mu.Unlock()
		return l, true
	}
	_, _ = io.WriteString(c, "Panel 7 line control\r\nlogin: ")
	user, ok := read()
	if !ok {
		return
	}
	_, _ = io.WriteString(c, "Password: ")
	pass, ok := read()
	if !ok {
		return
	}
	p.mu.Lock()
	want := p.password
	p.mu.Unlock()
	if user != "admin" || pass != want {
		_, _ = io.WriteString(c, "\r\nauth rejected\r\n")
		return
	}
	_, _ = io.WriteString(c, "\r\nREADY\r\n")
	for {
		l, ok := read()
		if !ok {
			return
		}
		if l == "GET INPUT" {
			_, _ = io.WriteString(c, "INPUT hdmi1\r\n")
		} else {
			_, _ = io.WriteString(c, "-ERR\r\n")
		}
	}
}

func loginTo(password string) *collection.SessionAuth {
	d := panelDialog()
	d.Steps[1].Send = password
	return &collection.SessionAuth{Login: d}
}

func TestLineSessionLoginOverRawTCP(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire login test")
	}
	srv := newPromptServer(t, "newtron")
	states := make(chan collection.SessionTransition, 16)
	verdicts := make(chan collection.AuthVerdict, 4)
	s := collection.StartSession(collection.SessionConfig{
		Target: srv.addr, Timeout: 2 * time.Second, Dialer: collection.NewLineSessionDialer(),
		Auth: loginTo("newtron"), Terminal: rejectClassifier,
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
		OnAuth:       func(v collection.AuthVerdict) { verdicts <- v },
	})
	defer s.Close()
	awaitTransition(t, states, collection.SessionUp)
	if v := <-verdicts; !v.Authenticated {
		t.Fatalf("verdict = %+v, want authenticated", v)
	}
	got, err := s.Exchange(context.Background(), "GET INPUT", &driver.Reply{Prefix: "INPUT "}, 2*time.Second)
	if err != nil || got != "INPUT hdmi1" {
		t.Fatalf("poll after login = %q, %v", got, err)
	}
	if lines := srv.lines(); len(lines) < 3 || lines[0] != "admin" || lines[1] != "newtron" || lines[2] != "GET INPUT" {
		t.Fatalf("the device heard %q, want the login before the poll", lines)
	}
}

func TestLineSessionWrongPasswordRejectsAfterOneAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("real-wire login test")
	}
	srv := newPromptServer(t, "newtron")
	states := make(chan collection.SessionTransition, 16)
	verdicts := make(chan collection.AuthVerdict, 4)
	s := collection.StartSession(collection.SessionConfig{
		Target: srv.addr, Timeout: 2 * time.Second, Dialer: collection.NewLineSessionDialer(),
		Auth: loginTo("wrong"), Terminal: rejectClassifier, Arm: []string{"SUBSCRIBE INPUT"},
		OnTransition: func(tr collection.SessionTransition) { states <- tr },
		OnAuth:       func(v collection.AuthVerdict) { verdicts <- v },
	})
	defer s.Close()
	tr := awaitTransition(t, states, collection.SessionRejected)
	if tr.Reason != "auth: auth rejected" {
		t.Fatalf("rejected reason = %q", tr.Reason)
	}
	if v := <-verdicts; v.Authenticated || v.Reason != "auth: auth rejected" {
		t.Fatalf("verdict = %+v, want refused with the device text", v)
	}
	time.Sleep(300 * time.Millisecond)
	if n := srv.accepts.Load(); n != 1 {
		t.Fatalf("the device was dialed %d times, want exactly 1", n)
	}
	for _, l := range srv.lines() {
		if l == "SUBSCRIBE INPUT" {
			t.Fatal("a listener armed over a session whose login was refused")
		}
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", nil, time.Second); err == nil {
		t.Fatal("a poll rode a rejected session")
	}
	// The fixed credential is a new session (the node recycles on a
	// credential change); a retry of this one, with the device fixed, reads
	// up too.
	srv.setPassword("wrong")
	s.Retry()
	awaitTransition(t, states, collection.SessionUp)
}

func awaitTransition(t *testing.T, states <-chan collection.SessionTransition, want collection.SessionState) collection.SessionTransition {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case tr := <-states:
			if tr.State == want {
				return tr
			}
		case <-deadline:
			t.Fatalf("session never read %s", want)
		}
	}
}
