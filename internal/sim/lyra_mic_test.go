//go:build linux

package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The Lyra microphones' faces (#894): the Arc A2 ceiling array and the HX-1
// wireless system, each its own profile (they are different kinds of
// microphone), answering mute and the input level over SNMP on the Lyra
// arc, the HX-1 its transmitter's battery too, and muted over HTTP.

const (
	lyraOIDMute    = "1.3.6.1.4.1.55530.1.1.0"
	lyraOIDLevel   = "1.3.6.1.4.1.55530.1.2.0"
	lyraOIDBattery = "1.3.6.1.4.1.55530.2.1.0"
	lyraKey        = "lyra-lab-key"
)

func startLyra(t *testing.T, name string) *Device {
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

// lyraSNMP gets oids, rendering each as a collector reads it; an OID the
// agent does not serve reads as "absent".
func lyraSNMP(t *testing.T, d *Device, oids ...string) map[string]string {
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
		switch val := v.Value.(type) {
		case []byte:
			out[v.Name[1:]] = string(val)
		case nil:
			out[v.Name[1:]] = "absent"
		default:
			out[v.Name[1:]] = fmt.Sprint(gosnmp.ToBigInt(val))
		}
	}
	return out
}

func lyraDo(t *testing.T, d *Device, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Lyra-Key", lyraKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestLyraMicsServeMuteAndLevel: each microphone's SNMP agent answers the
// mute and level its JSON status reports, and a mute set over HTTP reads
// back over SNMP; an off-enum mute is refused. The ceiling array has no
// battery to report; the wireless system answers its transmitter's.
func TestLyraMicsServeMuteAndLevel(t *testing.T) {
	for _, tc := range []struct {
		profile, model string
		battery        bool
	}{
		{"lyra-arc-a2", "Arc A2", false},
		{"lyra-hx-1", "HX-1", true},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			d := startLyra(t, tc.profile)
			code, s := lyraDo(t, d, http.MethodGet, "/api/status", "")
			if code != http.StatusOK {
				t.Fatalf("GET /api/status: %d", code)
			}
			dev, mic := s["device"].(map[string]any), s["mic"].(map[string]any)
			if dev["model"] != tc.model {
				t.Errorf("model = %v, want %s", dev["model"], tc.model)
			}
			got := lyraSNMP(t, d, lyraOIDMute, lyraOIDLevel, lyraOIDBattery)
			if got[lyraOIDMute] != fmt.Sprint(mic["mute"]) || got[lyraOIDLevel] != fmt.Sprint(mic["level_db"]) {
				t.Errorf("SNMP mute/level = %q/%q, HTTP %v/%v", got[lyraOIDMute], got[lyraOIDLevel], mic["mute"], mic["level_db"])
			}
			if tc.battery {
				if got[lyraOIDBattery] != fmt.Sprint(mic["battery_percent"]) {
					t.Errorf("SNMP battery = %q, HTTP %v", got[lyraOIDBattery], mic["battery_percent"])
				}
			} else if got[lyraOIDBattery] != "absent" {
				t.Errorf("the ceiling array answers a battery: %q", got[lyraOIDBattery])
			}

			if code, ack := lyraDo(t, d, http.MethodPost, "/api/mute", `{"mute": true}`); code != http.StatusOK || ack["mute"] != true {
				t.Fatalf("mute = %d %v", code, ack)
			}
			if v := lyraSNMP(t, d, lyraOIDMute)[lyraOIDMute]; v != "true" {
				t.Errorf("SNMP mute after muting = %q", v)
			}
			if code, _ := lyraDo(t, d, http.MethodPost, "/api/mute", `{"mute": "loud"}`); code != http.StatusBadRequest {
				t.Errorf("mute %q: %d, want 400", "loud", code)
			}
			if v := lyraSNMP(t, d, lyraOIDMute)[lyraOIDMute]; v != "true" {
				t.Errorf("SNMP mute after a refused write = %q, want true", v)
			}
		})
	}
}

// TestLyraMicBehaviors: the input level moves within a talker's band on
// each tick; the wireless system's battery drains a tenth of a percent a
// tick, exactly.
func TestLyraMicBehaviors(t *testing.T) {
	d := startLyra(t, "lyra-hx-1")
	tick := func(name string) {
		t.Helper()
		d.behaviorMu.Lock()
		var b *behaviorEntry
		for i := range d.behaviors {
			if d.behaviors[i].Name == name {
				b = &d.behaviors[i]
			}
		}
		d.behaviorMu.Unlock()
		if b == nil {
			t.Fatalf("no %s behavior; have %v", name, d.Behaviors())
		}
		d.tickBehavior(b)
	}
	read := func(path string) float64 {
		f, _, err := toFloat(d.ReadPath(path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return f
	}
	for i := 0; i < 20; i++ {
		tick("level_drift")
		if l := read("status.mic.level_db"); l < -70 || l > -20 {
			t.Fatalf("level after %d ticks = %v, outside -70 to -20", i+1, l)
		}
	}
	start := read("status.mic.battery_percent")
	for i := 0; i < 10; i++ {
		tick("battery_drain")
	}
	if b := read("status.mic.battery_percent"); math.Abs(start-b-1.0) > 1e-9 {
		t.Errorf("battery after ten minutes = %v from %v, want 1.0 less", b, start)
	}
}
