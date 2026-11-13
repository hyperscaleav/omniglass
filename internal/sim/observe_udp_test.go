//go:build linux

package sim

import (
	"testing"
	"time"
)

// A datagram face answers a line as one udp request; a datagram the face's
// seeded loss swallows on the way in was received and withheld, never
// answered, so the reconcile reads it as the sim's doing, not the node's.
func TestObservedUDPDatagramAnsweredAndWithheld(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	_, d := panelEngine(t, "127.88.9.41")
	d.SetDatagramLoss(0)
	target := faceAddr(t, d, "nvp-udp")
	c := dialFace(t)
	c.send(target, "GET INPUT")
	if got := c.recv(2 * time.Second); got != "INPUT hdmi1" {
		t.Fatalf("GET INPUT = %q", got)
	}
	got := observedEventually(t, d, "udp", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if got.Received != 1 || got.Answered != 1 || got.InFlight != 0 {
		t.Fatalf("one answered datagram = %+v", got)
	}
	d.SetDatagramLoss(100)
	c.send(target, "GET INPUT")
	if reply := c.recv(200 * time.Millisecond); reply != "" {
		t.Fatalf("a face dropping everything answered %q", reply)
	}
	got = observedEventually(t, d, "udp", func(c ObservationCounters) bool { return c.Settled() == 2 })
	if got.Received != 2 || got.Withheld != 1 || got.Answered != 1 {
		t.Fatalf("a datagram lost on the way in = %+v, want withheld", got)
	}
}

// The bare echo answers each datagram as one request.
func TestObservedUDPEchoAnswersEachDatagram(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	d := newAuthDevice(t, HTTPAuth{})
	addr, err := d.startUDP(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.StopUDP)
	c := dialFace(t)
	for range 2 {
		c.send(addr.String(), "ping")
		if got := c.recv(2 * time.Second); got != "ping" {
			t.Fatalf("echo = %q", got)
		}
	}
	got := observedEventually(t, d, "udp", func(c ObservationCounters) bool { return c.Settled() == 2 })
	if got.Received != 2 || got.Answered != 2 {
		t.Fatalf("two echoed datagrams = %+v", got)
	}
}
