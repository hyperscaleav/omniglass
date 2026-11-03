//go:build linux

package sim

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Kestrel Relay C1 codec and Gaze P2 PTZ camera faces (#897): each its
// own profile (neither is a video bar), a JSON API over https behind the
// same basic auth the VRoom demands. The codec reports its call, the content
// it is sharing and the peripherals plugged into it; the camera its pan,
// tilt, zoom, the preset it sits on and whether it tracks.

func startKestrel(t *testing.T, name string) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: name + "-1", Hostname: name + "-1", LoopbackIP: ip, ProfileName: name, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := deviceByProfile(t, e, name)
	d.StopBehaviors() // hold the readings still between reads
	return d
}

// kestrelDo calls the https face as the lab's login does; pass user "" for no
// credentials at all.
func kestrelDo(t *testing.T, d *Device, user, method, path, body string) (int, map[string]any) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // the sim's self-signed identity
	req, _ := http.NewRequest(method, "https://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.SetBasicAuth(user, "kestrel")
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

// TestKestrelRelayServesCallContentAndPeripherals: the codec reports its
// identity and uptime, an idle call, nothing shared and its peripherals; a
// call placed and content shared over the API read back; a request with no
// login is refused.
func TestKestrelRelayServesCallContentAndPeripherals(t *testing.T) {
	d := startKestrel(t, "kestrel-relay-c1")
	code, sys := kestrelDo(t, d, "admin", http.MethodGet, "/api/system", "")
	if code != http.StatusOK || sys["model"] != "Relay C1" || sys["serial"] == "" || sys["firmware"] == "" {
		t.Fatalf("GET /api/system = %d %v", code, sys)
	}
	if _, ok := sys["uptime"].(float64); !ok {
		t.Errorf("uptime = %v, want a number", sys["uptime"])
	}
	if code, c := kestrelDo(t, d, "admin", http.MethodGet, "/api/call", ""); code != http.StatusOK || c["active"] != false || c["participants"] != float64(0) {
		t.Errorf("GET /api/call = %d %v, want idle", code, c)
	}
	if code, c := kestrelDo(t, d, "admin", http.MethodGet, "/api/content", ""); code != http.StatusOK || c["sharing"] != false {
		t.Errorf("GET /api/content = %d %v, want nothing shared", code, c)
	}
	if code, p := kestrelDo(t, d, "admin", http.MethodGet, "/api/peripherals", ""); code != http.StatusOK || p["connected"].(float64) < 1 {
		t.Errorf("GET /api/peripherals = %d %v, want some connected", code, p)
	}
	if code, _ := kestrelDo(t, d, "", http.MethodGet, "/api/call", ""); code != http.StatusUnauthorized {
		t.Errorf("GET /api/call with no login: %d, want 401", code)
	}

	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/call/dial", `{"participants": 4}`); code != http.StatusOK {
		t.Fatalf("dial: %d", code)
	}
	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/content/share", `{"source": "hdmi1"}`); code != http.StatusOK {
		t.Fatalf("share: %d", code)
	}
	if _, c := kestrelDo(t, d, "admin", http.MethodGet, "/api/call", ""); c["active"] != true || c["participants"] != float64(4) {
		t.Errorf("after dial, call = %v", c)
	}
	if _, c := kestrelDo(t, d, "admin", http.MethodGet, "/api/content", ""); c["sharing"] != true || c["source"] != "hdmi1" {
		t.Errorf("after share, content = %v", c)
	}
	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/call/disconnect", `{}`); code != http.StatusOK {
		t.Fatalf("disconnect: %d", code)
	}
	if _, c := kestrelDo(t, d, "admin", http.MethodGet, "/api/call", ""); c["active"] != false || c["participants"] != float64(0) {
		t.Errorf("after disconnect, call = %v", c)
	}
}

// TestKestrelGazeServesPanTiltZoomAndPresets: the camera reports its
// position and tracking; a preset recalled moves it to the preset's
// position and takes it off tracking; a position set by hand clears the
// preset; a tracking state outside the enum is refused.
func TestKestrelGazeServesPanTiltZoomAndPresets(t *testing.T) {
	d := startKestrel(t, "kestrel-gaze-p2")
	code, sys := kestrelDo(t, d, "admin", http.MethodGet, "/api/system", "")
	if code != http.StatusOK || sys["model"] != "Gaze P2" || sys["serial"] == "" {
		t.Fatalf("GET /api/system = %d %v", code, sys)
	}
	code, cam := kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/camera: %d", code)
	}
	for _, k := range []string{"pan", "tilt", "magnification"} {
		if _, ok := cam[k].(float64); !ok {
			t.Errorf("%s = %v, want a number", k, cam[k])
		}
	}
	if cam["tracking"] != "tracking" || cam["preset"] != float64(0) {
		t.Errorf("at rest the camera = %v, want tracking and on no preset", cam)
	}
	if code, _ := kestrelDo(t, d, "", http.MethodGet, "/api/camera", ""); code != http.StatusUnauthorized {
		t.Errorf("GET /api/camera with no login: %d, want 401", code)
	}

	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/camera/preset", `{"preset": 2}`); code != http.StatusOK {
		t.Fatalf("recall preset: %d", code)
	}
	_, cam = kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", "")
	if cam["preset"] != float64(2) || cam["tracking"] != "manual" || cam["pan"] != float64(30) || cam["magnification"] != float64(2.5) {
		t.Errorf("after preset 2 the camera = %v, want preset 2 at pan 30, magnification 2.5, manual", cam)
	}
	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/camera/position", `{"pan": -12.5, "tilt": 4, "magnification": 1.5}`); code != http.StatusOK {
		t.Fatalf("set position: %d", code)
	}
	_, cam = kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", "")
	if cam["pan"] != -12.5 || cam["tilt"] != float64(4) || cam["magnification"] != 1.5 || cam["preset"] != float64(0) {
		t.Errorf("after a position set the camera = %v, want it there on no preset", cam)
	}
	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/camera/tracking", `{"tracking": "wandering"}`); code < 400 {
		t.Errorf("tracking %q: %d, want a refusal", "wandering", code)
	}
	if code, _ := kestrelDo(t, d, "admin", http.MethodPost, "/api/camera/tracking", `{"tracking": "tracking"}`); code != http.StatusOK {
		t.Fatalf("tracking on: %d", code)
	}
	if _, cam = kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", ""); cam["tracking"] != "tracking" {
		t.Errorf("tracking = %v, want tracking", cam["tracking"])
	}
}

// TestKestrelGazeTrackingDriftsWithinBounds: while tracking, a tick nudges
// pan and tilt and they stay within the camera's range; set to manual, a
// tick holds the position.
func TestKestrelGazeTrackingDriftsWithinBounds(t *testing.T) {
	d := startKestrel(t, "kestrel-gaze-p2")
	var track *behaviorEntry
	d.behaviorMu.Lock()
	for i := range d.behaviors {
		if d.behaviors[i].Name == "tracking_drift" {
			track = &d.behaviors[i]
		}
	}
	d.behaviorMu.Unlock()
	if track == nil {
		t.Fatalf("no tracking_drift behavior; have %v", d.Behaviors())
	}
	moved := false
	_, start := kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", "")
	for i := 0; i < 40; i++ {
		d.tickBehavior(track)
		_, cam := kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", "")
		pan, tilt := cam["pan"].(float64), cam["tilt"].(float64)
		if pan < -60 || pan > 60 || tilt < -20 || tilt > 20 {
			t.Fatalf("tick %d left the range: pan %v tilt %v", i, pan, tilt)
		}
		moved = moved || pan != start["pan"] || tilt != start["tilt"]
	}
	if !moved {
		t.Error("forty ticks while tracking never moved the camera")
	}
	kestrelDo(t, d, "admin", http.MethodPost, "/api/camera/tracking", `{"tracking": "manual"}`)
	_, before := kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", "")
	for i := 0; i < 10; i++ {
		d.tickBehavior(track)
	}
	if _, after := kestrelDo(t, d, "admin", http.MethodGet, "/api/camera", ""); after["pan"] != before["pan"] || after["tilt"] != before["tilt"] {
		t.Errorf("manual, ticks moved the camera from %v to %v", before, after)
	}
}
