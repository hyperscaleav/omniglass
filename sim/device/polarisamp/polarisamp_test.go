package polarisamp

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func route(t *testing.T, match string) spec.HTTPHandler {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r.Handle
		}
	}
	t.Fatalf("no route %s", match)
	return nil
}

func tick(t *testing.T, name string) spec.Tick {
	t.Helper()
	for _, tk := range Spec().Ticks {
		if tk.Name == name {
			return tk
		}
	}
	t.Fatalf("no tick %s", name)
	return spec.Tick{}
}

// The model table is the line: a product the catalog resolves to the family,
// with a channel for each zone it drives.
func TestTheRowIsAProductWithAChannelPerZone(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	res, ok := cat.ForProduct("polaris-amp-4100")
	if !ok || res.Definition.ID() != Name || res.Identity.Model != "Amp 4-100" || res.Identity.DeviceType != "amplifier" {
		t.Fatalf("polaris-amp-4100 resolves to %+v (%v)", res, ok)
	}
	if !strings.Contains(res.Identity.Summary, "4-channel") {
		t.Errorf("summary %q does not say its channels", res.Identity.Summary)
	}
	f := spectest.NewFake(Spec(), "polaris-amp-4100")
	for i, z := range Rows[0].Zones {
		if f.Read(channelPath(i, "name")) != z.Name || f.Read(channelPath(i, "output_db")) != z.OutputDB {
			t.Errorf("channel %d reads %v at %v, its zone says %s at %d", i, f.Read(channelPath(i, "name")), f.Read(channelPath(i, "output_db")), z.Name, z.OutputDB)
		}
	}
}

// The channel table is read as the agent always served it: a level, a clip flag
// and a load for each of the four channels, at the column and row an operator's
// MIB names, beside the scalars.
func TestSNMPServesAFourChannelTable(t *testing.T) {
	f := spectest.NewFake(Spec(), "polaris-amp-4100")
	got := map[string]surface.OID{}
	for _, o := range surfaceOf(f).OIDs {
		got[o.OID] = o
	}
	for col, field := range map[int]string{colOutput: "output_db", colClip: "clip", colLoad: "load_ohms"} {
		for ch := 1; ch <= 4; ch++ {
			oid := "1.3.6.1.4.1.55540.3.10.1." + string(rune('0'+col)) + "." + string(rune('0'+ch))
			o, ok := got[oid]
			if !ok || o.Path != channelPath(ch-1, field) {
				t.Errorf("%s serves %+v (%v), want a read of %s", oid, o, ok, channelPath(ch-1, field))
			}
		}
	}
	_ = f.Write(pathTemperature, 48.9)
	_ = f.Write(pathFan, int64(3100))
	if o := got["1.3.6.1.4.1.55540.3.3.0"]; o.Kind != surface.KindInteger || surfaceOf(f).OIDs[3].Get() == nil || o.Get() != 48 {
		t.Errorf("a fractional temperature reads %v as %v, want the whole number 48", o.Get(), o.Kind)
	}
	if o := got["1.3.6.1.4.1.55540.3.4.0"]; o.Kind != surface.KindCounter32 || o.Get() != uint(3100) {
		t.Errorf("the fan reads %v as kind %v, want the counter 3100", o.Get(), o.Kind)
	}
}

// Protect latches until an operator resets it, and a reboot clears it with the
// uptime and the heatsink's drift; it leaves the power state alone.
func TestProtectResetAndRebootClearWhatTheyName(t *testing.T) {
	f := spectest.NewFake(Spec(), "polaris-amp-4100")
	_ = f.Write(pathProtect, "overheat")
	_ = f.Write(pathTemperature, 51)
	_ = f.Write(pathPower, "standby")
	route(t, "POST /api/protect/reset")(f, spec.HTTPRequest{})
	if f.Read(pathProtect) != "normal" || f.Read(pathTemperature) != 51 {
		t.Errorf("a protect reset left protect %v temperature %v", f.Read(pathProtect), f.Read(pathTemperature))
	}
	_ = f.Write(pathProtect, "protect")
	route(t, "POST /api/admin/reboot")(f, spec.HTTPRequest{})
	if f.Read(pathProtect) != "normal" || f.Read(pathTemperature) != 41 || f.Read(pathBootEpoch) != f.Clock.Unix() || f.Read(pathPower) != "standby" {
		t.Errorf("after a reboot: protect %v temperature %v epoch %v power %v", f.Read(pathProtect), f.Read(pathTemperature), f.Read(pathBootEpoch), f.Read(pathPower))
	}
}

// The outputs carry the program while someone is in the room and the amplifier
// is on, and fall to silence when it is in standby however full the room is.
func TestOutputsCarryTheProgramOnlyWhileTheAmplifierIsOn(t *testing.T) {
	f := spectest.NewFake(Spec(), "polaris-amp-4100")
	full := spec.TickContext{Room: calendar.RoomState{HasCalendar: true, Attendees: 5}, Now: f.Clock, Rand: behave.NewSource("amp")}
	drive := tick(t, "output_drift")
	run := func() {
		for _, c := range drive.Run(f, full) {
			_ = f.Write(c.Path, c.Value)
		}
	}
	run()
	if f.Read(pathSignal) != true {
		t.Fatal("the amplifier is on in a full room and carries no signal")
	}
	for i := range Rows[0].Zones {
		if v := f.Read(channelPath(i, "output_db")).(float64); v < -40 || v > -6 {
			t.Errorf("channel %d reads %v in a live room, outside -40..-6", i, v)
		}
	}
	_ = f.Write(pathPower, "standby")
	run()
	if f.Read(pathSignal) != false {
		t.Error("an amplifier in standby carries a signal")
	}
	if v := f.Read(channelPath(0, "output_db")).(float64); v > -88 || v < -92 {
		t.Errorf("a standby channel reads %v, want the floor near -90", v)
	}
}
