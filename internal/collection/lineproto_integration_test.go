package collection_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The real line exchanger against a real line server (the capability
// carve-out): a genuine dial, a genuine request line, a genuine answer line.

func TestLineExchangerReal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(line, "\r\n") == "GET INPUT" {
					_, _ = c.Write([]byte("INPUT hdmi-2\r\n"))
					return
				}
				_, _ = c.Write([]byte("ERR unknown command\r\n"))
			}(conn)
		}
	}()

	e := collection.NewLineExchanger()
	ctx := context.Background()

	t.Run("one ask, one answer", func(t *testing.T) {
		got, err := e.Exchange(ctx, ln.Addr().String(), "GET INPUT", 3*time.Second)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if got != "INPUT hdmi-2" {
			t.Fatalf("answer = %q", got)
		}
	})

	t.Run("a server that never answers is an error", func(t *testing.T) {
		mute, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer func() { _ = mute.Close() }()
		go func() {
			for {
				conn, err := mute.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
			}
		}()
		if _, err := e.Exchange(ctx, mute.Addr().String(), "GET INPUT", 500*time.Millisecond); err == nil {
			t.Fatal("a mute server answered")
		}
	})
}
