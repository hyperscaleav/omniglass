package collection_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
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
			conns <- c
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
		conn, err := d.Dial(ctx, addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(50 * time.Millisecond) // let the banner arrive before the first ask
		for i := 0; i < 3; i++ {
			got, err := conn.Exchange(ctx, "GET INPUT", 2*time.Second)
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
		conn, err := d.Dial(ctx, addr, 2*time.Second)
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
		if _, err := conn.Exchange(ctx, "GET INPUT", time.Second); err == nil {
			t.Fatal("an exchange over a dead connection succeeded")
		}
	})

	t.Run("an unterminated flood kills the connection, bounded", func(t *testing.T) {
		addr, _, _ := lineServer(t, "")
		conn, err := d.Dial(ctx, addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.Exchange(ctx, "FLOOD", time.Second); err == nil {
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
		conn, err := d.Dial(ctx, addr, 2*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		// An unknown command draws ERR, a known one its answer: an
		// exchange takes exactly its own reply.
		if got, err := conn.Exchange(ctx, "NOPE", time.Second); err != nil || got != "ERR" {
			t.Fatalf("unknown = %q, %v", got, err)
		}
		if got, err := conn.Exchange(ctx, "GET INPUT", time.Second); err != nil || got != "INPUT hdmi1" {
			t.Fatalf("after ERR = %q, %v", got, err)
		}
	})

	t.Run("a refused dial is an error", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		if _, err := d.Dial(ctx, addr, time.Second); err == nil {
			t.Fatal("dialing a closed port succeeded")
		}
	})
}
