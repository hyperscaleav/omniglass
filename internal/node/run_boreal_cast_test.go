//go:build linux

package node_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeBorealCastReadsItsPlayback (#893): the shipped boreal-cast-api
// driver on the simulated media player's http face, its key in a header,
// lands the player's playback state, the item it is playing and how full
// its storage is, beside its identity. Paused on the player, it reads
// paused on the next poll.
func TestNodeBorealCastReadsItsPlayback(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var cast string
	s := newHTTPStack(t, "edge-cast", nil, func(store *profile.Store) []string {
		cast = driverProfile(t, store, "boreal-cast-api")
		return []string{cast}
	})
	s.secret("cast-key", "api-key", map[string]string{"key": "boreal-cast-key"})
	s.component("player-1")
	dev := s.devices[cast]
	ep := s.attachHTTP("player-1", "boreal-cast-api", "edge-cast", map[string]string{
		"host": s.ips[cast], "port": fmt.Sprint(s.httpPort(cast)), "key": "cast-key"})
	s.runNode("edge-cast")

	s.waitFor("player-1", ep.Name, "endpoint-authenticated", is("yes"), "yes: the key accepted")
	s.waitFor("player-1", ep.Name, "model-number", is("Cast M1"), "the model")
	s.waitFor("player-1", ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), "the serial")
	s.waitFor("player-1", ep.Name, "firmware-version", is(fmt.Sprint(dev.ReadPath("configuration.firmware.version"))), "the firmware")
	s.waitFor("player-1", ep.Name, "playback-state", is("playing"), "playing at rest")
	s.waitFor("player-1", ep.Name, "content-name", func(v string) bool { return v != "" }, "the item playing")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if u, ok := s.metric("player-1", ep.Name, "storage-used"); ok && u > 0 && u < 100 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the player's poll landed no storage-used")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if res := dev.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/playback", Body: `{"state": "paused"}`}); res.Status != 200 {
		t.Fatalf("pause: %+v", res)
	}
	s.runNode("edge-cast") // a restarted node polls at once, rather than in 30s
	s.waitFor("player-1", ep.Name, "playback-state", is("paused"), "paused on the player")
}
