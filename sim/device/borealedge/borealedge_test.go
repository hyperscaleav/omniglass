package borealedge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

// The model table is the line: five sizes, each a product with its own
// identity, resolution and summary, the table's rows and the catalog agreeing.
func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(Spec())
	if err != nil {
		t.Fatal(err)
	}
	if len(Rows) != 5 {
		t.Fatalf("the Edge line has %d rows, want 5 sizes", len(Rows))
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v, want the %s family", r.Product, res.Definition, Name)
		}
		if r.Product != fmt.Sprintf("boreal-edge-%d", r.Diagonal) {
			t.Errorf("%s: the product is not named for its %d-inch diagonal", r.Product, r.Diagonal)
		}
		if res.Identity.Model != fmt.Sprintf("Edge %d", r.Diagonal) || res.Identity.Manufacturer != "Boreal" || res.Identity.DeviceType != "display" {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		if !strings.Contains(res.Identity.Summary, fmt.Sprintf("%d-inch", r.Diagonal)) {
			t.Errorf("%s: summary %q does not say its size", r.Product, res.Identity.Summary)
		}
		if got, want := res.Overlay["configuration"].(map[string]any)["device"].(map[string]any)["model"], r.ModelName(); got != want {
			t.Errorf("%s: the overlay sets model %v, want %v", r.Product, got, want)
		}
		if r.Resolution != "3840x2160" {
			t.Errorf("%s: resolution %q: the line is 4K", r.Product, r.Resolution)
		}
	}
}

// A 98-inch reports itself as one: in its status document, its dashboard's
// title, bar and device card, and the SNMP sysDescr, never as another size.
func TestEverySizeReportsItsOwnModelEverywhere(t *testing.T) {
	for _, r := range Rows {
		t.Run(r.Product, func(t *testing.T) {
			f := spectest.NewFake(Spec(), r.Product)
			want := fmt.Sprintf("Edge %d", r.Diagonal)

			var status statusDoc
			res := statusRoute(f, spec.HTTPRequest{})
			if err := json.Unmarshal(res.Body.([]byte), &status); err != nil || status.Device.Model != want {
				t.Errorf("status model = %q (%v), want %q", status.Device.Model, err, want)
			}

			page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
			for _, mark := range []string{
				fmt.Sprintf("<title>%s, Boreal %s</title>", f.Hostname(), want),
				fmt.Sprintf(`<div class="model">%s</div>`, want),
				fmt.Sprintf(`<span class="k">model</span><span class="v">%s</span>`, want),
			} {
				if !strings.Contains(page, mark) {
					t.Errorf("the dashboard lacks %q", mark)
				}
			}
			for _, other := range Rows {
				if other.Diagonal != r.Diagonal && strings.Contains(page, fmt.Sprintf("Edge %d", other.Diagonal)) {
					t.Errorf("the %d-inch dashboard names the %d-inch model", r.Diagonal, other.Diagonal)
				}
			}

			for _, o := range surfaceOf(f).OIDs {
				if o.OID == "1.3.6.1.2.1.1.1.0" && o.Get() != want {
					t.Errorf("sysDescr = %v, want %q", o.Get(), want)
				}
			}
		})
	}
}

// The dashboard is html/template: what a device says of itself is escaped, and
// the script is the page's own, never device data.
func TestDashboardEscapesWhatTheDeviceSays(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-edge-55")
	f.Host = `<b>x</b>&"`
	page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	if strings.Contains(page, "<b>x</b>") || !strings.Contains(page, "&lt;b&gt;x&lt;/b&gt;&amp;") {
		t.Fatalf("a hostname with markup reached the page unescaped")
	}
	if !strings.Contains(page, "// Browser inherits the Basic auth credentials") {
		t.Fatal("the page's script lost its comments")
	}
}

// The dashboard follows the state: asleep, muted and on another input it shows
// each.
func TestDashboardShowsTheDevicesState(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-edge-75")
	rest := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	if !strings.Contains(rest, `<span class="pill on">on</span>`) || !strings.Contains(rest, `<option value="hdmi1" selected>`) || !strings.Contains(rest, "Mute audio") {
		t.Fatalf("the dashboard at rest does not show on, hdmi1 and unmuted")
	}
	_ = f.Write(pathPower, "standby")
	_ = f.Write(pathInput, "usbc")
	_ = f.Write(pathMute, true)
	moved := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	for _, mark := range []string{`<span class="pill off">off</span>`, `<option value="usbc"  selected>`, "Muted &mdash; click to unmute"} {
		if !strings.Contains(moved, mark) {
			t.Errorf("the dashboard after the changes lacks %q", mark)
		}
	}
}

// The telemetry is SNMP's alone: the identity document carries no power,
// input, volume or firmware, and SNMP carries no firmware.
func TestTelemetryIsSNMPsAndFirmwareIsTheDashboardsAlone(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-edge-86")
	body := string(statusRoute(f, spec.HTTPRequest{}).Body.([]byte))
	for _, word := range []string{"power", "input", "volume", "brightness", "firmware"} {
		if strings.Contains(body, word) {
			t.Errorf("the identity document mentions %q", word)
		}
	}
	for _, o := range surfaceOf(f).OIDs {
		if strings.HasSuffix(o.OID, ".5.1.0") || o.Path == pathFirmware {
			t.Errorf("SNMP serves the firmware at %s", o.OID)
		}
	}
	if !strings.Contains(string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte)), "3.4.1") {
		t.Error("the dashboard does not show the firmware")
	}
}

// SNMP reads the types it always did: text, integers (a fractional panel
// temperature truncated), and the mute as true or false.
func TestSNMPReadsTheStateAsTheProfileDid(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-edge-86")
	_ = f.Write(pathTemperature, 38.8)
	_ = f.Write(pathMute, true)
	got := map[string]surface.OID{}
	for _, o := range surfaceOf(f).OIDs {
		got[o.OID] = o
	}
	for oid, want := range map[string]any{
		"1.3.6.1.4.1.55520.1.1.1.0": "on",
		"1.3.6.1.4.1.55520.2.3.0":   80,
		"1.3.6.1.4.1.55520.2.4.0":   38,
		"1.3.6.1.4.1.55520.3.2.0":   "true",
		"1.3.6.1.2.1.1.1.0":         "Edge 86",
	} {
		o, ok := got[oid]
		if !ok || o.Get() != want {
			t.Errorf("OID %s = %v (served %v), want %v", oid, o.Get(), ok, want)
		}
	}
	if got["1.3.6.1.4.1.55520.2.4.0"].Kind != surface.KindInteger || got["1.3.6.1.4.1.55520.3.2.0"].Kind != surface.KindOctetString {
		t.Error("an OID is served as the wrong SNMP type")
	}
}

// A reboot puts uptime back to zero and the volatile display state back to its
// defaults; a factory reset puts back every status path. Neither touches what
// it is not told to.
func TestRebootAndFactoryResetRestoreWhatTheyName(t *testing.T) {
	route := func(f *spectest.Fake, match string) {
		for _, r := range Spec().HTTP {
			if r.Match == match {
				if res := r.Handle(f, spec.HTTPRequest{}); res.Status != 0 {
					t.Fatalf("%s: status %d", match, res.Status)
				}
				return
			}
		}
		t.Fatalf("no route %s", match)
	}
	away := func(f *spectest.Fake) {
		for path, v := range map[string]any{pathPower: "standby", pathInput: "usbc", pathBrightness: int64(12), pathVolume: int64(77), pathMute: true, pathContrast: int64(5)} {
			_ = f.Write(path, v)
		}
	}

	f := spectest.NewFake(Spec(), "boreal-edge-98")
	away(f)
	route(f, "POST /api/admin/reboot")
	if f.Read(pathBootEpoch) != f.Clock.Unix() || f.Read(pathInput) != "hdmi1" || f.Read(pathBrightness) != 80 {
		t.Errorf("after a reboot: epoch %v input %v brightness %v", f.Read(pathBootEpoch), f.Read(pathInput), f.Read(pathBrightness))
	}
	if f.Read(pathPower) != "standby" || f.Read(pathVolume) != int64(77) || f.Read(pathMute) != true {
		t.Errorf("a reboot reset what it does not name: power %v volume %v mute %v", f.Read(pathPower), f.Read(pathVolume), f.Read(pathMute))
	}
	if len(f.Writes) != 7 || len(f.Writes[6]) != 3 {
		t.Errorf("a reboot is one change of three paths, got %+v", f.Writes[len(f.Writes)-1])
	}

	away(f)
	route(f, "POST /api/admin/factory-reset")
	for path, want := range map[string]any{pathPower: "on", pathInput: "hdmi1", pathBrightness: 80, pathContrast: 70, pathVolume: 20, pathMute: false, pathResolution: "3840x2160"} {
		if f.Read(path) != want {
			t.Errorf("after a factory reset %s = %v, want %v", path, f.Read(path), want)
		}
	}
}

// The quick sets are the panel's own choices.
func TestRoutesOfferThePanelsPowerAndInputs(t *testing.T) {
	sets := map[string][]string{}
	for _, r := range Spec().HTTP {
		for _, s := range r.Sets {
			sets[s.Path] = s.Values
		}
	}
	if fmt.Sprint(sets[pathPower]) != "[on off standby]" || fmt.Sprint(sets[pathInput]) != "[hdmi1 hdmi2 hdmi3 dp1 usbc]" || len(sets) != 2 {
		t.Fatalf("quick sets = %v", sets)
	}
}
