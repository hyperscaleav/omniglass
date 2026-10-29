//go:build linux

package sim

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Kestrel codec and PTZ camera (#897), each its own profile over the
// Kestrel API's https face behind basic auth: the codec answers the routes
// the bar's driver reads and adds content sharing and peripherals; the
// camera answers its presets, tracking and position.

func startKestrel(t *testing.T, name string) *Device {
	t.Helper()
	e := newCatalogEngine(t)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: name + "-1", Hostname: name + "-1", LoopbackIP: simtest.ClaimLoopback(t),
		ProfileName: name, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := deviceByProfile(t, e, name)
	d.StopBehaviors()
	return d
}

func kestrelJSON(t *testing.T, d *Device, method, path, body string) (int, map[string]any) {
	t.Helper()
	res := d.Operate(Operation{Kind: "http", Method: method, Path: path, Body: body})
	var out map[string]any
	_ = json.Unmarshal([]byte(res.Body), &out)
	return res.Status, out
}

// kestrelHTTPS sends one request to the real https face, with or without the
// family's basic login.
func kestrelHTTPS(t *testing.T, d *Device, path string, login bool) int {
	t.Helper()
	// Trust exactly the device's own self-signed certificate.
	pool := x509.NewCertPool()
	pool.AddCert(d.HTTPSCertificate())
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: d.HTTPSCertificate().DNSNames[0]}}}
	req, _ := http.NewRequest(http.MethodGet, "https://"+faceAddr(t, d, "http-api")+path, nil)
	if login {
		req.SetBasicAuth("admin", "kestrel")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestRelayCodecAnswersTheBarsRoutesAndItsOwn: the codec serves the routes
// kestrel-api reads (system, call, audio) behind basic auth over https, and
// its own: content sharing (set and read back, an off-enum value refused,
// cleared by a disconnect) and its peripherals.
func TestRelayCodecAnswersTheBarsRoutesAndItsOwn(t *testing.T) {
	d := startKestrel(t, "kestrel-relay-c1")
	if code := kestrelHTTPS(t, d, "/api/system", false); code != http.StatusUnauthorized {
		t.Errorf("https /api/system with no login: %d, want 401", code)
	}
	for _, p := range []string{"/api/system", "/api/call", "/api/audio", "/api/content", "/api/peripherals"} {
		if code := kestrelHTTPS(t, d, p, true); code != http.StatusOK {
			t.Errorf("https %s: %d", p, code)
		}
	}
	if _, sys := kestrelJSON(t, d, "GET", "/api/system", ""); sys["model"] != "Relay C1" || sys["power"] != "on" {
		t.Errorf("system = %v", sys)
	}
	if _, p := kestrelJSON(t, d, "GET", "/api/peripherals", ""); len(p["peripherals"].([]any)) != 4 {
		t.Errorf("peripherals = %v, want four", p)
	}

	kestrelJSON(t, d, "POST", "/api/call/dial", `{"id": "c1", "participants": 4}`)
	if code, c := kestrelJSON(t, d, "POST", "/api/content", `{"sharing": true}`); code != 200 || c["sharing"] != true {
		t.Fatalf("share = %d %v", code, c)
	}
	if _, call := kestrelJSON(t, d, "GET", "/api/call", ""); call["active"] != true || call["participants"] != float64(4) {
		t.Errorf("call = %v", call)
	}
	if code, _ := kestrelJSON(t, d, "POST", "/api/content", `{"sharing": "maybe"}`); code != 400 {
		t.Errorf("sharing %q: %d, want 400", "maybe", code)
	}
	kestrelJSON(t, d, "POST", "/api/call/disconnect", `{}`)
	if _, c := kestrelJSON(t, d, "GET", "/api/content", ""); c["sharing"] != false {
		t.Errorf("content after hanging up = %v, want not sharing", c)
	}
}

// TestGazeCameraPresetsAndTracking: the camera recalls a preset (which stops
// tracking), turns tracking on, refuses a preset it does not have without
// moving, and while tracking its pan follows a presenter within the sweep.
func TestGazeCameraPresetsAndTracking(t *testing.T) {
	d := startKestrel(t, "kestrel-gaze-p2")
	if code := kestrelHTTPS(t, d, "/api/camera", false); code != http.StatusUnauthorized {
		t.Errorf("https /api/camera with no login: %d, want 401", code)
	}
	if _, c := kestrelJSON(t, d, "GET", "/api/camera", ""); c["preset"] != "wide" || c["tracking"] != false {
		t.Errorf("camera at rest = %v", c)
	}
	if code, c := kestrelJSON(t, d, "POST", "/api/camera/tracking", `{"tracking": true}`); code != 200 || c["tracking"] != true {
		t.Fatalf("tracking on = %d %v", code, c)
	}
	d.behaviorMu.Lock()
	var follow *behaviorEntry
	for i := range d.behaviors {
		if d.behaviors[i].Name == "presenter_tracking" {
			follow = &d.behaviors[i]
		}
	}
	d.behaviorMu.Unlock()
	if follow == nil {
		t.Fatalf("no presenter_tracking; have %v", d.Behaviors())
	}
	moved := false
	for i := 0; i < 20; i++ {
		d.tickBehavior(follow)
		p, _, _ := toFloat(d.ReadPath("status.camera.pan"))
		if p < -30 || p > 30 {
			t.Fatalf("pan %v outside the sweep", p)
		}
		moved = moved || p != 0
	}
	if !moved {
		t.Error("tracking never moved the camera")
	}

	if code, c := kestrelJSON(t, d, "POST", "/api/camera/preset", `{"preset": "lectern"}`); code != 200 || c["preset"] != "lectern" || c["tracking"] != false {
		t.Fatalf("recall lectern = %d %v, want lectern with tracking off", code, c)
	}
	held, _, _ := toFloat(d.ReadPath("status.camera.pan"))
	d.tickBehavior(follow)
	if p, _, _ := toFloat(d.ReadPath("status.camera.pan")); p != held {
		t.Errorf("tracking off, the pan moved %v to %v", held, p)
	}
	kestrelJSON(t, d, "POST", "/api/camera/tracking", `{"tracking": true}`)
	if code, _ := kestrelJSON(t, d, "POST", "/api/camera/preset", `{"preset": "balcony"}`); code != 400 {
		t.Errorf("preset %q: %d, want 400", "balcony", code)
	}
	if _, c := kestrelJSON(t, d, "GET", "/api/camera", ""); c["preset"] != "lectern" || c["tracking"] != true {
		t.Errorf("after a refused preset = %v, want lectern, still tracking", c)
	}
}
