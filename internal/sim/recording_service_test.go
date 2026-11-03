//go:build linux

package sim

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The recording service's face (#1363): a software service's HTTPS JSON API
// behind an api key, reporting its health and version, the work it carries
// (recordings in progress, storage used, queue depth), and a recording that
// fails.

const recordingKey = "recording-lab-key"

func startRecordingService(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "rec-1", Hostname: "rec-1", LoopbackIP: ip, ProfileName: "recording-service", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return deviceByProfile(t, e, "recording-service")
}

func recordingDo(t *testing.T, d *Device, method, path, body, key string) (int, map[string]any) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, _ := http.NewRequest(method, "https://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("X-Recording-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestRecordingServiceServesHealthAndWork: the health route reports a status
// and a version, the work route the three counters; a request without the
// key is refused.
func TestRecordingServiceServesHealthAndWork(t *testing.T) {
	d := startRecordingService(t)
	code, h := recordingDo(t, d, http.MethodGet, "/api/health", "", recordingKey)
	if code != http.StatusOK || h["status"] != "ok" || h["version"] == "" || h["version"] == nil {
		t.Fatalf("GET /api/health = %d %v, want ok with a version", code, h)
	}
	code, w := recordingDo(t, d, http.MethodGet, "/api/work", "", recordingKey)
	if code != http.StatusOK {
		t.Fatalf("GET /api/work: %d", code)
	}
	if n, ok := w["recordings_in_progress"].(float64); !ok || n < 0 {
		t.Errorf("recordings_in_progress = %v, want a count", w["recordings_in_progress"])
	}
	if u, ok := w["storage_used_percent"].(float64); !ok || u <= 0 || u >= 100 {
		t.Errorf("storage_used_percent = %v, want a used percentage", w["storage_used_percent"])
	}
	if q, ok := w["queue_depth"].(float64); !ok || q < 0 {
		t.Errorf("queue_depth = %v, want a count", w["queue_depth"])
	}
	if code, _ := recordingDo(t, d, http.MethodGet, "/api/health", "", ""); code != http.StatusUnauthorized {
		t.Errorf("GET /api/health with no key: %d, want 401", code)
	}
}

// TestRecordingServiceFailureIsSimulatedByARoute: failing a recording records
// it as the last failure (and names it on the live feed); a refused id
// changes nothing.
func TestRecordingServiceFailureIsSimulatedByARoute(t *testing.T) {
	d := startRecordingService(t)
	if code, _ := recordingDo(t, d, http.MethodPost, "/api/recordings/fail", `{"id": "r-9001", "reason": "disk full"}`, recordingKey); code != http.StatusOK {
		t.Fatalf("fail a recording: %d", code)
	}
	lastFailure := func() map[string]any {
		_, w := recordingDo(t, d, http.MethodGet, "/api/work", "", recordingKey)
		lf, _ := w["last_failure"].(map[string]any)
		return lf
	}
	if lf := lastFailure(); lf["id"] != "r-9001" || lf["reason"] != "disk full" {
		t.Errorf("last_failure = %v, want r-9001 for disk full", lf)
	}
	if code, _ := recordingDo(t, d, http.MethodPost, "/api/recordings/fail", `{"id": "bad id!", "reason": "x"}`, recordingKey); code < 400 {
		t.Errorf("a malformed id: %d, want a refusal", code)
	}
	if lf := lastFailure(); lf["id"] != "r-9001" {
		t.Errorf("after a refused write last_failure = %v, want r-9001 unchanged", lf)
	}
}
