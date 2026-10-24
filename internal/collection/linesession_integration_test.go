package collection_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/nettest"
)

// The real held line connection over a real socket (the capability
// carve-out): one dial serves many exchanges, a line nobody asked for is not
// taken as an answer, and the peer closing is noticed without a poll.

// lineServer answers "GET INPUT" with "INPUT hdmi1" on a held connection,
// greets with a banner, and counts accepts. Each accepted conn is handed to
// conns so a test can drop it.
func lineServer(t *testing.T, banner string) (addr string, accepts *atomic.Int32, conns chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepts = &atomic.Int32{}
	conns = make(chan net.Conn, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			select {
			case conns <- c:
			default: // nobody is collecting connections: keep serving
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				if banner != "" {
					_, _ = c.Write([]byte(banner + "\r\n"))
				}
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch strings.TrimRight(line, "\r\n") {
					case "GET INPUT":
						_, _ = c.Write([]byte("INPUT hdmi1\r\n"))
					case "SLOW GET INPUT":
						time.Sleep(300 * time.Millisecond)
						_, _ = c.Write([]byte("INPUT slow\r\n"))
					case "GET VOLUME":
						_, _ = c.Write([]byte("VOLUME 40\r\n"))
					case "FLOOD":
						_, _ = c.Write([]byte(strings.Repeat("x", 128*1024)))
					default:
						_, _ = c.Write([]byte("ERR\r\n"))
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String(), accepts, conns
}

func TestLineSessionReal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ctx := context.Background()
	d := collection.NewLineSessionDialer()

	t.Run("one dial serves many exchanges, a banner is not an answer", func(t *testing.T) {
		addr, accepts, _ := lineServer(t, "PANEL READY")
		conn, err := d.Dial(ctx, addr, 2*time.Second, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(50 * time.Millisecond) // let the banner arrive before the first ask
		for i := 0; i < 3; i++ {
			got, err := conn.Exchange(ctx, "GET INPUT", nil, 2*time.Second)
			if err != nil {
				t.Fatalf("exchange %d: %v", i, err)
			}
			if got != "INPUT hdmi1" {
				t.Fatalf("exchange %d = %q, want the answer, not the banner", i, got)
			}
		}
		if n := accepts.Load(); n != 1 {
			t.Fatalf("server accepted %d connections for three exchanges, want 1", n)
		}
	})

	t.Run("the peer closing is noticed without a poll", func(t *testing.T) {
		addr, _, conns := lineServer(t, "")
		conn, err := d.Dial(ctx, addr, 2*time.Second, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		(<-conns).Close()
		select {
		case <-conn.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("a closed peer was not noticed")
		}
		if conn.Err() == nil {
			t.Error("a dead connection carries no cause")
		}
		if _, err := conn.Exchange(ctx, "GET INPUT", nil, time.Second); err == nil {
			t.Fatal("an exchange over a dead connection succeeded")
		}
	})

	t.Run("an unterminated flood kills the connection, bounded", func(t *testing.T) {
		addr, _, _ := lineServer(t, "")
		conn, err := d.Dial(ctx, addr, 2*time.Second, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.Exchange(ctx, "FLOOD", nil, time.Second); err == nil {
			t.Fatal("an unterminated flood answered")
		}
		select {
		case <-conn.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("an unterminated flood left the connection open")
		}
	})

	t.Run("each exchange takes exactly its own reply", func(t *testing.T) {
		addr, _, _ := lineServer(t, "")
		conn, err := d.Dial(ctx, addr, 2*time.Second, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		// An unknown command draws ERR, a known one its answer: an
		// exchange takes exactly its own reply.
		if got, err := conn.Exchange(ctx, "NOPE", nil, time.Second); err != nil || got != "ERR" {
			t.Fatalf("unknown = %q, %v", got, err)
		}
		if got, err := conn.Exchange(ctx, "GET INPUT", nil, time.Second); err != nil || got != "INPUT hdmi1" {
			t.Fatalf("after ERR = %q, %v", got, err)
		}
	})

	// Review finding 2: a banner the device sends on connect sits in the
	// socket before the first ask; it is never that ask's answer.
	t.Run("a banner on connect is never an answer", func(t *testing.T) {
		addr, _, _ := lineServer(t, "PANEL READY")
		for i := 0; i < 50; i++ {
			conn, err := d.Dial(ctx, addr, 2*time.Second, nil)
			if err != nil {
				t.Fatalf("dial %d: %v", i, err)
			}
			got, err := conn.Exchange(ctx, "GET INPUT", nil, 2*time.Second)
			_ = conn.Close()
			if err != nil || got != "INPUT hdmi1" {
				t.Fatalf("dial %d: first exchange = %q, %v; the banner was taken as the answer", i, got, err)
			}
		}
	})

	// Review finding 1: an exchange that times out after its write recycles
	// the connection, since its late reply would answer the next ask.
	t.Run("a timed-out exchange recycles the connection", func(t *testing.T) {
		addr, _, _ := lineServer(t, "")
		conn, err := d.Dial(ctx, addr, 2*time.Second, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.Exchange(ctx, "SLOW GET INPUT", nil, 100*time.Millisecond); err == nil {
			t.Fatal("a slow answer arrived inside a 100ms timeout")
		}
		select {
		case <-conn.Done():
		case <-time.After(time.Second):
			t.Fatal("a timed-out exchange left its connection held")
		}
		if !errors.Is(conn.Err(), collection.ErrExchangeTimeout) {
			t.Fatalf("cause = %v, want the exchange timeout", conn.Err())
		}
	})

	t.Run("a refused dial is an error", func(t *testing.T) {
		if _, err := d.Dial(ctx, nettest.RefusedAddr(t), time.Second, nil); err == nil {
			t.Fatal("dialing a closed port succeeded")
		}
	})
}

// Review finding 1 at the session: the late reply of a timed-out ask never
// answers the next one. SLOW GET INPUT times out, the session recycles its
// connection, and GET VOLUME gets VOLUME, not the stale INPUT.
func TestHeldSessionTimeoutNeverAnswersTheNextAsk(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	addr, _, _ := lineServer(t, "")
	states := make(chan collection.SessionState, 64)
	s := collection.StartSession(collection.SessionConfig{
		Target: addr, Timeout: time.Second, Dialer: collection.NewLineSessionDialer(),
		Backoff:      func(int) time.Duration { return time.Millisecond },
		OnTransition: func(tr collection.SessionTransition) { states <- tr.State },
	})
	defer s.Close()
	awaitState(t, states, collection.SessionUp)
	if _, err := s.Exchange(context.Background(), "SLOW GET INPUT", nil, 100*time.Millisecond); err == nil {
		t.Fatal("a slow answer arrived inside a 100ms timeout")
	}
	awaitState(t, states, collection.SessionRecovering)
	awaitState(t, states, collection.SessionUp)
	time.Sleep(350 * time.Millisecond) // past the slow reply's arrival
	got, err := s.Exchange(context.Background(), "GET VOLUME", nil, time.Second)
	if err != nil || got != "VOLUME 40" {
		t.Fatalf("the ask after a timeout = %q, %v; want its own answer", got, err)
	}
}

// Review finding 1, the hung application: a device that accepts and never
// answers reads recovering and then down, never up forever.
func TestHeldSessionToAHungDeviceReadsDown(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { // reads every ask, answers none
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	states := make(chan collection.SessionState, 256)
	s := collection.StartSession(collection.SessionConfig{
		Target: ln.Addr().String(), Timeout: time.Second, Dialer: collection.NewLineSessionDialer(),
		// Every connection outlives the stable window by this clock, so only
		// the timeout itself can count the failures.
		Now:          steppingClock(collection.SessionStableAfter + time.Second),
		Backoff:      func(int) time.Duration { return time.Millisecond },
		OnTransition: func(tr collection.SessionTransition) { states <- tr.State },
	})
	defer s.Close()
	sawRecovering := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = s.Exchange(context.Background(), "GET INPUT", nil, 30*time.Millisecond)
		for drained := false; !drained; {
			select {
			case st := <-states:
				if st == collection.SessionRecovering {
					sawRecovering = true
				}
				if st == collection.SessionDown {
					if !sawRecovering {
						t.Fatal("down without recovering first")
					}
					return
				}
			default:
				drained = true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	st, _ := s.State()
	t.Fatalf("a hung device never read down (now %s)", st)
}

func awaitState(t *testing.T, states chan collection.SessionState, want collection.SessionState) {
	t.Helper()
	deadline := time.After(5 * time.Second)
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
