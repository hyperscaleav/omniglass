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

// TestHTTPSProbeCancelledMidHandshakeReadsNoLaterDialWrite is a probe whose
// request ends (its context cancelled) while the TLS handshake still waits on
// a silent device: the transport's dial outlives the request, and what it
// learns afterwards must not race what the probe already returned. Run under
// -race, the write the handshake makes once its own timeout fires is the
// regression.
func TestHTTPSProbeCancelledMidHandshakeReadsNoLaterDialWrite(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Say nothing until the handshake gives up and hangs up.
		for {
			if _, err := conn.Read(make([]byte, 1<<16)); err != nil {
				break
			}
		}
		_ = conn.Close()
	}()
	t.Cleanup(func() { _ = ln.Close() })

	const handshake = 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res, err := collection.NewHTTPProber().Probe(ctx, collection.HTTPTask{
		Target: ln.Addr().String(), Timeout: handshake,
		TLS: &collection.TLSTrust{Mode: driver.TLSModeSkip}})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.Responded || res.Accepted {
		t.Fatalf("a silent device answered: %+v", res)
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the handshake never gave up on the silent device")
	}
	// The dial goroutine writes its handshake facts just before it hangs
	// up; give it the moment to land them.
	time.Sleep(50 * time.Millisecond)
	if res.Handshaken {
		t.Fatalf("the probe saw a handshake that never finished: %+v", res)
	}
}
