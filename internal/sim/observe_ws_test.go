package sim

import (
	"testing"
)

// A websocket upgrade is a stream, opened and never in flight; each frame
// the face answers is one request; an upgrade the face's auth refuses is one
// refused request; the socket's connection is accepted and closes with it.
func TestObservedWebSocketFramesUpgradeAndRefusal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket test")
	}
	dev, addr := newWSDevice(t, HTTPAuth{Mode: HTTPAuthBasic, User: "admin", Password: "kestrel"}, false)
	if _, _, err := dialWS(t, "ws://"+addr+"/api/ws", basicHeader("admin", "wrong")); err == nil {
		t.Fatal("a wrong credential upgraded")
	}
	got := observedEventually(t, dev, "websocket", func(c ObservationCounters) bool { return c.Settled() == 1 })
	if got.Refused != 1 || got.StreamsOpened != 0 {
		t.Fatalf("a refused upgrade = %+v", got)
	}
	c, _, err := dialWS(t, "ws://"+addr+"/api/ws", basicHeader("admin", "kestrel"))
	if err != nil {
		t.Fatal(err)
	}
	got = observedEventually(t, dev, "websocket", func(c ObservationCounters) bool { return c.OpenStreams == 1 })
	if got.StreamsOpened != 1 || got.InFlight != 0 || got.Received != 1 {
		t.Fatalf("an open socket = %+v, want a stream, not a request", got)
	}
	send(t, c, `{"type":"get","id":"1","path":"power"}`)
	if m := next(t, c); m["type"] != "result" {
		t.Fatalf("get = %v", m)
	}
	send(t, c, `{"type":"get","id":"2","path":"nope"}`)
	if m := next(t, c); m["type"] != "error" {
		t.Fatalf("an unknown path = %v", m)
	}
	got = observedEventually(t, dev, "websocket", func(c ObservationCounters) bool { return c.Settled() == 3 })
	if got.Answered != 1 || got.Error != 1 || got.Refused != 1 || got.Received != 3 {
		t.Fatalf("two frames = %+v, want one answered and one error", got)
	}
	_ = c.Close()
	got = observedEventually(t, dev, "websocket", func(c ObservationCounters) bool { return c.OpenStreams == 0 && c.OpenConnections == 0 })
	if got.Accepted != 2 {
		t.Fatalf("after the socket closed: %+v, want both connections accepted", got)
	}
}
