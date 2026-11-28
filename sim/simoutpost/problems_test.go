package simoutpost

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/impair"
	"github.com/hyperscaleav/omniglass/sim/simtest/shipped"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// problemRig is an engine of one room's real devices (a display, a wireless
// microphone and a video bar) under a world that scripts three problems on the
// room, one a kind of impairment, and a second room the problems miss.
type problemRig struct {
	e    *sim.Engine
	x    *problemExecutor
	now  time.Time
	logs *bytes.Buffer
}

// rigLoss is the share of packets the rig's lossy problem drops; the NET_ADMIN
// lane sets it to all of them, to see the window on a real network.
var rigLoss = 30.0

var rigOpen = time.Date(2026, 11, 24, 13, 0, 0, 0, time.UTC) // a Tuesday

func newProblemRig(t *testing.T) *problemRig {
	t.Helper()
	cat, err := device.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	mk := func(sys, slot, product string) world.Device {
		r, ok := cat.ForProduct(product)
		if !ok {
			t.Fatalf("the catalog simulates no %s", product)
		}
		return world.Device{Name: sys + "-" + slot + "-1", Slot: slot, Product: product, Profile: r.Definition.ID()}
	}
	w := &world.World{
		Version: world.Version, Scenario: "t", Outpost: "edge-hq", CalendarSeed: 5,
		Problems: []calendar.ProblemSpec{
			{Name: "tuesday-dark", Cadence: "weekly", Weekday: "tuesday", At: "13:00", For: 2 * time.Hour,
				Scope:      calendar.Scope{Standard: "mr86", Role: "main-display"},
				Impairment: calendar.Impairment{Pin: &calendar.Pin{Path: "status.device.power", Value: "off"}}},
			{Name: "dead-battery", Cadence: "daily", At: "13:00", For: time.Hour,
				Scope:      calendar.Scope{Rooms: []string{"hq-mr86-1"}, Role: "wireless-mic"},
				Impairment: calendar.Impairment{Pin: &calendar.Pin{Path: "status.mic.battery_percent", Value: "0"}}},
			{Name: "bar-dark", Cadence: "daily", At: "13:30", For: time.Hour,
				Scope:      calendar.Scope{Site: "hq", Role: "video-bar"},
				Impairment: calendar.Impairment{Unplugged: true}},
			{Name: "mic-lossy", Cadence: "daily", At: "14:00", For: time.Hour,
				Scope:      calendar.Scope{Site: "hq", Role: "wireless-mic"},
				Impairment: calendar.Impairment{LossPct: rigLoss}},
		},
		Sites: []world.Site{{Name: "hq", Archetype: "hq", Rooms: []world.Room{
			{Name: "r1", Building: "tower", Standard: "mr86", System: "hq-mr86-1", Devices: []world.Device{
				mk("hq-mr86-1", "main-display", "boreal-edge-86"), mk("hq-mr86-1", "wireless-mic", "lyra-hx-1"), mk("hq-mr86-1", "video-bar", "kestrel-vroom")}},
			{Name: "r2", Building: "tower", Standard: "mr65", System: "hq-mr65-1", Devices: []world.Device{
				mk("hq-mr65-1", "main-display", "boreal-edge-86")}},
		}}},
	}
	e := sim.NewEngineOver(cat)
	installWorld(e, w)
	var snaps []sim.DeviceSnapshot
	ip := 1
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, d := range r.Devices {
				snaps = append(snaps, sim.DeviceSnapshot{DeviceID: "id-" + d.Name, Hostname: d.Name, LoopbackIP: fmt.Sprintf("127.70.0.%d", ip),
					ProfileName: d.Profile, Product: d.Product, Version: 1, SiteCode: s.Name, Building: r.Building, Room: r.Name})
				ip++
			}
		}
	}
	if err := e.LoadDevices(snaps); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, d := range e.Devices() {
			d.StopBehaviorOverrides()
		}
	})
	rig := &problemRig{e: e, now: rigOpen.Add(-time.Minute), logs: &bytes.Buffer{}}
	rig.x = newProblemExecutor(w, e.Calendar(), e.Arbiter(), func() time.Time { return rig.now }, slog.New(slog.NewTextHandler(rig.logs, nil)))
	if rig.x == nil {
		t.Fatal("a world with problems has no executor")
	}
	return rig
}

func (r *problemRig) dev(name string) *sim.Device {
	for _, d := range r.e.Devices() {
		if d.Hostname == name {
			return d
		}
	}
	panic("no device " + name)
}

func (r *problemRig) at(netOK bool, t time.Time) map[string]string {
	r.now = t
	out := map[string]string{}
	r.x.reconcile(r.e.Devices(), netOK)
	net, errs := r.e.Arbiter().Settle(r.e.Devices())
	if len(errs) != 0 {
		panic(errs)
	}
	for id, imp := range net {
		out[id] = fmt.Sprintf("unplugged=%v loss=%v", imp.Unplugged, imp.LossPct)
	}
	return out
}

// #2769: a pinned-path problem shows on the device that fills its role in the
// rooms of its scope the moment its window opens, and the path is put back the
// moment it closes; nothing else in the fleet is touched.
func TestAPinnedProblemShowsOnItsDeviceAndClearsAfter(t *testing.T) {
	r := newProblemRig(t)
	display, mic, other := r.dev("hq-mr86-1-main-display-1"), r.dev("hq-mr86-1-wireless-mic-1"), r.dev("hq-mr65-1-main-display-1")
	power := func(d *sim.Device) any { return d.ReadPath("status.device.power") }
	if power(display) != "on" {
		t.Fatalf("the display starts %v, want on", power(display))
	}
	r.at(true, rigOpen.Add(-time.Second))
	if power(display) != "on" || len(display.OverrideIDs()) != 0 {
		t.Fatalf("a second before the window the display reads %v, pins %v", power(display), display.OverrideIDs())
	}

	r.at(true, rigOpen)
	if power(display) != "off" {
		t.Errorf("at the window's opening the display reads %v, want off", power(display))
	}
	if got := fmt.Sprint(mic.ReadPath("status.mic.battery_percent")); got != "0" {
		t.Errorf("the microphone's battery reads %v, want it pinned to 0", got)
	}
	if power(other) != "on" || len(other.OverrideIDs()) != 0 {
		t.Errorf("a room outside the scope was touched: %v %v", power(other), other.OverrideIDs())
	}
	// A profile behavior that writes the pinned path does not win against the pin.
	display.TickBehavior("room_meeting")
	mic.TickBehavior("battery_drain")
	if power(display) != "off" || fmt.Sprint(mic.ReadPath("status.mic.battery_percent")) != "0" {
		t.Error("the device's own behavior undid the pin")
	}

	r.at(true, rigOpen.Add(time.Hour)) // dead-battery closes, tuesday-dark still holds
	if power(display) != "off" {
		t.Errorf("an hour in, the two-hour display problem reads %v", power(display))
	}
	if len(mic.OverrideIDs()) != 0 || fmt.Sprint(mic.ReadPath("status.mic.battery_percent")) == "0" {
		t.Errorf("the battery problem closed but the microphone still reads %v with pins %v", mic.ReadPath("status.mic.battery_percent"), mic.OverrideIDs())
	}
	r.at(true, rigOpen.Add(2*time.Hour))
	if power(display) != "on" || len(display.OverrideIDs()) != 0 {
		t.Errorf("after its window the display reads %v with pins %v, want on and none", power(display), display.OverrideIDs())
	}
	// The same window a week on is the same problem again (weekly, Tuesday).
	r.at(true, rigOpen.AddDate(0, 0, 7))
	if power(display) != "off" {
		t.Error("the weekly problem did not return a week later")
	}
	r.at(true, rigOpen.AddDate(0, 0, 8)) // a Wednesday at 13:00
	if power(display) != "on" {
		t.Error("the Tuesday problem held on a Wednesday")
	}
}

// Unplugged and lossy problems are the network's: the executor asks for them
// by component id for the window, nothing before or after, and says so on a
// simulator that cannot apply them rather than failing silently.
func TestUnpluggedAndLossProblemsAskTheNetworkForTheirWindow(t *testing.T) {
	r := newProblemRig(t)
	if got := r.at(true, rigOpen.Add(20*time.Minute)); len(got) != 0 {
		t.Fatalf("before any network window: %v", got)
	}
	got := r.at(true, rigOpen.Add(40*time.Minute))
	if len(got) != 1 || got["id-hq-mr86-1-video-bar-1"] != "unplugged=true loss=0" {
		t.Fatalf("the bar's window: %v, want only the bar unplugged", got)
	}
	got = r.at(true, rigOpen.Add(70*time.Minute))
	if len(got) != 2 || got["id-hq-mr86-1-video-bar-1"] != "unplugged=true loss=0" || got["id-hq-mr86-1-wireless-mic-1"] != "unplugged=false loss=30" {
		t.Fatalf("both windows open: %v", got)
	}
	if got := r.at(true, rigOpen.Add(100*time.Minute)); len(got) != 1 || got["id-hq-mr86-1-wireless-mic-1"] == "" {
		t.Fatalf("after the bar's window closes: %v", got)
	}
	if got := r.at(true, rigOpen.Add(121*time.Minute)); len(got) != 0 {
		t.Fatalf("after every window: %v, want the network clear", got)
	}

	// Without NET_ADMIN: the impairer applies nothing, the pins still apply,
	// and the refusal is a clear log line, once per window.
	r = newProblemRig(t)
	ap := &fakeApplier{}
	r.e.Arbiter().SetNet(newImpairer(ap, false))
	for _, at := range []time.Duration{40 * time.Minute, 45 * time.Minute} {
		r.at(false, rigOpen.Add(at))
		if len(ap.applied) != 0 {
			t.Fatalf("a simulator without NET_ADMIN applied %v", ap.applied)
		}
	}
	if n := strings.Count(r.logs.String(), "needs the network and is not applied"); n != 1 || !strings.Contains(r.logs.String(), "bar-dark") || !strings.Contains(r.logs.String(), "NET_ADMIN") {
		t.Fatalf("the refusal was logged %d times: %q", n, r.logs.String())
	}
	if r.dev("hq-mr86-1-main-display-1").ReadPath("status.device.power") != "off" {
		t.Error("a pin was refused along with the network")
	}
}

// A world with no scripted problems has no executor: a device is untouched.
func TestAWorldWithNoProblemsHasNoExecutor(t *testing.T) {
	w := worldFixture()
	e := shipped.Engine(t)
	installWorld(e, w)
	if newProblemExecutor(w, e.Calendar(), e.Arbiter(), nil, slog.Default()) != nil || newProblemExecutor(nil, nil, nil, nil, slog.Default()) != nil {
		t.Fatal("an executor exists for a world with no problems")
	}
}

// #2799: the operator's impairment and the scripted problems are separate
// sources of one arbiter: a problem's window opening and closing never clears
// what the operator set, and the operator's repair never clears a problem.
func TestAProblemWindowAndAnOperatorsImpairmentDoNotClearEachOther(t *testing.T) {
	r := newProblemRig(t)
	arb := r.e.Arbiter()
	const mic = "id-hq-mr86-1-wireless-mic-1"
	arb.Set(operatorSource, map[string]sim.Want{mic: {Impairment: impair.Impairment{Unplugged: true}}})

	got := r.at(true, rigOpen.Add(70*time.Minute)) // mic-lossy is open
	if got[mic] != "unplugged=true loss=30" {
		t.Fatalf("operator unplug and scripted loss together: %q", got[mic])
	}
	got = r.at(true, rigOpen.Add(121*time.Minute)) // every window closed
	if got[mic] != "unplugged=true loss=0" {
		t.Fatalf("the problem closed and left %q, want the operator's unplug alone", got[mic])
	}
	r.at(true, rigOpen.Add(24*time.Hour+70*time.Minute)) // the next day's window
	arb.Set(operatorSource, nil)                         // the operator repairs it
	got = r.at(true, rigOpen.Add(24*time.Hour+71*time.Minute))
	if got[mic] != "unplugged=false loss=30" {
		t.Fatalf("the operator's repair left %q, want the scripted loss alone", got[mic])
	}
}
