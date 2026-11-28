//go:build linux

package sim

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/nvp"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// familyEngine serves the test family beside the shipped families.
func familyEngine(t *testing.T, fam spec.Spec) *Engine {
	t.Helper()
	e := NewEngineOver(shippedCatalog(t, fam))
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	return e
}

func familySnap(id, ip, product string) DeviceSnapshot {
	return DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: ip, ProfileName: spectest.Name, Product: product, Version: 1}
}

func deviceNamed(t *testing.T, e *Engine, id string) *Device {
	t.Helper()
	for _, d := range e.Devices() {
		if d.DeviceID == id {
			return d
		}
	}
	t.Fatalf("engine serves no device %s", id)
	return nil
}

// A Go family serves a product end to end beside the shipped families (#2829): a
// real HTTP request reaches the family's Go handler and gets its typed
// response, for each row of the model table, while a YAML device on the same
// engine answers as it always did.
func TestGoFamilyServesItsModelTableBesideYAMLProfiles(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	snaps := []DeviceSnapshot{{DeviceID: "dsp-a", Hostname: "dsp-a", LoopbackIP: "127.88.20.1", ProfileName: "generic-device", Version: 1}}
	for i, r := range spectest.Rows {
		snaps = append(snaps, familySnap("tf-"+r.Product, "127.88.20."+string(rune('2'+i)), r.Product))
	}
	if err := e.Sync(snaps); err != nil {
		t.Fatal(err)
	}

	for _, r := range spectest.Rows {
		d := deviceNamed(t, e, "tf-"+r.Product)
		code, body := dspDo(t, "GET", "http://"+faceAddr(t, d, "api")+"/api/info", "")
		if code != 200 {
			t.Fatalf("%s: GET /api/info = %d %s", r.Product, code, body)
		}
		var got spectest.Info
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("%s: response is not the typed Info: %v: %s", r.Product, err, body)
		}
		want := spectest.Info{Product: r.Product, Diagonal: r.Diagonal, PowerW: r.PowerW, Power: "on", Host: "tf-" + r.Product, Firmware: "1.2.3"}
		if got != want {
			t.Errorf("%s: info = %+v, want %+v", r.Product, got, want)
		}
		// The per-model state overlay reached the device's state.
		if v := d.ReadPath("configuration.device.diagonal"); v != r.Diagonal {
			t.Errorf("%s: diagonal in state = %v, want %d", r.Product, v, r.Diagonal)
		}
	}

	yaml := deviceNamed(t, e, "dsp-a")
	if got := beamSNMP(t, yaml, "1.3.6.1.2.1.1.1.0")["1.3.6.1.2.1.1.1.0"]; got != "Generic SNMP Device" {
		t.Fatalf("the YAML device did not answer beside the family: sysDescr = %q", got)
	}
}

// A handler writes state the way a route's mutation does: the version
// advances, the diff reaches the publisher, and the next read answers the new
// value. A request the handler refuses changes nothing.
func TestGoFamilyHandlerWritesStateAndPublishesTheDiff(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.21.1", "tf-display-55")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	var diffs []StateChange
	d.DiffPublisher = func(c StateChange) { diffs = append(diffs, c) }
	url := "http://" + faceAddr(t, d, "api")

	if code, body := dspDo(t, "POST", url+"/api/power", `{"power":"standby"}`); code != 400 {
		t.Fatalf("a bad power = %d %s, want 400", code, body)
	}
	if len(diffs) != 0 || d.ReadPath("status.device.power") != "on" {
		t.Fatalf("a refused request changed state: diffs=%v power=%v", diffs, d.ReadPath("status.device.power"))
	}
	if code, body := dspDo(t, "POST", url+"/api/power", `{"power":"off"}`); code != 200 || !strings.Contains(body, `"off"`) {
		t.Fatalf("POST power = %d %s", code, body)
	}
	if len(diffs) != 1 || diffs[0].Diffs["status.device.power"] != "off" || diffs[0].Version != 2 {
		t.Fatalf("diffs = %+v, want one change of status.device.power to off at version 2", diffs)
	}
	_, body := dspDo(t, "GET", url+"/api/info", "")
	if !strings.Contains(body, `"power":"off"`) {
		t.Fatalf("info after the write = %s", body)
	}
}

// The family's line commands answer over a real Telnet face, and RunCommand,
// the one entry every line protocol uses, runs the same Go handler.
func TestGoFamilyAnswersLineCommandsOverTelnet(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.22.1", "tf-display-65")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")

	r, ok, err := d.RunCommand("status")
	if err != nil || !ok || r.Response != "power=on\n" {
		t.Fatalf("RunCommand = %q ok=%v err=%v", r.Response, ok, err)
	}
	if _, ok, _ := d.RunCommand("nope"); ok {
		t.Fatal("an unregistered command matched")
	}

	conn, err := net.DialTimeout("tcp", faceAddr(t, d, "console"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(telnetReader{r: conn})
	for _, step := range []struct{ wait, send string }{{"login:", "op"}, {"Password:", "x"}, {"tf-1>", "status"}} {
		if err := readUntil(rd, step.wait); err != nil {
			t.Fatalf("waiting for %q: %v", step.wait, err)
		}
		if _, err := conn.Write([]byte(step.send + "\r\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := readUntil(rd, "power=on"); err != nil {
		t.Fatalf("the telnet face did not answer the family's command: %v", err)
	}
}

// A family's animator runs while its device serves and stops with it, the
// seam the behavior primitives plug into.
func TestGoFamilyAnimatorRunsWhileTheDeviceServes(t *testing.T) {
	var started, stopped atomic.Int32
	var handle atomic.Value
	fam := spectest.Spec()
	fam.Animate = func(d spec.Device) func() {
		started.Add(1)
		handle.Store(d)
		return func() { stopped.Add(1) }
	}
	e := familyEngine(t, fam)
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.23.1", "tf-display-55")}); err != nil {
		t.Fatal(err)
	}
	if started.Load() != 1 || stopped.Load() != 0 {
		t.Fatalf("started=%d stopped=%d, want 1 and 0 while serving", started.Load(), stopped.Load())
	}
	sd := handle.Load().(spec.Device)
	if sd.DeviceID() != "tf-1" || sd.Model().Product != "tf-display-55" {
		t.Fatalf("the animator's device = %s / %+v", sd.DeviceID(), sd.Model())
	}
	if err := sd.Write("status.device.power", "off"); err != nil {
		t.Fatal(err)
	}
	if got := deviceNamed(t, e, "tf-1").ReadPath("status.device.power"); got != "off" {
		t.Fatalf("the animator's write left power = %v", got)
	}
	if err := e.Sync(nil); err != nil {
		t.Fatal(err)
	}
	if stopped.Load() != 1 {
		t.Fatalf("stopped = %d after the device left, want 1", stopped.Load())
	}
}

// A snapshot of a family's own name (no product) serves the family's base
// state, as a profile's own name serves the profile's.
func TestGoFamilyOwnNameServesTheBaseState(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.24.1", "")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	_, body := dspDo(t, "GET", "http://"+faceAddr(t, d, "api")+"/api/info", "")
	var got spectest.Info
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Product != "" || got.Diagonal != 0 || got.Power != "on" {
		t.Fatalf("info = %+v (%v), want the base state with no model row", got, err)
	}
}

// The engine builds a family's product with the faces the family declares at
// their declared ports, the state with the product's overlay merged, and the
// product named on the device (#2829).
func TestEngineBuildsAFamilyProduct(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	var snaps []DeviceSnapshot
	products := []struct {
		name     string
		diagonal string
	}{{"tf-display-55", "55"}, {"tf-display-65", "65"}}
	for i, p := range products {
		snaps = append(snaps, familySnap("go-"+p.name, "127.88.25."+string(rune('1'+i)), p.name))
	}
	if err := e.Sync(snaps); err != nil {
		t.Fatal(err)
	}
	for _, p := range products {
		g := deviceNamed(t, e, "go-"+p.name)
		for _, face := range []string{"api", "console"} {
			if f := faceOf(t, g, face); f.Declared == 0 {
				t.Errorf("%s: face %s = %+v, want it at its declared port", p.name, face, f)
			}
		}
		if got := fmt.Sprint(g.ReadPath("configuration.device.diagonal")); got != p.diagonal {
			t.Errorf("%s: diagonal = %s, want the row's %s", p.name, got, p.diagonal)
		}
		for _, path := range []string{"status.device.power", "configuration.firmware.version"} {
			if g.ReadPath(path) == nil {
				t.Errorf("%s: %s is unset", p.name, path)
			}
		}
		if g.Product != p.name {
			t.Errorf("product = %s, want %s", g.Product, p.name)
		}
	}
}

// A snapshot naming a family the catalog lacks is skipped, as for a profile.
func TestEngineSkipsASnapshotOfAnUnknownDefinition(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	snap := familySnap("x", "127.88.26.1", "tf-display-55")
	snap.ProfileName = "no-such-family"
	if err := e.Sync([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	if n := len(e.Devices()); n != 0 {
		t.Fatalf("serves %d devices, want none", n)
	}
}

// A family serves a shared protocol surface (sim/surface/nvp, #2831) through
// its Spec: a command reads the device's state, a SET reports the change and
// the engine applies it as it applies a YAML command's mutation (version, diff
// stream), and the surface's subjects join the panel face's session config.
func TestGoFamilyServesASharedSurface(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.27.1", "tf-display-55")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	var diffs []StateChange
	d.DiffPublisher = func(c StateChange) { diffs = append(diffs, c) }

	if r, ok, err := d.RunCommand("GET INPUT"); err != nil || !ok || r.Response != "INPUT hdmi1" {
		t.Fatalf("GET INPUT = %q ok=%v err=%v", r.Response, ok, err)
	}
	if r, ok, err := d.RunCommand("SET INPUT dp1"); err != nil || !ok || r.Response != "INPUT dp1" {
		t.Fatalf("SET INPUT = %q ok=%v err=%v", r.Response, ok, err)
	}
	if d.ReadPath("status.video.input") != "dp1" || len(diffs) != 1 || diffs[0].Diffs["status.video.input"] != "dp1" {
		t.Fatalf("state %v diffs %+v: the reported change was not applied", d.ReadPath("status.video.input"), diffs)
	}
	if _, ok, _ := d.RunCommand("SET INPUT nonsense"); ok {
		t.Fatal("a value the surface does not allow was served")
	}
	// The family's own command still answers beside the surface's.
	if r, ok, _ := d.RunCommand("status"); !ok || r.Response != "power=on\n" {
		t.Fatalf("status = %q ok=%v", r.Response, ok)
	}
	reg := d.TCPSession()
	if reg == nil || len(reg.cfg.Subjects) != len(nvp.Subjects()) {
		t.Fatalf("the panel's session config carries %v subjects, want the surface's %d", reg, len(nvp.Subjects()))
	}
}

// A family's typed ticks run on their interval (once at start for OnStart),
// read the room and call sim/behave primitives, and their writes go through the
// path a YAML behavior's mutations do: only what changed is written, the diff
// stream sees it with the behavior as its cause, and a stopped device ticks no
// more. A room with no calendar leaves the power where it was, so
// PowerFollowsRoom writes nothing.
func TestGoFamilyTicksWriteOnlyWhatChanged(t *testing.T) {
	var runs atomic.Int64
	var sawRoom atomic.Bool
	fam := spectest.Spec()
	fam.Ticks = []spec.Tick{
		{Name: "count", Interval: 20 * time.Millisecond, OnStart: true, Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			n := runs.Add(1)
			if c.Room.Phase == calendar.PhaseIdle && c.Rand != nil && !c.Now.IsZero() {
				sawRoom.Store(true)
			}
			return []surface.Change{{Path: "status.device.ticks", Value: n}}
		}},
		{Name: "power", Interval: 20 * time.Millisecond, Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			prev, _ := d.Read("status.device.power").(string)
			next, _ := behave.PowerFollowsRoom(behave.PowerState{Power: prev}, c.Room, c.Now, behave.DefaultPower)
			return []surface.Change{{Path: "status.device.power", Value: next.Power}}
		}},
	}
	e := familyEngine(t, fam)
	var mu sync.Mutex
	var causes []string
	snap := familySnap("tf-1", "127.88.28.1", "tf-display-55")
	// The publisher is set before the device serves, so the OnStart tick is seen.
	e.SetDiffPublisher(func(c StateChange) {
		mu.Lock()
		causes = append(causes, c.Cause)
		mu.Unlock()
	})
	if err := e.Sync([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	if runs.Load() < 1 || d.ReadPath("status.device.ticks") == nil {
		t.Fatal("the OnStart tick did not run before the device served")
	}
	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() < 4 || !sawRoom.Load() {
		t.Fatalf("runs=%d sawRoom=%v", runs.Load(), sawRoom.Load())
	}
	mu.Lock()
	for _, c := range causes {
		if c != "behavior:count" {
			t.Errorf("a write with cause %q: the power tick changed nothing and must write nothing", c)
		}
	}
	mu.Unlock()
	if d.ReadPath("status.device.power") != "on" {
		t.Fatalf("power = %v, want it held with no calendar", d.ReadPath("status.device.power"))
	}
	d.StopBehaviors()
	n := runs.Load()
	time.Sleep(100 * time.Millisecond)
	if runs.Load() != n {
		t.Fatal("a stopped device kept ticking")
	}
}

// A handler's WriteMany is one change: one version, one diff carrying every
// path, and all of it or none of it, so a refused write leaves state and the
// diff stream as they were (#2832).
func TestGoFamilyWriteManyIsOneChangeAndAllOrNothing(t *testing.T) {
	var handle atomic.Value
	fam := spectest.Spec()
	fam.Animate = func(d spec.Device) func() { handle.Store(d); return func() {} }
	e := familyEngine(t, fam)
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.31.1", "tf-display-55")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	var diffs []StateChange
	d.DiffPublisher = func(c StateChange) { diffs = append(diffs, c) }
	sd := handle.Load().(spec.Device)

	if err := sd.WriteMany(surface.Change{Path: "status.device.power", Value: "off"}, surface.Change{Path: "status.video.input", Value: "dp1"}); err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || len(diffs[0].Diffs) != 2 || diffs[0].Version != 2 {
		t.Fatalf("diffs = %+v, want one change of two paths at version 2", diffs)
	}
	// A path under a value that is not a map cannot be set: nothing is written.
	err := sd.WriteMany(surface.Change{Path: "status.video.input", Value: "hdmi2"}, surface.Change{Path: "status.device.power.x", Value: 1})
	if err == nil {
		t.Fatal("a write under a string was accepted")
	}
	if d.ReadPath("status.video.input") != "dp1" || len(diffs) != 1 || d.Version != 2 {
		t.Fatalf("a refused change left input %v, %d diffs, version %d; want dp1, 1, 2", d.ReadPath("status.video.input"), len(diffs), d.Version)
	}
	if got := sd.Now(); !got.Equal(d.clock()) && got.Sub(d.clock()) > time.Second {
		t.Fatalf("the handle's clock is %v, the device's %v", got, d.clock())
	}
	if sd.Default("status.device.power") != "on" || sd.Default("status.video.input") != "hdmi1" {
		t.Fatalf("defaults = %v, %v; a reset restores on and hdmi1", sd.Default("status.device.power"), sd.Default("status.video.input"))
	}
}

// A family's ticks are fired by name, listed beside YAML behaviors, and read
// the building's hours the way a template's buildingHours did.
func TestGoFamilyTicksRunByNameAndReadTheBuildingsHours(t *testing.T) {
	var got atomic.Value
	fam := spectest.Spec()
	fam.Ticks = []spec.Tick{{Name: "hours_probe", Interval: time.Hour, Run: func(d spec.Device, c spec.TickContext) []surface.Change {
		got.Store(c.Hours)
		return nil
	}}}
	e := familyEngine(t, fam)
	k := calendar.Knobs{Hours: calendar.Hours{Start: 8, End: 18}}
	e.SetCalendar(calendar.New(1, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	snap := familySnap("tf-1", "127.88.32.1", "tf-display-55")
	snap.SiteCode, snap.Building, snap.Room = "hq", "tower", "r101"
	if err := e.Sync([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	d.StopBehaviors()
	if !slices.Contains(d.Behaviors(), "hours_probe") {
		t.Fatalf("behaviors = %v, want the family's tick listed", d.Behaviors())
	}
	for _, c := range []struct {
		h    int
		open bool
	}{{7, false}, {9, true}, {18, false}} {
		at := time.Date(2026, 11, 3, c.h, 30, 0, 0, time.UTC)
		d.SetClock(func() time.Time { return at })
		if !d.TickBehavior("hours_probe") {
			t.Fatal("the tick was not found by name")
		}
		if h := got.Load().(behave.HoursReading); !h.Known || h.Open != c.open {
			t.Errorf("%02d:30: hours = %+v, want known and open=%v", c.h, h, c.open)
		}
	}
	if d.TickBehavior("nope") {
		t.Error("an unknown behavior was fired")
	}
}

// The quick sets a family's routes declare reach the simulator as a profile's
// mutations do: a path driven off its default is a fault the console shows.
func TestGoFamilyRouteSetsReportFaultsAndListOnTheSurface(t *testing.T) {
	fam := spectest.Spec()
	fam.HTTP = append([]spec.HTTPRoute(nil), fam.HTTP...)
	for i := range fam.HTTP {
		if fam.HTTP[i].Match == "POST /api/power" {
			fam.HTTP[i].Sets = []wire.Set{{Path: "status.device.power", Field: "power", Values: []string{"on", "off"}}}
		}
	}
	e := familyEngine(t, fam)
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.33.1", "tf-display-55")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	if f := d.Faults(); len(f) != 0 {
		t.Fatalf("a healthy device has faults %+v", f)
	}
	if code, _ := dspDo(t, "POST", "http://"+faceAddr(t, d, "api")+"/api/power", `{"power":"off"}`); code != 200 {
		t.Fatalf("POST power = %d", code)
	}
	if f := d.Faults(); len(f) != 1 || f[0].Path != "status.device.power" || f[0].Value != "off" || f[0].Default != "on" {
		t.Fatalf("faults = %+v, want the power held off its default", f)
	}
	def, _ := e.cat.Lookup(spectest.Name)
	var listed []wire.Set
	for _, s := range def.Surfaces() {
		for _, r := range s.Routes {
			listed = append(listed, r.Sets...)
		}
	}
	if len(listed) != 1 || listed[0].Field != "power" {
		t.Fatalf("the surface lists sets %+v", listed)
	}
}

// A family's SNMP scalars answer an operator's fire from the console, and the
// surface lists each with the state path it reads, as a profile's OID map is.
func TestGoFamilyOIDsAnswerTheConsoleAndAreListed(t *testing.T) {
	fam := spectest.Spec()
	fam.Interfaces = append(append([]wire.Interface(nil), fam.Interfaces...), wire.Interface{Name: "snmp", Protocol: "snmp", Port: 161})
	fam.Surface = func(d spec.Device) spec.Surface {
		return spec.Surface{OIDs: []surface.OID{
			surface.Text("1.3.6.1.4.1.9.1.0", func() string { return spec.String(d, "status.device.power") }).At("status.device.power"),
			surface.Integer("1.3.6.1.4.1.9.2.0", func() int { return spec.Int(d, "configuration.device.diagonal") }).At("configuration.device.diagonal"),
		}}
	}
	e := familyEngine(t, fam)
	if err := e.Sync([]DeviceSnapshot{familySnap("tf-1", "127.88.34.1", "tf-display-65")}); err != nil {
		t.Fatal(err)
	}
	d := deviceNamed(t, e, "tf-1")
	for oid, want := range map[string]string{"1.3.6.1.4.1.9.1.0": "on", ".1.3.6.1.4.1.9.2.0": "65"} {
		if res := d.Operate(Operation{Kind: "snmp", OID: oid}); res.Error != "" || res.Body != want {
			t.Errorf("fire %s = %+v, want %q", oid, res, want)
		}
	}
	if res := d.Operate(Operation{Kind: "snmp", OID: "1.3.6.1.4.1.9.9.0"}); res.Error == "" {
		t.Error("an OID nothing serves answered")
	}
	def, _ := e.cat.Lookup(spectest.Name)
	var listed map[string]string
	for _, s := range def.Surfaces() {
		if s.Protocol == "snmp" {
			listed = s.OIDs
		}
	}
	if listed["status.device.power"] != "1.3.6.1.4.1.9.1.0" || len(listed) != 2 {
		t.Fatalf("the surface lists OIDs %v", listed)
	}
}
