//go:build linux

package node_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeRecordingServiceReportsItsWork (#1363): the shipped
// recording-service-api driver on the simulated recording service's https
// face, its key in a header, lands the service's status and version, its
// three work counters as samples, and a failed recording as an event on the
// component.
func TestNodeRecordingServiceReportsItsWork(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var rec string
	s := newHTTPStack(t, "edge-rec", nil, func(store *profile.Store) []string {
		rec = driverProfile(t, store, "recording-service-api")
		return []string{rec}
	})
	s.secret("rec-key", "api-key", map[string]string{"key": "recording-lab-key"})
	s.component("recorder-1")
	dev := s.devices[rec]
	ep := s.attachHTTP("recorder-1", "recording-service-api", "edge-rec", map[string]string{
		"host": s.ips[rec], "port": fmt.Sprint(s.httpPort(rec)), "key": "rec-key"})
	s.runNode("edge-rec")

	s.waitFor("recorder-1", ep.Name, "endpoint-authenticated", is("yes"), "yes: the key accepted")
	s.waitFor("recorder-1", ep.Name, "service-state", is("ok"), "ok at rest")
	s.waitFor("recorder-1", ep.Name, "software-version", is(fmt.Sprint(dev.ReadPath("configuration.software.version"))), "the version")
	for _, name := range []string{"active-recording-count", "queue-depth"} {
		deadline := time.Now().Add(20 * time.Second)
		for {
			if _, ok := s.metric("recorder-1", ep.Name, name); ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the service's poll landed no %s sample", name)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if u, ok := s.metric("recorder-1", ep.Name, "storage-utilization"); ok && u > 0 && u < 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the service's poll landed no storage-utilization, a ratio")
		}
		time.Sleep(50 * time.Millisecond)
	}

	waitFor(t, "the stream up", 20*time.Second, func() bool {
		v, err := s.gw.LatestProperty(s.ctx, "recorder-1", "session-state", "https")
		return err == nil && v != nil && v.Value == "up"
	})
	if res := dev.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/recordings/fail", Body: `{"id":"r-9001","reason":"disk full"}`}); res.Status != 200 {
		t.Fatalf("fail a recording: %+v", res)
	}
	waitFor(t, "recording-failed", 5*time.Second, func() bool { return len(eventsOf(t, s.gw, "recorder-1", "recording-failed")) == 1 })
	failed := eventsOf(t, s.gw, "recorder-1", "recording-failed")[0]
	if p := payloadOf(t, failed); failed.Message != "Recording failed" || p["recording_id"] != "r-9001" || p["reason"] != "disk full" {
		t.Fatalf("recording-failed = %+v %s, want the recording id and the reason", failed, failed.Attributes)
	}
}
