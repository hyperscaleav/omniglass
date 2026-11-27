//go:build linux

package sim

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
	"github.com/hyperscaleav/omniglass/sim/surface/nvp"
)

// familyEngine serves the test family beside the shipped YAML profiles.
func familyEngine(t *testing.T, fam spec.Spec) *Engine {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := spec.NewCatalog(store, fam)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineOver(cat, profiles.Templates())
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

// A Go family serves a product end to end beside a YAML profile (#2829): a
// real HTTP request reaches the family's Go handler and gets its typed
// response, for each row of the model table, while a YAML device on the same
// engine answers as it always did.
func TestGoFamilyServesItsModelTableBesideYAMLProfiles(t *testing.T) {
	e := familyEngine(t, spectest.Spec())
	snaps := []DeviceSnapshot{dspSnapshot("dsp-a", "127.88.20.1")}
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

	dsp := deviceNamed(t, e, "dsp-a")
	if code, body := dspDo(t, "GET", dspURL(t, dsp, "/api/status"), ""); code != 200 {
		t.Fatalf("the YAML device did not answer beside the family: %d %s", code, body)
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

// The engine builds a family's product exactly as it builds the YAML twin's
// (#2829): the same faces at the same declared ports, the same state with the
// product's overlay merged, the same identity in its enriched state.
func TestEngineBuildsAFamilyProductAsItBuildsItsYAMLTwin(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineOver(cat, nil)
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	var snaps []DeviceSnapshot
	for i, p := range spectest.Pairs {
		ip := func(k int) string { return "127.88.25." + string(rune('1'+k)) }
		snaps = append(snaps,
			DeviceSnapshot{DeviceID: "go-" + p.Go, Hostname: "go-" + p.Go, LoopbackIP: ip(2 * i), ProfileName: spectest.Name, Product: p.Go, Version: 1},
			DeviceSnapshot{DeviceID: "yaml-" + p.YAML, Hostname: "yaml-" + p.YAML, LoopbackIP: ip(2*i + 1), ProfileName: spectest.TwinName, Product: p.YAML, Version: 1})
	}
	if err := e.Sync(snaps); err != nil {
		t.Fatal(err)
	}
	for _, p := range spectest.Pairs {
		g, y := deviceNamed(t, e, "go-"+p.Go), deviceNamed(t, e, "yaml-"+p.YAML)
		for _, face := range []string{"api", "console"} {
			gf, yf := faceOf(t, g, face), faceOf(t, y, face)
			if gf.Protocol != yf.Protocol || gf.Declared != yf.Declared || gf.TLS != yf.TLS {
				t.Errorf("%s: face %s = %+v, twin %+v", p.Go, face, gf, yf)
			}
		}
		for _, path := range []string{"status.device.power", "configuration.firmware.version", "configuration.device.diagonal"} {
			if gv, yv := g.ReadPath(path), y.ReadPath(path); gv != yv || gv == nil {
				t.Errorf("%s: %s = %v, twin %v", p.Go, path, gv, yv)
			}
		}
		if g.Product != p.Go || y.Product != p.YAML {
			t.Errorf("products = %s, %s", g.Product, y.Product)
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
