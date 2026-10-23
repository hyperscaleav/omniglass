package collection_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The telnet reachability probe (#1033) over real sockets: a component that
// sends its negotiation or banner responds; a port that accepts and stays
// silent is reached but not responsive; a closed port is not reached.

func listenWith(t *testing.T, serve func(net.Conn)) string {
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
			go serve(c)
		}
	}()
	return ln.Addr().String()
}

func telnetSamples(t *testing.T, target string) map[string]collection.Sample {
	t.Helper()
	r := &collection.Runner{Telnet: collection.NewTelnetProber()}
	dps, err := r.CollectTelnet(context.Background(), collection.TelnetTask{Target: target, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	out := map[string]collection.Sample{}
	for _, d := range dps {
		out[d.Name] = d
	}
	return out
}

func TestTelnetProbeNegotiationResponds(t *testing.T) {
	addr := listenWith(t, func(c net.Conn) {
		_, _ = c.Write([]byte{0xFF, 0xFB, 0x01})
		time.Sleep(time.Second)
		_ = c.Close()
	})
	dps := telnetSamples(t, addr)
	if dps["tcp-open"].Value != 1 || dps["endpoint-responsive"].Text != "up" {
		t.Fatalf("samples = %+v, want open and responsive", dps)
	}
	if _, ok := dps["telnet-response-time"]; !ok {
		t.Fatal("no telnet-response-time on a responding component")
	}
}

func TestTelnetProbeSilentPortIsReachedButNotResponsive(t *testing.T) {
	addr := listenWith(t, func(c net.Conn) {
		time.Sleep(time.Second)
		_ = c.Close()
	})
	dps := telnetSamples(t, addr)
	if dps["tcp-open"].Value != 1 || dps["endpoint-responsive"].Text != "down" {
		t.Fatalf("samples = %+v, want open and not responsive", dps)
	}
	if _, ok := dps["telnet-response-time"]; ok {
		t.Fatal("a silent port emitted a response time")
	}
}

func TestTelnetProbeClosedPortIsNotReached(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	dps := telnetSamples(t, addr)
	if dps["tcp-open"].Value != 0 || dps["endpoint-responsive"].Text != "down" {
		t.Fatalf("samples = %+v, want closed and not responsive", dps)
	}
}

// Review of #1033, finding 2: a port that accepts and hangs up at once is a
// hang-up, not a timeout.
func TestTelnetProbeHangUpIsNotATimeout(t *testing.T) {
	addr := listenWith(t, func(c net.Conn) { _ = c.Close() })
	dps := telnetSamples(t, addr)
	if got := dps["endpoint-responsive"].Labels[collection.ReasonLabel]; got != string(collection.Closed) {
		t.Fatalf("responsive reason = %q, want closed", got)
	}
}
