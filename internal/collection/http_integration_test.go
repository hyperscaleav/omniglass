package collection_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
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
		res, err := p.Probe(ctx, srv.Listener.Addr().String(), 3*time.Second)
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
		res, err := p.Probe(ctx, ln.Addr().String(), 500*time.Millisecond)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if !res.Dialed || res.Responded {
			t.Fatalf("want dialed and NOT responded, got %+v", res)
		}
	})

	t.Run("port closed classifies on the dial rung", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		res, err := p.Probe(ctx, addr, 2*time.Second)
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
