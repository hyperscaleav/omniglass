//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Boreal projector's faces (#899): its state over SNMP on the Boreal
// arc, the same state over its web admin's JSON behind HTTP Digest, and
// power, the shutter and the input set over HTTP, from one live state.

const (
	beamOIDPower   = "1.3.6.1.4.1.55520.1.1.1.0"
	beamOIDHours   = "1.3.6.1.4.1.55520.6.1.0"
	beamOIDShutter = "1.3.6.1.4.1.55520.6.2.0"
	beamOIDInput   = "1.3.6.1.4.1.55520.6.3.0"
	beamOIDTemp    = "1.3.6.1.4.1.55520.6.4.0"
)

var beamNC int

func startBeam(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "projector-1", Hostname: "projector-1", LoopbackIP: ip, ProfileName: "boreal-beam-340", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return deviceByProfile(t, e, "boreal-beam-340")
}

// beamDo sends one request to the web admin, answering its digest
// challenge; with no credentials it stops at the challenge.
func beamDo(t *testing.T, d *Device, method, path, body string, login bool) *http.Response {
	t.Helper()
	send := func(auth string) *http.Response {
		req, _ := http.NewRequest(method, "http://"+faceAddr(t, d, "web-admin")+path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
	first := send("")
	if !login {
		return first
	}
	first.Body.Close()
	var challenge string
	for _, v := range first.Header.Values("WWW-Authenticate") {
		if strings.HasPrefix(v, "Digest ") {
			challenge = v
		}
	}
	if challenge == "" {
		t.Fatalf("%s %s: no digest challenge (status %d)", method, path, first.StatusCode)
	}
	// The web admin holds one nonce per client and refuses a count it has
	// seen, so each answer counts up, as a browser's do.
	beamNC++
	return send(digestAnswer(challenge, method, path, "admin", "boreal", fmt.Sprintf("%08x", beamNC)))
}

func beamJSON(t *testing.T, d *Device, method, path, body string) map[string]any {
	t.Helper()
	resp := beamDo(t, d, method, path, body, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status %d", method, path, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return out
}

func beamSNMP(t *testing.T, d *Device, oids ...string) map[string]string {
	t.Helper()
	g := &gosnmp.GoSNMP{
		Target: d.LoopbackIP.String(), Port: uint16(facePort(t, d, "snmp")), Community: "public",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1,
	}
	if err := g.Connect(); err != nil {
		t.Fatalf("SNMP connect: %v", err)
	}
	defer g.Conn.Close()
	pkt, err := g.Get(oids)
	if err != nil {
		t.Fatalf("SNMP GET %v: %v", oids, err)
	}
	out := map[string]string{}
	for _, v := range pkt.Variables {
		if b, ok := v.Value.([]byte); ok {
			out[v.Name[1:]] = string(b)
		} else {
			out[v.Name[1:]] = fmt.Sprint(gosnmp.ToBigInt(v.Value))
		}
	}
	return out
}

// TestBorealBeamServesItsStateOverSNMPAndHTTP: the SNMP agent answers the
// state the web admin's JSON reports (power, light-source hours, shutter,
// input, temperature), and the web admin challenges a request with no
// credentials.
func TestBorealBeamServesItsStateOverSNMPAndHTTP(t *testing.T) {
	d := startBeam(t)
	d.StopBehaviors() // hold the readings still between the two reads
	s := beamJSON(t, d, http.MethodGet, "/api/status", "")
	dev, proj := s["device"].(map[string]any), s["projector"].(map[string]any)
	if dev["model"] != "Beam 340" || dev["power"] != "on" {
		t.Errorf("device = %v, want a Beam 340, on", dev)
	}
	got := beamSNMP(t, d, beamOIDPower, beamOIDHours, beamOIDShutter, beamOIDInput, beamOIDTemp)
	for oid, want := range map[string]string{
		beamOIDPower:   fmt.Sprint(dev["power"]),
		beamOIDHours:   fmt.Sprint(proj["light_source_hours"]),
		beamOIDShutter: fmt.Sprint(proj["shutter"]),
		beamOIDInput:   fmt.Sprint(proj["input"]),
		beamOIDTemp:    fmt.Sprint(int64(proj["temperature"].(float64))),
	} {
		if got[oid] != want {
			t.Errorf("SNMP %s = %q, HTTP %q", oid, got[oid], want)
		}
	}
	resp := beamDo(t, d, http.MethodGet, "/api/status", "", false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/status with no credentials: status %d, want 401", resp.StatusCode)
	}
}

// TestBorealBeamControlOverHTTP: standby, the shutter and the input set
// over the web admin read back over SNMP; a value outside an enum is
// refused and changes nothing.
func TestBorealBeamControlOverHTTP(t *testing.T) {
	d := startBeam(t)
	if ack := beamJSON(t, d, http.MethodPost, "/api/projector/shutter", `{"shutter": "closed"}`); ack["shutter"] != "closed" {
		t.Fatalf("shutter ack = %v", ack)
	}
	if ack := beamJSON(t, d, http.MethodPost, "/api/projector/input", `{"input": "hdmi2"}`); ack["input"] != "hdmi2" {
		t.Fatalf("input ack = %v", ack)
	}
	if ack := beamJSON(t, d, http.MethodPost, "/api/projector/power", `{"power": "standby"}`); ack["power"] != "standby" {
		t.Fatalf("power ack = %v", ack)
	}
	got := beamSNMP(t, d, beamOIDPower, beamOIDShutter, beamOIDInput)
	if got[beamOIDPower] != "standby" || got[beamOIDShutter] != "closed" || got[beamOIDInput] != "hdmi2" {
		t.Errorf("SNMP after the writes = %v, want standby, closed, hdmi2", got)
	}

	resp := beamDo(t, d, http.MethodPost, "/api/projector/shutter", `{"shutter": "ajar"}`, true)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("shutter %q: status %d, want a refusal", "ajar", resp.StatusCode)
	}
	if v := beamSNMP(t, d, beamOIDShutter)[beamOIDShutter]; v != "closed" {
		t.Errorf("SNMP shutter after a refused write = %q, want closed", v)
	}
}

// TestBorealBeamLightSourceAgesWhileLit: the light source gains hours on
// each tick while the projector is on, and none in standby, the behavior
// a scenario runs faster to walk a source toward its service threshold.
func TestBorealBeamLightSourceAgesWhileLit(t *testing.T) {
	d := startBeam(t)
	d.StopBehaviors()
	var aging *behaviorEntry
	d.behaviorMu.Lock()
	for i := range d.behaviors {
		if d.behaviors[i].Name == "light_source_aging" {
			aging = &d.behaviors[i]
		}
	}
	d.behaviorMu.Unlock()
	if aging == nil {
		t.Fatalf("no light_source_aging behavior; have %v", d.Behaviors())
	}
	hours := func() float64 {
		f, _, err := toFloat(d.ReadPath("status.projector.light_source_hours"))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	start := hours()
	for i := 0; i < 3; i++ {
		d.tickBehavior(aging)
	}
	if lit := hours(); lit <= start || lit > start+0.06 {
		t.Fatalf("hours after three lit minutes = %v from %v, want about 0.05 more", lit, start)
	}
	beamJSON(t, d, http.MethodPost, "/api/projector/power", `{"power": "standby"}`)
	before := hours()
	d.tickBehavior(aging)
	if after := hours(); after != before {
		t.Errorf("hours in standby went %v to %v, want unchanged", before, after)
	}
}
