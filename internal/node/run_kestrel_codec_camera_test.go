//go:build linux

package node_test

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeKestrelCodecAndCamera (#897): the shipped kestrel-api driver reads
// the simulated Relay codec as it reads the bar (its call, audio and power
// over https behind basic auth, the key pinned on first use), and the new
// kestrel-camera-api driver reads the Gaze camera's preset and tracking. A
// call placed on the codec and a preset recalled on the camera read back.
func TestNodeKestrelCodecAndCamera(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	const codec, camera = "kestrel-relay-c1", "kestrel-gaze-p2"
	s := newHTTPStack(t, "edge-kestrel", nil, func(store *profile.Store) []string {
		if got := driverProfile(t, store, "kestrel-camera-api"); got != camera {
			t.Fatalf("kestrel-camera-api reads %s, want the camera", got)
		}
		return []string{codec, camera}
	})
	s.secret("kestrel-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	s.component("codec-1")
	s.component("camera-1")
	cep := s.attachHTTP("codec-1", "kestrel-api", "edge-kestrel", map[string]string{
		"host": s.ips[codec], "port": fmt.Sprint(s.httpPort(codec)), "login": "kestrel-login"})
	kep := s.attachHTTP("camera-1", "kestrel-camera-api", "edge-kestrel", map[string]string{
		"host": s.ips[camera], "port": fmt.Sprint(s.httpPort(camera)), "login": "kestrel-login"})
	s.runNode("edge-kestrel")

	s.waitFor("codec-1", cep.Name, "model-number", is("Relay C1"), "the codec's model")
	s.waitFor("codec-1", cep.Name, "call-state", is("idle"), "idle at rest")
	s.waitFor("codec-1", cep.Name, "power-state", is("on"), "on")
	s.waitFor("camera-1", kep.Name, "model-number", is("Gaze P2"), "the camera's model")
	s.waitFor("camera-1", kep.Name, "camera-preset", is("wide"), "the wide preset at rest")
	s.waitFor("camera-1", kep.Name, "tracking-state", is("off"), "not tracking")

	if res := s.devices[codec].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id": "c42", "participants": 5}`}); res.Status != 200 {
		t.Fatalf("dial: %+v", res)
	}
	if res := s.devices[camera].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/camera/preset", Body: `{"preset": "lectern"}`}); res.Status != 200 {
		t.Fatalf("preset: %+v", res)
	}
	// The codec's call arrives on its live feed; the camera's on its next poll.
	s.waitFor("codec-1", cep.Name, "call-state", is("active"), "the call, pushed on the feed")
	s.runNode("edge-kestrel")
	s.waitFor("camera-1", kep.Name, "camera-preset", is("lectern"), "the lectern, recalled over HTTP")
}
