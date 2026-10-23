package collection

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
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
	hc, err := NewLineSessionDialer().Dial(context.Background(), addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hc.Close() }()
	lc := hc.(*lineConn)
	go func() {
		time.Sleep(30 * time.Millisecond) // after the first segment, before the rest
		_ = lc.c.SetReadDeadline(time.Now())
	}()
	got, err := hc.Exchange(context.Background(), "SPLIT", 2*time.Second)
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
	hc, err := NewLineSessionDialer().Dial(context.Background(), addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hc.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := hc.Exchange(ctx, "MUTE", 2*time.Second); err == nil {
		t.Fatal("a mute device answered")
	}
	<-hc.Done() // abandoned: recycled, since its reply may still come
	if errors.Is(hc.Err(), ErrExchangeTimeout) {
		t.Fatalf("the caller's deadline was recorded as the device's timeout: %v", hc.Err())
	}
}
