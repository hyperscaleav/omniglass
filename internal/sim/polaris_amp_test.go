//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Polaris amplifier's faces (#898): its operating state over SNMP, the
// same state over its JSON API, and standby and the protect reset over HTTP,
// all from one live state, served the way the lab serves it.

const ampToken = "polaris-lab-token"

func startAmp(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "amp-1", Hostname: "amp-1", LoopbackIP: ip, ProfileName: "polaris-amp-4100", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return deviceByProfile(t, e, "polaris-amp-4100")
}

func ampSNMP(t *testing.T, d *Device, oids ...string) []gosnmp.SnmpPDU {
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
	return pkt.Variables
}

// snmpText renders a varbind as the text a collector reads: an octet string
// as its bytes, a number as its decimal.
func snmpText(v gosnmp.SnmpPDU) string {
	if b, ok := v.Value.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(gosnmp.ToBigInt(v.Value))
}

func ampPost(t *testing.T, d *Device, path, body string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polaris-Token", ampToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status %d", path, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("POST %s: decoding the ack: %v", path, err)
	}
	return out
}

type ampStatus struct {
	Device struct {
		Model    string  `json:"model"`
		Serial   string  `json:"serial"`
		Firmware string  `json:"firmware"`
		Uptime   float64 `json:"uptime"`
	} `json:"device"`
	Amp struct {
		Power       string  `json:"power"`
		Protect     string  `json:"protect"`
		Temperature float64 `json:"temperature"`
		FanRPM      float64 `json:"fan_rpm"`
	} `json:"amp"`
	Channels []struct {
		Index    int     `json:"index"`
		Name     string  `json:"name"`
		OutputDB float64 `json:"output_db"`
		Clip     bool    `json:"clip"`
		LoadOhms float64 `json:"load_ohms"`
	} `json:"channels"`
}

func ampGetStatus(t *testing.T, d *Device) ampStatus {
	t.Helper()
	resp, err := authedGet("http://"+faceAddr(t, d, "http-api")+"/api/status", ampToken)
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status: status %d", resp.StatusCode)
	}
	var s ampStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decoding /api/status: %v", err)
	}
	return s
}

const (
	ampOIDPower   = "1.3.6.1.4.1.55540.3.1.0"
	ampOIDProtect = "1.3.6.1.4.1.55540.3.2.0"
	ampOIDTemp    = "1.3.6.1.4.1.55540.3.3.0"
	ampOIDFan     = "1.3.6.1.4.1.55540.3.4.0"
)

// ampChannelOID is a column of the amplifier's channel table for channel n
// (1 to 4): 2 the output level, 3 the clip flag, 4 the load.
func ampChannelOID(column, n int) string {
	return fmt.Sprintf("1.3.6.1.4.1.55540.3.10.1.%d.%d", column, n)
}

// TestPolarisAmpServesItsStateOverSNMPAndHTTP: the amplifier's SNMP agent
// answers the operating state its JSON API reports (power, protect,
// temperature, fan, and every channel's output level, clip and load), and
// an access refused without the API token.
func TestPolarisAmpServesItsStateOverSNMPAndHTTP(t *testing.T) {
	d := startAmp(t)
	d.StopBehaviors() // hold the readings still between the two reads
	s := ampGetStatus(t, d)
	if s.Device.Model != "Amp 4-100" {
		t.Errorf("model = %q, want Amp 4-100", s.Device.Model)
	}
	if s.Amp.Power != "on" || s.Amp.Protect != "normal" {
		t.Errorf("power/protect = %q/%q, want on/normal at rest", s.Amp.Power, s.Amp.Protect)
	}
	if len(s.Channels) != 4 {
		t.Fatalf("channels = %d, want the four of a 4-100", len(s.Channels))
	}

	oids := []string{ampOIDPower, ampOIDProtect, ampOIDTemp, ampOIDFan}
	for n := 1; n <= 4; n++ {
		oids = append(oids, ampChannelOID(2, n), ampChannelOID(3, n), ampChannelOID(4, n))
	}
	vars := ampSNMP(t, d, oids...)
	got := map[string]string{}
	for _, v := range vars {
		got[v.Name[1:]] = snmpText(v) // gosnmp names carry a leading dot
	}
	if got[ampOIDPower] != s.Amp.Power || got[ampOIDProtect] != s.Amp.Protect {
		t.Errorf("SNMP power/protect = %q/%q, HTTP %q/%q", got[ampOIDPower], got[ampOIDProtect], s.Amp.Power, s.Amp.Protect)
	}
	// SNMP carries whole degrees and rpm, the shape an enterprise MIB gives.
	if want := fmt.Sprint(int64(s.Amp.Temperature)); got[ampOIDTemp] != want {
		t.Errorf("SNMP temperature = %q, HTTP %v", got[ampOIDTemp], s.Amp.Temperature)
	}
	if want := fmt.Sprint(int64(s.Amp.FanRPM)); got[ampOIDFan] != want {
		t.Errorf("SNMP fan = %q, HTTP %v", got[ampOIDFan], s.Amp.FanRPM)
	}
	for i, ch := range s.Channels {
		n := i + 1
		if g, w := got[ampChannelOID(2, n)], fmt.Sprint(ch.OutputDB); g != w {
			t.Errorf("channel %d SNMP output = %q, HTTP %s", n, g, w)
		}
		if g, w := got[ampChannelOID(3, n)], fmt.Sprint(ch.Clip); g != w {
			t.Errorf("channel %d SNMP clip = %q, HTTP %s", n, g, w)
		}
		if g, w := got[ampChannelOID(4, n)], fmt.Sprint(ch.LoadOhms); g != w {
			t.Errorf("channel %d SNMP load = %q, HTTP %s", n, g, w)
		}
	}

	resp, err := http.Get("http://" + faceAddr(t, d, "http-api") + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/status without the token: status %d, want 401", resp.StatusCode)
	}
}

// TestPolarisAmpStandbyAndProtectResetOverHTTP: standby entered over HTTP
// reads standby over SNMP and back on again; a value outside the power
// enum is refused and changes nothing; a tripped protect is cleared by the
// reset route, and the SNMP agent says so.
func TestPolarisAmpStandbyAndProtectResetOverHTTP(t *testing.T) {
	d := startAmp(t)
	if ack := ampPost(t, d, "/api/power", `{"power": "standby"}`); ack["power"] != "standby" {
		t.Fatalf("standby ack = %v, want power standby", ack)
	}
	if v := snmpText(ampSNMP(t, d, ampOIDPower)[0]); v != "standby" {
		t.Errorf("SNMP power after standby = %q", v)
	}
	if ack := ampPost(t, d, "/api/power", `{"power": "on"}`); ack["power"] != "on" {
		t.Fatalf("power-on ack = %v", ack)
	}

	req, _ := http.NewRequest(http.MethodPost, "http://"+faceAddr(t, d, "http-api")+"/api/power", bytes.NewBufferString(`{"power": "loud"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polaris-Token", ampToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("power %q: status %d, want a refusal", "loud", resp.StatusCode)
	}
	if v := snmpText(ampSNMP(t, d, ampOIDPower)[0]); v != "on" {
		t.Errorf("SNMP power after a refused write = %q, want on", v)
	}

	// Protect trips on the amplifier itself (a scenario sets it); the reset
	// route is the operator's way back.
	st := d.State()
	st["status"].(map[string]any)["amp"].(map[string]any)["protect"] = "protect"
	d.SetState(st, 2)
	if v := snmpText(ampSNMP(t, d, ampOIDProtect)[0]); v != "protect" {
		t.Fatalf("SNMP protect after the trip = %q", v)
	}
	if ack := ampPost(t, d, "/api/protect/reset", `{}`); ack["protect"] != "normal" {
		t.Fatalf("protect reset ack = %v", ack)
	}
	if v := snmpText(ampSNMP(t, d, ampOIDProtect)[0]); v != "normal" {
		t.Errorf("SNMP protect after the reset = %q", v)
	}
}

// TestPolarisAmpDrifts: the amplifier's temperature, fan and output levels
// are behaviors, so a running amplifier's readings move as a real one's do
// and a timeline can drive them later. One tick of each writes every path it
// names, each still inside its band.
func TestPolarisAmpDrifts(t *testing.T) {
	d := startAmp(t)
	d.StopBehaviors()
	want := map[string][]string{
		"temperature_drift": {"status.amp.temperature"},
		"fan_drift":         {"status.amp.fan_rpm"},
		"output_drift":      {"status.channels.0.output_db", "status.channels.1.output_db", "status.channels.2.output_db", "status.channels.3.output_db"},
	}
	var diffs map[string]any
	d.DiffPublisher = func(c StateChange) { diffs = c.Diffs }
	d.behaviorMu.Lock()
	entries := append([]behaviorEntry(nil), d.behaviors...)
	d.behaviorMu.Unlock()
	for _, b := range entries {
		paths, ok := want[b.Name]
		if !ok {
			continue
		}
		delete(want, b.Name)
		diffs = nil
		// A random step can land on the value it started from; a few ticks
		// move it.
		for i := 0; i < 20 && len(diffs) < len(paths); i++ {
			d.tickBehavior(&b)
		}
		for _, p := range paths {
			if _, ok := diffs[p]; !ok {
				t.Errorf("behavior %s wrote no %s (diffs %v)", b.Name, p, diffs)
			}
		}
	}
	for name := range want {
		t.Errorf("behavior %s missing; have %v", name, d.Behaviors())
	}
	inBand := func(path string, lo, hi float64) {
		f, _, err := toFloat(d.ReadPath(path))
		if err != nil || f < lo || f > hi {
			t.Errorf("%s = %v (%v), want within [%v, %v]", path, d.ReadPath(path), err, lo, hi)
		}
	}
	inBand("status.amp.temperature", 34, 52)
	inBand("status.amp.fan_rpm", 2200, 3600)
	for n := 0; n < 4; n++ {
		inBand(fmt.Sprintf("status.channels.%d.output_db", n), -40, -6)
	}
}
