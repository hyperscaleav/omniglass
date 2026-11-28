package borealbeam

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v", r.Product, res.Definition)
		}
		if res.Identity.Model != r.Model || res.Identity.DeviceType != "projector" || res.Identity.Summary == "" {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		var doc statusDoc
		if err := json.Unmarshal(statusRoute(f, spec.HTTPRequest{}).Body.([]byte), &doc); err != nil || doc.Device.Model != r.Model || doc.Device.Firmware != r.Firmware {
			t.Errorf("%s: status = %+v (%v)", r.Product, doc.Device, err)
		}
		if doc.Projector.LightSourceHours != r.LightSourceHours {
			t.Errorf("%s: light source hours %v, row says %v", r.Product, doc.Projector.LightSourceHours, r.LightSourceHours)
		}
	}
}

// The light source ages only while the projector is on: a minute on adds a
// minute's hours, standby adds none, and a tick writes nothing else.
func TestLightSourceAgesOnlyWhileOn(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-beam-340")
	var aging spec.Tick
	for _, tk := range Spec().Ticks {
		if tk.Name == "light_source_aging" {
			aging = tk
		}
	}
	run := func() float64 {
		for _, c := range aging.Run(f, spec.TickContext{}) {
			_ = f.Write(c.Path, c.Value)
		}
		return spec.Float(f, pathHours)
	}
	start := spec.Float(f, pathHours)
	for i := 0; i < 3; i++ {
		run()
	}
	// Each minute adds 0.0166667 kept to the ten-thousandth, so three add 0.0501.
	if got := spec.Float(f, pathHours); math.Abs(got-start-0.0501) > 1e-9 {
		t.Fatalf("three lit minutes moved %v to %v, want 0.0501 more", start, got)
	}
	_ = f.Write(pathPower, "standby")
	before := spec.Float(f, pathHours)
	if after := run(); after != before {
		t.Fatalf("in standby the hours went %v to %v", before, after)
	}
}

// The projector has no off, only standby; the shutter blanks the picture with
// the projector still on; a reboot opens it.
func TestControlsAreTheProjectorsOwn(t *testing.T) {
	sets := map[string][]string{}
	var reboot spec.HTTPRoute
	for _, r := range Spec().HTTP {
		for _, s := range r.Sets {
			sets[s.Path] = s.Values
		}
		if r.Match == "POST /api/admin/reboot" {
			reboot = r
		}
	}
	if fmt.Sprint(sets[pathPower]) != "[on standby]" || fmt.Sprint(sets[pathShutter]) != "[open closed]" || fmt.Sprint(sets[pathInput]) != "[hdmi1 hdmi2]" {
		t.Fatalf("quick sets = %v", sets)
	}
	f := spectest.NewFake(Spec(), "boreal-beam-340")
	_ = f.Write(pathShutter, "closed")
	_ = f.Write(pathPower, "standby")
	reboot.Handle(f, spec.HTTPRequest{})
	if f.Read(pathShutter) != "open" || f.Read(pathBootEpoch) != f.Clock.Unix() || f.Read(pathPower) != "standby" {
		t.Errorf("after a reboot: shutter %v epoch %v power %v", f.Read(pathShutter), f.Read(pathBootEpoch), f.Read(pathPower))
	}
}

// SNMP: the identity arc is the Beam's own (firmware and serial), its branch
// is .6, and the hours are text where the temperature is an integer.
func TestSNMPServesTheBoreal6BranchAndTheIdentityArc(t *testing.T) {
	f := spectest.NewFake(Spec(), "boreal-beam-340")
	got := map[string]surface.OID{}
	for _, o := range surfaceOf(f).OIDs {
		got[o.OID] = o
	}
	for oid, want := range map[string]any{
		"1.3.6.1.4.1.55520.6.1.0": "12480.5", "1.3.6.1.4.1.55520.6.2.0": "open", "1.3.6.1.4.1.55520.6.3.0": "hdmi1",
		"1.3.6.1.4.1.55520.6.4.0": 36, "1.3.6.1.4.1.55520.5.1.0": "1.12.0", "1.3.6.1.2.1.1.1.0": "Beam 340",
	} {
		if o, ok := got[oid]; !ok || o.Get() != want {
			t.Errorf("OID %s = %v (served %v), want %v", oid, o.Get(), ok, want)
		}
	}
	if len(got) != 9 {
		t.Errorf("serves %d OIDs, want 9", len(got))
	}
}
