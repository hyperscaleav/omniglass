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

// The Meridian switcher's and extender's faces (#895): the MX 84's input
// and output tables over SNMP and its routing over HTTP, and the XL-1's
// link and signal over SNMP, each its own profile.

const meridianKey = "meridian-lab-key"

func startMeridian(t *testing.T, name string) *Device {
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
	d.StopBehaviors()
	return d
}

func meridianSNMP(t *testing.T, d *Device, oids ...string) map[string]string {
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
			out[v.Name[1:]] = fmt.Sprint(v.Value)
		}
	}
	return out
}

func meridianDo(t *testing.T, d *Device, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+faceAddr(t, d, "http-api")+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Meridian-Key", meridianKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func switcherOutput(n int, column int) string {
	return fmt.Sprintf("1.3.6.1.4.1.55550.2.%d.%d", column, n)
}

// TestMeridianSwitcherRoutesAndReportsSignal: the SNMP tables answer what
// the JSON status reports; an input routed over HTTP reads back on the
// output with that input's signal; an input going dark darkens every output
// showing it on the next tick; an output or input outside the switcher's is
// refused.
func TestMeridianSwitcherRoutesAndReportsSignal(t *testing.T) {
	d := startMeridian(t, "meridian-mx-84")
	code, s := meridianDo(t, d, http.MethodGet, "/api/status", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/status: %d", code)
	}
	outs := s["outputs"].(map[string]any)
	got := meridianSNMP(t, d, switcherOutput(1, 1), switcherOutput(1, 2), switcherOutput(3, 1), "1.3.6.1.4.1.55550.1.3")
	if want := fmt.Sprint(outs["1"].(map[string]any)["input"]); got[switcherOutput(1, 1)] != want {
		t.Errorf("SNMP output 1 input = %q, HTTP %q", got[switcherOutput(1, 1)], want)
	}
	if got[switcherOutput(1, 2)] != "true" || got["1.3.6.1.4.1.55550.1.3"] != "false" {
		t.Errorf("SNMP output 1 signal / input 3 signal = %q/%q, want true/false", got[switcherOutput(1, 2)], got["1.3.6.1.4.1.55550.1.3"])
	}

	// Route the dark hdmi3 to output 1: the output shows it, dark.
	if code, _ := meridianDo(t, d, http.MethodPost, "/api/route", `{"output": 1, "input": "hdmi3"}`); code != http.StatusOK {
		t.Fatalf("route: %d", code)
	}
	if got := meridianSNMP(t, d, switcherOutput(1, 1), switcherOutput(1, 2)); got[switcherOutput(1, 1)] != "hdmi3" || got[switcherOutput(1, 2)] != "false" {
		t.Errorf("after routing hdmi3 to output 1: %v, want hdmi3, no signal", got)
	}

	// The room PC (hdmi1, on outputs 2 and 4) goes to sleep: its outputs go
	// dark on the next tick; output 3 (hdmi2) holds.
	if code, _ := meridianDo(t, d, http.MethodPost, "/api/inputs/signal", `{"input": "hdmi1", "signal": false}`); code != http.StatusOK {
		t.Fatalf("input signal: %d", code)
	}
	d.behaviorMu.Lock()
	var follow *behaviorEntry
	for i := range d.behaviors {
		if d.behaviors[i].Name == "output_signal_follow" {
			follow = &d.behaviors[i]
		}
	}
	d.behaviorMu.Unlock()
	if follow == nil {
		t.Fatalf("no output_signal_follow behavior; have %v", d.Behaviors())
	}
	d.tickBehavior(follow)
	got = meridianSNMP(t, d, switcherOutput(2, 2), switcherOutput(3, 2), switcherOutput(4, 2))
	if got[switcherOutput(2, 2)] != "false" || got[switcherOutput(4, 2)] != "false" || got[switcherOutput(3, 2)] != "true" {
		t.Errorf("output signals after hdmi1 went dark = %v, want 2 and 4 dark, 3 lit", got)
	}

	for _, body := range []string{`{"output": 5, "input": "hdmi1"}`, `{"output": 1, "input": "sdi1"}`} {
		if code, _ := meridianDo(t, d, http.MethodPost, "/api/route", body); code != http.StatusBadRequest {
			t.Errorf("route %s: %d, want 400", body, code)
		}
	}
	if v := meridianSNMP(t, d, switcherOutput(1, 1))[switcherOutput(1, 1)]; v != "hdmi3" {
		t.Errorf("output 1 after refused routes = %q, want hdmi3", v)
	}
}

// TestMeridianExtenderReportsLinkAndSignal: the extender's SNMP agent
// answers the link and signal its JSON status reports, and a link dropped
// reads down over SNMP.
func TestMeridianExtenderReportsLinkAndSignal(t *testing.T) {
	d := startMeridian(t, "meridian-xl-1")
	code, s := meridianDo(t, d, http.MethodGet, "/api/status", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/status: %d", code)
	}
	link := s["link"].(map[string]any)
	got := meridianSNMP(t, d, "1.3.6.1.4.1.55550.3.1.0", "1.3.6.1.4.1.55550.3.2.0")
	if got["1.3.6.1.4.1.55550.3.1.0"] != link["state"] || got["1.3.6.1.4.1.55550.3.2.0"] != fmt.Sprint(link["signal"]) {
		t.Errorf("SNMP link/signal = %v, HTTP %v", got, link)
	}
	if link["state"] != "up" {
		t.Errorf("link at rest = %v, want up", link["state"])
	}
	st := d.State()
	st["status"].(map[string]any)["link"] = map[string]any{"state": "down", "signal": false}
	d.SetState(st, 2)
	if got := meridianSNMP(t, d, "1.3.6.1.4.1.55550.3.1.0", "1.3.6.1.4.1.55550.3.2.0"); got["1.3.6.1.4.1.55550.3.1.0"] != "down" || got["1.3.6.1.4.1.55550.3.2.0"] != "false" {
		t.Errorf("SNMP after the link dropped = %v, want down, false", got)
	}
}
