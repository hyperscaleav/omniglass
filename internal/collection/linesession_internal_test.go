package collection

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The held line connection's internals over a real socket: a late interrupt
// landing mid-reply must not truncate the reply (re-review finding 3), a
// failed keepalive is a real cause and not an interrupt (finding 4), and a
// caller's deadline is not the device's timeout (note 5).

func splitServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch strings.TrimRight(line, "\r\n") {
					case "SPLIT":
						_, _ = c.Write([]byte("INPUT hd"))
						time.Sleep(60 * time.Millisecond)
						_, _ = c.Write([]byte("mi1\r\n"))
					case "MUTE":
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestLineConnLateInterruptKeepsThePartialReply(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	addr := splitServer(t)
	hc, err := NewLineSessionDialer().Dial(context.Background(), addr, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hc.Close() }()
	lc := hc.(*lineConn)
	go func() {
		time.Sleep(30 * time.Millisecond) // after the first segment, before the rest
		_ = lc.c.SetReadDeadline(time.Now())
	}()
	got, err := hc.Exchange(context.Background(), "SPLIT", nil, 2*time.Second)
	if err != nil || got != "INPUT hdmi1" {
		t.Fatalf("a reply split across segments with a late interrupt = %q, %v", got, err)
	}
}

func TestLineConnInterruptIsOnlyTheDeadline(t *testing.T) {
	if !isInterrupt(&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}) {
		t.Fatal("a passed read deadline is not taken as the interrupt")
	}
	keepalive := &net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ETIMEDOUT)}
	if isInterrupt(keepalive) {
		t.Fatal("a failed keepalive (ETIMEDOUT) was taken as an interrupt; its cause would be lost")
	}
}

func TestLineConnCallerDeadlineIsNotTheDevicesTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	addr := splitServer(t)
	hc, err := NewLineSessionDialer().Dial(context.Background(), addr, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hc.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := hc.Exchange(ctx, "MUTE", nil, 2*time.Second); err == nil {
		t.Fatal("a mute device answered")
	}
	<-hc.Done() // abandoned: recycled, since its reply may still come
	if errors.Is(hc.Err(), ErrExchangeTimeout) {
		t.Fatalf("the caller's deadline was recorded as the device's timeout: %v", hc.Err())
	}
}

// #817: inbound routing on a held connection. A listener's line is never an
// exchange's answer; a declared reply shape picks the answer out of pushes
// and noise; a line nobody takes is reported unclaimed; a push already in
// the socket when an ask begins is routed, not discarded.

type fakeInbound struct {
	mu        sync.Mutex
	prefix    string
	claimed   []string
	unclaimed []string
}

func (f *fakeInbound) Claim(line string) bool {
	if !strings.HasPrefix(line, f.prefix) {
		return false
	}
	f.mu.Lock()
	f.claimed = append(f.claimed, line)
	f.mu.Unlock()
	return true
}

func (f *fakeInbound) Unclaimed(line string) {
	f.mu.Lock()
	f.unclaimed = append(f.unclaimed, line)
	f.mu.Unlock()
}

func (f *fakeInbound) seen() (claimed, unclaimed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.claimed...), append([]string(nil), f.unclaimed...)
}

func pushServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = c.Write([]byte("EVT INPUT pre\r\n"))
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch strings.TrimRight(line, "\r\n") {
					case "GET INPUT":
						_, _ = c.Write([]byte("EVT INPUT mid\r\nNOISE\r\nINPUT hdmi1\r\n"))
					case "GET BOTH":
						_, _ = c.Write([]byte("EVT INPUT both\r\n"))
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestLineConnRoutesInbound(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ctx := context.Background()
	reply := &driver.Match{Prefix: "INPUT "}

	t.Run("a declared reply shape picks the answer out of pushes and noise", func(t *testing.T) {
		in := &fakeInbound{prefix: "EVT "}
		hc, err := NewLineSessionDialer().Dial(ctx, pushServer(t), time.Second, in)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = hc.Close() }()
		waitFor(t, "the push sent on connect", func() bool { c, _ := in.seen(); return contains(c, "EVT INPUT pre") })
		got, err := hc.Exchange(ctx, "GET INPUT", reply, 2*time.Second)
		if err != nil || got != "INPUT hdmi1" {
			t.Fatalf("answer = %q, %v; want the line matching the reply shape", got, err)
		}
		claimed, unclaimed := in.seen()
		if !contains(claimed, "EVT INPUT mid") {
			t.Fatalf("the push mid-exchange went to %v / %v, want the listener", claimed, unclaimed)
		}
		if !contains(unclaimed, "NOISE") {
			t.Fatalf("unclaimed = %v, want the noise reported", unclaimed)
		}
	})

	t.Run("without a reply shape a listener's line is still never the answer", func(t *testing.T) {
		in := &fakeInbound{prefix: "EVT "}
		hc, err := NewLineSessionDialer().Dial(ctx, pushServer(t), time.Second, in)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = hc.Close() }()
		got, err := hc.Exchange(ctx, "GET INPUT", nil, 2*time.Second)
		if err != nil || got != "NOISE" {
			t.Fatalf("answer = %q, %v; want the first line no listener claims", got, err)
		}
		waitFor(t, "the late answer reported unclaimed", func() bool { _, u := in.seen(); return contains(u, "INPUT hdmi1") })
		if c, _ := in.seen(); !contains(c, "EVT INPUT mid") {
			t.Fatalf("claimed = %v, want the push", c)
		}
	})

	t.Run("a push already waiting when the ask begins is routed, not discarded", func(t *testing.T) {
		in := &fakeInbound{prefix: "EVT "}
		hc, err := NewLineSessionDialer().Dial(ctx, pushServer(t), time.Second, in)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = hc.Close() }()
		if _, err := hc.Exchange(ctx, "GET INPUT", reply, 2*time.Second); err != nil {
			t.Fatal(err)
		}
		if c, _ := in.seen(); !contains(c, "EVT INPUT pre") {
			t.Fatalf("claimed = %v, want the push that was waiting at the ask", c)
		}
	})

	t.Run("a line matching both the reply shape and a listener goes to both", func(t *testing.T) {
		in := &fakeInbound{prefix: "EVT "}
		hc, err := NewLineSessionDialer().Dial(ctx, pushServer(t), time.Second, in)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = hc.Close() }()
		got, err := hc.Exchange(ctx, "GET BOTH", &driver.Match{Prefix: "EVT INPUT b"}, 2*time.Second)
		if err != nil || got != "EVT INPUT both" {
			t.Fatalf("answer = %q, %v", got, err)
		}
		if c, _ := in.seen(); !contains(c, "EVT INPUT both") {
			t.Fatalf("claimed = %v, want the listener to have it too", c)
		}
	})
}
