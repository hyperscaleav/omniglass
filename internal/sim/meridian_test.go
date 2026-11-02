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

// The Meridian signal devices' faces (#895): the MX 84 presentation switcher
// and the XL-1 AV extender, each its own profile (a switcher and an extender
// are different kinds of component), answering SNMP on the Meridian arc. The
// switcher reports its routed input and the signal on each input and takes a
// new route over HTTP; the extender reports its link and the signal on its
// one input.

const (
	meridianOIDRouted     = "1.3.6.1.4.1.55550.1.1.0"
	meridianOIDRoutedSig  = "1.3.6.1.4.1.55550.1.2.0"
	meridianOIDLink       = "1.3.6.1.4.1.55550.2.1.0"
	meridianOIDExtSignal  = "1.3.6.1.4.1.55550.2.2.0"
	meridianOIDUptime     = "1.3.6.1.2.1.1.3.0"
	meridianKey           = "meridian-lab-key"
	meridianInputTableCol = "1.3.6.1.4.1.55550.1.10.1.2." // + the input's number, 1 to 4
)

var meridianInputs = []string{"hdmi1", "hdmi2", "hdmi3", "dp1"}

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
	d.StopBehaviors() // hold the readings still between reads
	return d
}

// meridianSNMP gets oids, rendering each as a collector reads it; an OID the
// agent does not serve reads as "absent".
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

// meridianSet writes a state path on the device, as a scenario does.
func meridianSet(t *testing.T, d *Device, path string, value any) {
	t.Helper()
	st := d.State()
	if err := SetPath(st, path, value); err != nil {
		t.Fatalf("set %s: %v", path, err)
	}
	d.SetState(st, time.Now().UnixNano())
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

// TestMeridianSwitcherServesRouteAndSignalOverSNMP: the MX 84 answers its
// routed input, the signal on the routed input, and the signal on each of
// its four inputs, the same readings its JSON status reports; its identity
// is its own, and the extender's link is not on it.
func TestMeridianSwitcherServesRouteAndSignalOverSNMP(t *testing.T) {
	d := startMeridian(t, "meridian-mx-84")
	code, s := meridianDo(t, d, http.MethodGet, "/api/status", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/status: %d", code)
	}
	if model := s["device"].(map[string]any)["model"]; model != "MX 84" {
		t.Errorf("model = %v, want MX 84", model)
	}
	sw := s["switcher"].(map[string]any)

	oids := []string{meridianOIDRouted, meridianOIDRoutedSig, meridianOIDUptime, meridianOIDLink}
	for i := range meridianInputs {
		oids = append(oids, fmt.Sprintf("%s%d", meridianInputTableCol, i+1))
	}
	got := meridianSNMP(t, d, oids...)
	if got[meridianOIDRouted] != fmt.Sprint(sw["input"]) {
		t.Errorf("SNMP routed input = %q, HTTP %v", got[meridianOIDRouted], sw["input"])
	}
	inputs := sw["inputs"].(map[string]any)
	for i, name := range meridianInputs {
		want := fmt.Sprint(inputs[name].(map[string]any)["signal"])
		if g := got[fmt.Sprintf("%s%d", meridianInputTableCol, i+1)]; g != want {
			t.Errorf("SNMP signal on %s = %q, HTTP %q", name, g, want)
		}
	}
	routed := fmt.Sprint(sw["input"])
	if want := fmt.Sprint(inputs[routed].(map[string]any)["signal"]); got[meridianOIDRoutedSig] != want {
		t.Errorf("SNMP signal on the routed input %s = %q, want %q", routed, got[meridianOIDRoutedSig], want)
	}
	if got[meridianOIDUptime] == "absent" {
		t.Error("the switcher answers no uptime")
	}
	if got[meridianOIDLink] != "absent" {
		t.Errorf("the switcher answers a link state: %q", got[meridianOIDLink])
	}
}

// TestMeridianSwitcherRouteOverHTTP: a route set over HTTP reads back over
// SNMP at once, and the signal on the routed input follows the new route;
// an input the switcher does not have is refused and changes nothing.
func TestMeridianSwitcherRouteOverHTTP(t *testing.T) {
	d := startMeridian(t, "meridian-mx-84")
	// Lose the signal on dp1 (a source unplugged), then route to it.
	meridianSet(t, d, "status.switcher.inputs.dp1.signal", "absent")

	code, ack := meridianDo(t, d, http.MethodPost, "/api/route", `{"input": "dp1"}`)
	if code != http.StatusOK || ack["input"] != "dp1" {
		t.Fatalf("route dp1 = %d %v", code, ack)
	}
	got := meridianSNMP(t, d, meridianOIDRouted, meridianOIDRoutedSig)
	if got[meridianOIDRouted] != "dp1" || got[meridianOIDRoutedSig] != "absent" {
		t.Errorf("after routing to the dead dp1: routed %q, signal %q", got[meridianOIDRouted], got[meridianOIDRoutedSig])
	}

	if code, _ := meridianDo(t, d, http.MethodPost, "/api/route", `{"input": "hdmi2"}`); code != http.StatusOK {
		t.Fatalf("route hdmi2 = %d", code)
	}
	got = meridianSNMP(t, d, meridianOIDRouted, meridianOIDRoutedSig)
	if got[meridianOIDRouted] != "hdmi2" || got[meridianOIDRoutedSig] != "present" {
		t.Errorf("after routing to hdmi2: routed %q, signal %q", got[meridianOIDRouted], got[meridianOIDRoutedSig])
	}

	if code, _ := meridianDo(t, d, http.MethodPost, "/api/route", `{"input": "vga"}`); code != http.StatusBadRequest {
		t.Errorf("route vga: %d, want 400", code)
	}
	if v := meridianSNMP(t, d, meridianOIDRouted)[meridianOIDRouted]; v != "hdmi2" {
		t.Errorf("routed input after a refused route = %q, want hdmi2", v)
	}
}

// TestMeridianExtenderServesLinkAndSignalOverSNMP: the XL-1 answers its link
// and the signal on its input, the same readings its JSON status reports,
// under its own identity; the switcher's route is not on it, and a reboot
// restarts its uptime.
func TestMeridianExtenderServesLinkAndSignalOverSNMP(t *testing.T) {
	d := startMeridian(t, "meridian-xl-1")
	code, s := meridianDo(t, d, http.MethodGet, "/api/status", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/status: %d", code)
	}
	if model := s["device"].(map[string]any)["model"]; model != "XL-1" {
		t.Errorf("model = %v, want XL-1", model)
	}
	ext := s["extender"].(map[string]any)
	got := meridianSNMP(t, d, meridianOIDLink, meridianOIDExtSignal, meridianOIDUptime, meridianOIDRouted)
	if got[meridianOIDLink] != fmt.Sprint(ext["link"]) || got[meridianOIDExtSignal] != fmt.Sprint(ext["signal"]) {
		t.Errorf("SNMP link/signal = %q/%q, HTTP %v/%v", got[meridianOIDLink], got[meridianOIDExtSignal], ext["link"], ext["signal"])
	}
	if got[meridianOIDLink] != "up" {
		t.Errorf("link at rest = %q, want up", got[meridianOIDLink])
	}
	if got[meridianOIDRouted] != "absent" {
		t.Errorf("the extender answers a routed input: %q", got[meridianOIDRouted])
	}
	if code, _ := meridianDo(t, d, http.MethodPost, "/api/route", `{"input": "hdmi2"}`); code != http.StatusNotFound {
		t.Errorf("the extender takes a route: %d, want 404", code)
	}

	meridianSet(t, d, "status.extender.link", "down")
	if v := meridianSNMP(t, d, meridianOIDLink)[meridianOIDLink]; v != "down" {
		t.Errorf("SNMP link after the cable is pulled = %q, want down", v)
	}
	if code, _ := meridianDo(t, d, http.MethodPost, "/api/admin/reboot", `{}`); code != http.StatusOK {
		t.Fatalf("reboot = %d", code)
	}
	if v := meridianSNMP(t, d, meridianOIDLink)[meridianOIDLink]; v != "up" {
		t.Errorf("SNMP link after a reboot = %q, want up", v)
	}
}

// TestMeridianSwitcherSignalFollowsTheRoutedInput: a source unplugged from
// the routed input drops the signal the switcher reports for it on the next
// tick, and plugged back in, restores it.
func TestMeridianSwitcherSignalFollowsTheRoutedInput(t *testing.T) {
	d := startMeridian(t, "meridian-mx-84")
	tick := func() {
		t.Helper()
		d.behaviorMu.Lock()
		var b *behaviorEntry
		for i := range d.behaviors {
			if d.behaviors[i].Name == "signal_follow" {
				b = &d.behaviors[i]
			}
		}
		d.behaviorMu.Unlock()
		if b == nil {
			t.Fatalf("no signal_follow behavior; have %v", d.Behaviors())
		}
		d.tickBehavior(b)
	}
	routed := fmt.Sprint(d.ReadPath("status.switcher.input"))
	for _, signal := range []string{"absent", "present"} {
		meridianSet(t, d, "status.switcher.inputs."+routed+".signal", signal)
		tick()
		if v := meridianSNMP(t, d, meridianOIDRoutedSig)[meridianOIDRoutedSig]; v != signal {
			t.Errorf("signal on routed %s after it reads %s = %q", routed, signal, v)
		}
	}
}

// TestMeridianProductsHaveProfiles: each Meridian product in the catalog is
// simulated by a profile.
func TestMeridianProductsHaveProfiles(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"meridian-mx-84", "meridian-xl-1"} {
		if _, _, ok := store.ForProduct(name); !ok {
			t.Errorf("no profile simulates %s", name)
		}
	}
}
