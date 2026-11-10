package collection_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/nettest"
)

// The real HTTP probe against real listeners (the capability carve-out): the
// fake-based shapes above are necessary but not sufficient, because the point
// of this rung is drawing a real response off a real socket.

func TestHTTPProberReal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	p := collection.NewHTTPProber()
	ctx := context.Background()

	t.Run("a responding server, whatever its status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		res, err := p.Probe(ctx, collection.HTTPTask{Target: srv.Listener.Addr().String(), Timeout: 3 * time.Second})
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Dialed || !res.Responded || res.Status != 500 {
			t.Fatalf("want dialed+responded with the 500, got %+v", res)
		}
		if res.ResponseMS <= 0 || res.DialMS <= 0 {
			t.Fatalf("want positive timings, got %+v", res)
		}
	})

	t.Run("reached but not responsive: the port accepts, the API never answers", func(t *testing.T) {
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
				// Hold the connection open and say nothing: the hang this
				// rung exists to see.
				defer func() { _ = conn.Close() }()
			}
		}()
		res, err := p.Probe(ctx, collection.HTTPTask{Target: ln.Addr().String(), Timeout: 500 * time.Millisecond})
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Dialed || res.Responded {
			t.Fatalf("want dialed and NOT responded, got %+v", res)
		}
	})

	t.Run("port closed classifies on the dial rung", func(t *testing.T) {
		res, err := p.Probe(ctx, collection.HTTPTask{Target: nettest.RefusedAddr(t), Timeout: 2 * time.Second})
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if res.Dialed || res.Responded {
			t.Fatalf("want neither dialed nor responded, got %+v", res)
		}
		if res.Reason != collection.Refused {
			t.Fatalf("reason = %s, want refused", res.Reason)
		}
	})
}

// A probe whose request times out before its TLS dial finishes returns while
// the transport's dial goroutine is still handshaking; that goroutine then
// writes the result it shares with the probe (#1552). Run under -race: the
// probe must not read what the dial goroutine writes without a lock.
func TestHTTPProberTimeoutDuringTheHandshakeDoesNotRace(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	res, err := collection.NewHTTPProber().Probe(context.Background(), collection.HTTPTask{
		Target: ln.Addr().String(), Timeout: 300 * time.Millisecond,
		TLS: &collection.TLSTrust{Mode: driver.TLSModeSkip},
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.Responded {
		t.Fatalf("a server that never answered the handshake responded: %+v", res)
	}

	// The server hangs up now, so the handshake still running behind the
	// returned probe fails and writes its result; give that write a beat to
	// land while the test, and the race detector, still run.
	conn := <-accepted
	_ = conn.Close()
	time.Sleep(200 * time.Millisecond)
}
