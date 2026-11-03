//go:build linux

package node_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeKestrelRelayAndGazeReadTheirClass (#897): the shipped
// kestrel-relay-api and kestrel-gaze-api drivers on the simulated codec's
// and camera's https faces, behind the Kestrel login, land the codec's call
// state, shared content and peripherals and the camera's pan, tilt, zoom,
// preset and tracking, beside their identity. A call placed on the codec and
// a preset recalled on the camera read back on the next poll.
func TestNodeKestrelRelayAndGazeReadTheirClass(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var relay, gaze string
	s := newHTTPStack(t, "edge-kestrel", nil, func(store *profile.Store) []string {
		relay, gaze = driverProfile(t, store, "kestrel-relay-api"), driverProfile(t, store, "kestrel-gaze-api")
		return []string{relay, gaze}
	})
	s.secret("kestrel-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	attach := func(comp, prof, driver string) string {
		s.component(comp)
		return s.attachHTTP(comp, driver, "edge-kestrel", map[string]string{
			"host": s.ips[prof], "port": fmt.Sprint(s.httpPort(prof)), "login": "kestrel-login"}).Name
	}
	relayEP, gazeEP := attach("codec-1", relay, "kestrel-relay-api"), attach("camera-1", gaze, "kestrel-gaze-api")
	s.runNode("edge-kestrel")

	metric := func(comp, ep, key string, ok func(float64) bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			if v, has := s.metric(comp, ep, key); has && ok(v) {
				return
			}
			if time.Now().After(deadline) {
				v, has := s.metric(comp, ep, key)
				t.Fatalf("%s on %s = %v (%v)", key, comp, v, has)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for _, d := range []struct{ comp, ep, prof, model string }{
		{"codec-1", relayEP, relay, "Relay C1"}, {"camera-1", gazeEP, gaze, "Gaze P2"},
	} {
		s.waitFor(d.comp, d.ep, "endpoint-authenticated", is("yes"), "yes: the login accepted")
		s.waitFor(d.comp, d.ep, "model-number", is(d.model), "the model")
		s.waitFor(d.comp, d.ep, "serial-number", is(fmt.Sprint(s.devices[d.prof].ReadPath("configuration.device.serial"))), "the serial")
		s.waitFor(d.comp, d.ep, "firmware-version", is(fmt.Sprint(s.devices[d.prof].ReadPath("configuration.firmware.version"))), "the firmware")
		metric(d.comp, d.ep, "uptime", func(v float64) bool { return v >= 0 })
	}

	// The codec: idle at rest, nothing shared, its peripherals counted.
	s.waitFor("codec-1", relayEP, "call-state", is("idle"), "idle at rest")
	s.waitFor("codec-1", relayEP, "content-sharing-state", is("idle"), "nothing shared at rest")
	metric("codec-1", relayEP, "participant-count", func(v float64) bool { return v == 0 })
	metric("codec-1", relayEP, "peripheral-count", func(v float64) bool { return v >= 1 })

	// The camera: where it points, and that it tracks.
	s.waitFor("camera-1", gazeEP, "tracking-state", is("tracking"), "tracking at rest")
	metric("camera-1", gazeEP, "pan-angle", func(v float64) bool { return v >= -60 && v <= 60 })
	metric("camera-1", gazeEP, "tilt-angle", func(v float64) bool { return v >= -20 && v <= 20 })
	metric("camera-1", gazeEP, "zoom-level", func(v float64) bool { return v >= 1 })
	metric("camera-1", gazeEP, "camera-preset", func(v float64) bool { return v == 0 })
	if _, has := s.metric("camera-1", gazeEP, "call-state"); has {
		t.Error("the camera landed a call state")
	}
	if _, has := s.metric("codec-1", relayEP, "pan-angle"); has {
		t.Error("the codec landed a pan angle")
	}

	for _, op := range []struct {
		prof string
		sim.Operation
	}{
		{relay, sim.Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"participants": 5}`}},
		{relay, sim.Operation{Kind: "http", Method: "POST", Path: "/api/content/share", Body: `{"source": "usbc"}`}},
		{gaze, sim.Operation{Kind: "http", Method: "POST", Path: "/api/camera/preset", Body: `{"preset": 2}`}},
	} {
		if res := s.devices[op.prof].Operate(op.Operation); res.Status != 200 {
			t.Fatalf("%s: %+v", op.Path, res)
		}
	}
	s.runNode("edge-kestrel") // a restarted node polls at once, rather than in 30s
	s.waitFor("codec-1", relayEP, "call-state", is("active"), "in a call on the codec")
	s.waitFor("codec-1", relayEP, "content-sharing-state", is("sharing"), "sharing on the codec")
	metric("codec-1", relayEP, "participant-count", func(v float64) bool { return v == 5 })
	s.waitFor("camera-1", gazeEP, "tracking-state", is("manual"), "off tracking after a preset")
	metric("camera-1", gazeEP, "camera-preset", func(v float64) bool { return v == 2 })
	metric("camera-1", gazeEP, "pan-angle", func(v float64) bool { return v == 30 })
	metric("camera-1", gazeEP, "zoom-level", func(v float64) bool { return v == 2.5 })
}
