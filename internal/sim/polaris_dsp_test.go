//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Polaris DSP 16's program output (#1493): the mute and level of the bus
// that feeds the room, answered over SNMP on the DSP's branch of the Polaris
// arc and muted over its HTTP API, from one live state. The per-channel
// strips (gain, mute, peak) are a separate surface; this is the pair an
// operator watches to know the room is audible.

const (
	dspOIDMute  = "1.3.6.1.4.1.55540.2.5.0"
	dspOIDLevel = "1.3.6.1.4.1.55540.2.6.0"
)

func startDSP(t *testing.T) *Device {
	t.Helper()
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "dsp-1", Hostname: "dsp-1", LoopbackIP: ip, ProfileName: "polaris-dsp-16", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return deviceByProfile(t, e, "polaris-dsp-16")
}

func dspPostRaw(t *testing.T, d *Device, path, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polaris-Token", ampToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestPolarisDSPServesItsProgramOutput: the SNMP agent answers the mute and
// level the DSP's JSON status reports for its program output, unmuted and at
// a level inside the band a room is run at.
func TestPolarisDSPServesItsProgramOutput(t *testing.T) {
	d := startDSP(t)
	d.StopBehaviors() // hold the level still between the two reads
	resp, err := authedGet("http://"+faceAddr(t, d, "http-api")+"/api/status", ampToken)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s struct {
		Output struct {
			Mute    bool    `json:"mute"`
			LevelDB float64 `json:"level_db"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decoding /api/status: %v", err)
	}
	if s.Output.Mute || s.Output.LevelDB < -60 || s.Output.LevelDB > 0 {
		t.Errorf("program output at rest = muted %v at %v dB, want unmuted inside -60..0", s.Output.Mute, s.Output.LevelDB)
	}
	got := map[string]string{}
	for _, v := range ampSNMP(t, d, dspOIDMute, dspOIDLevel) {
		got[v.Name[1:]] = snmpText(v)
	}
	if got[dspOIDMute] != fmt.Sprint(s.Output.Mute) || got[dspOIDLevel] != fmt.Sprint(s.Output.LevelDB) {
		t.Errorf("SNMP mute/level = %q/%q, HTTP %v/%v", got[dspOIDMute], got[dspOIDLevel], s.Output.Mute, s.Output.LevelDB)
	}
}

// TestPolarisDSPMuteOverHTTPReadsBackOverSNMP: muting the program output over
// the control API reads muted over SNMP and unmuting reads it back; a mute
// outside the boolean enum is refused and changes nothing.
func TestPolarisDSPMuteOverHTTPReadsBackOverSNMP(t *testing.T) {
	d := startDSP(t)
	d.StopBehaviors()
	read := func() string { return snmpText(ampSNMP(t, d, dspOIDMute)[0]) }
	if v := read(); v != "false" {
		t.Fatalf("SNMP mute at rest = %q, want false", v)
	}
	if ack := ampPost(t, d, "/api/output/mute", `{"mute": true}`); ack["mute"] != true {
		t.Fatalf("mute ack = %v", ack)
	}
	if v := read(); v != "true" {
		t.Errorf("SNMP mute after muting = %q", v)
	}
	if code := dspPostRaw(t, d, "/api/output/mute", `{"mute": "loud"}`); code < 400 {
		t.Errorf("mute %q: status %d, want a refusal", "loud", code)
	}
	if v := read(); v != "true" {
		t.Errorf("SNMP mute after a refused write = %q, want true", v)
	}
	if ack := ampPost(t, d, "/api/output/mute", `{"mute": false}`); ack["mute"] != false {
		t.Fatalf("unmute ack = %v", ack)
	}
	if v := read(); v != "false" {
		t.Errorf("SNMP mute after unmuting = %q", v)
	}
}

// TestPolarisDSPOutputLevelDrifts: the program level is a behavior, so a
// running DSP's reading moves as program audio does; a tick writes the path
// and leaves it inside its band.
func TestPolarisDSPOutputLevelDrifts(t *testing.T) {
	d := startDSP(t)
	d.StopBehaviors()
	var diffs map[string]any
	d.DiffPublisher = func(c StateChange) { diffs = c.Diffs }
	d.behaviorMu.Lock()
	entries := append([]behaviorEntry(nil), d.behaviors...)
	d.behaviorMu.Unlock()
	found := false
	for _, b := range entries {
		if b.Name != "output_level_drift" {
			continue
		}
		found = true
		// A random step can land on the value it started from; a few ticks move it.
		for i := 0; i < 20; i++ {
			d.tickBehavior(&b)
			if _, ok := diffs["status.output.level_db"]; ok {
				break
			}
		}
	}
	if !found {
		t.Fatalf("no output_level_drift behavior; have %v", d.Behaviors())
	}
	if _, ok := diffs["status.output.level_db"]; !ok {
		t.Errorf("output_level_drift wrote no status.output.level_db (diffs %v)", diffs)
	}
	f, _, err := toFloat(d.ReadPath("status.output.level_db"))
	if err != nil || f < -40 || f > -6 {
		t.Errorf("status.output.level_db = %v (%v), want within [-40, -6]", d.ReadPath("status.output.level_db"), err)
	}
}

// TestPolarisDSPRebootRestoresTheProgramOutput: a reboot returns the program
// output to its declared defaults, as a power cycle does the real DSP's
// volatile state.
func TestPolarisDSPRebootRestoresTheProgramOutput(t *testing.T) {
	d := startDSP(t)
	d.StopBehaviors()
	ampPost(t, d, "/api/output/mute", `{"mute": true}`)
	ampPost(t, d, "/api/admin/reboot", `{}`)
	if v := snmpText(ampSNMP(t, d, dspOIDMute)[0]); v != "false" {
		t.Errorf("SNMP mute after a reboot = %q, want false", v)
	}
}
