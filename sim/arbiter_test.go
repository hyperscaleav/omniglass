package sim

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/impair"
	"github.com/hyperscaleav/omniglass/sim/world"
)

type fakeSink struct {
	last   map[string]impair.Impairment
	served map[string]netip.Addr
	calls  int
}

func (f *fakeSink) Reconcile(served map[string]netip.Addr, wants map[string]impair.Impairment) {
	f.calls++
	f.served, f.last = served, wants
}

type arbRig struct {
	a    *Arbiter
	d    *Device
	sink *fakeSink
	now  time.Time
}

func newArbRig(t *testing.T) *arbRig {
	t.Helper()
	r := &arbRig{sink: &fakeSink{}, now: time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)}
	r.a = NewArbiter(func() time.Time { return r.now })
	r.a.SetNet(r.sink)
	r.d = newTestDevice(t, "d-1", map[string]any{"power": "on", "level": int64(7)})
	t.Cleanup(r.d.StopBehaviorOverrides)
	return r
}

func (r *arbRig) settle(t *testing.T) map[string]impair.Impairment {
	t.Helper()
	net, errs := r.a.Settle([]*Device{r.d})
	if len(errs) != 0 {
		t.Fatalf("settle: %v", errs)
	}
	return net
}

func (r *arbRig) read(path string) string { return fmt.Sprint(r.d.ReadPath(path)) }

// #2799: an operator's unplug and a scripted pin on one device both hold, and
// releasing either leaves the other.
func TestAnOperatorUnplugAndAScriptedPinBothHoldAndReleaseIndependently(t *testing.T) {
	r := newArbRig(t)
	r.a.Set("operator", map[string]Want{"d-1": {Impairment: impair.Impairment{Unplugged: true}}})
	r.a.Set("problem:dark", map[string]Want{"d-1": {Pins: []Pin{{Path: "power", Value: "off"}}}})
	net := r.settle(t)
	if !net["d-1"].Unplugged || r.read("power") != "off" {
		t.Fatalf("both wants should hold: net %+v power %s", net, r.read("power"))
	}

	r.a.Release("problem:dark") // the scripted problem ends
	net = r.settle(t)
	if !net["d-1"].Unplugged {
		t.Error("clearing the problem plugged the operator's unplugged device back in")
	}
	if r.read("power") != "on" {
		t.Errorf("the pin did not release: power %s", r.read("power"))
	}

	r.a.Set("problem:dark", map[string]Want{"d-1": {Pins: []Pin{{Path: "power", Value: "off"}}}})
	r.a.Set("operator", nil) // the operator repairs it
	net = r.settle(t)
	if net["d-1"].Unplugged || len(net) != 0 {
		t.Errorf("the operator's repair left the network impaired: %+v", net)
	}
	if r.read("power") != "off" {
		t.Error("the operator's repair released the scripted pin")
	}
	r.a.Release("problem:dark")
	r.settle(t)
	if r.read("power") != "on" || len(r.d.OverrideIDs()) != 0 {
		t.Errorf("everything released but power %s, pins %v", r.read("power"), r.d.OverrideIDs())
	}
}

// #2799: two pins on one path resolve to the newest and restore in order as
// winners release, and the path goes back to what it read before any pin.
func TestTwoPinsOnOnePathNewestWinsAndRestoreInOrder(t *testing.T) {
	r := newArbRig(t)
	pin := func(v string) map[string]Want {
		return map[string]Want{"d-1": {Pins: []Pin{{Path: "level", Value: v}}}}
	}
	r.a.Set("a", pin("1"))
	r.settle(t)
	r.a.Set("b", pin("2"))
	r.settle(t)
	r.a.Set("c", pin("3"))
	r.settle(t)
	if got := r.read("level"); got != "3" {
		t.Fatalf("three pins: level %s, want the newest (3)", got)
	}
	// Re-registering an older source unchanged does not make it newer.
	r.a.Set("a", pin("1"))
	r.settle(t)
	if got := r.read("level"); got != "3" {
		t.Fatalf("re-asserting the oldest pin took the path: %s", got)
	}
	r.a.Release("c")
	r.settle(t)
	if got := r.read("level"); got != "2" {
		t.Errorf("the newest released: level %s, want 2", got)
	}
	r.a.Release("a") // a loser leaving changes nothing
	r.settle(t)
	if got := r.read("level"); got != "2" {
		t.Errorf("a loser released: level %s, want 2", got)
	}
	r.a.Release("b")
	r.settle(t)
	if got := r.read("level"); got != "7" {
		t.Errorf("all released: level %s, want the original 7", got)
	}
	if ids := r.d.OverrideIDs(); len(ids) != 0 {
		t.Errorf("overrides left behind: %v", ids)
	}
}

// A pin that was released and asked for again is new again.
func TestAPinAskedForAgainIsTheNewest(t *testing.T) {
	r := newArbRig(t)
	pin := func(v string) map[string]Want {
		return map[string]Want{"d-1": {Pins: []Pin{{Path: "level", Value: v}}}}
	}
	r.a.Set("a", pin("1"))
	r.a.Set("b", pin("2"))
	r.settle(t)
	r.a.Release("a")
	r.a.Set("a", pin("1"))
	r.settle(t)
	if got := r.read("level"); got != "1" {
		t.Errorf("level %s, want the re-asked pin (1) to be the newest", got)
	}
}

func TestNetworkWantsResolveByTheirKind(t *testing.T) {
	r := newArbRig(t)
	r.a.Set("operator", map[string]Want{"d-1": {Impairment: impair.Impairment{LatencyMs: 50, JitterMs: 5, LossPct: 10, RateKbit: 900}}})
	r.a.Set("problem:lossy", map[string]Want{"d-1": {Impairment: impair.Impairment{LatencyMs: 20, LossPct: 30, RateKbit: 500}}})
	net := r.settle(t)
	want := impair.Impairment{LatencyMs: 50, JitterMs: 5, LossPct: 30, RateKbit: 500}
	if net["d-1"] != want {
		t.Errorf("resolved %+v, want %+v (highest loss, latency and jitter; tightest rate)", net["d-1"], want)
	}
	if r.sink.last["d-1"] != want || r.sink.served["d-1"].IsValid() == false {
		t.Errorf("the sink was handed %+v served %v", r.sink.last, r.sink.served)
	}
	if got := r.a.Sources(); len(got) != 2 || got[0] != "operator" || got[1] != "problem:lossy" {
		t.Errorf("sources %v", got)
	}
	r.a.Release("problem:lossy")
	if got := r.settle(t)["d-1"]; got != (impair.Impairment{LatencyMs: 50, JitterMs: 5, LossPct: 10, RateKbit: 900}) {
		t.Errorf("after the problem, %+v: the operator's own impairment should remain whole", got)
	}
	if r.sink.calls != 2 {
		t.Errorf("the sink was reconciled %d times", r.sink.calls)
	}
}

// Set replaces a source's wants wholesale: a device left out is released, and
// an empty want is no want.
func TestSetReplacesASourcesWants(t *testing.T) {
	r := newArbRig(t)
	r.a.Set("operator", map[string]Want{"d-1": {Impairment: impair.Impairment{Unplugged: true}}, "d-2": {Impairment: impair.Impairment{Unplugged: true}}})
	r.a.Set("operator", map[string]Want{"d-2": {Impairment: impair.Impairment{Unplugged: true}}, "d-1": {}})
	if net := r.a.Resolved(); len(net) != 1 || !net["d-2"].Unplugged {
		t.Errorf("resolved %+v, want only d-2", net)
	}
	r.a.Set("operator", nil)
	if len(r.a.Sources()) != 0 {
		t.Errorf("sources %v after the operator released", r.a.Sources())
	}
}

// The meeting platform's offline listing asks the arbiter: an operator's unplug
// reads as a scripted one does, since when the earliest source had it, and a
// derivation answers for devices this simulator does not serve.
func TestOfflineReadsEverySourceAndDerivation(t *testing.T) {
	r := newArbRig(t)
	l := world.Listing{Name: r.d.Hostname}
	if _, down := r.a.Offline(l, r.now); down {
		t.Fatal("a healthy device reads offline")
	}
	r.a.Set("operator", map[string]Want{"d-1": {Impairment: impair.Impairment{Unplugged: true}}})
	r.settle(t)
	if since, down := r.a.Offline(l, r.now); !down || since != r.now.Unix() {
		t.Errorf("an operator's unplug reads (%d, %v), want since now", since, down)
	}
	r.now = r.now.Add(time.Hour)
	earlier := r.now.Add(-3 * time.Hour)
	r.a.Set("problem:dark", map[string]Want{"d-1": {Since: earlier, Impairment: impair.Impairment{Unplugged: true}}})
	if since, _ := r.a.Offline(l, r.now); since != earlier.Unix() {
		t.Errorf("since %d, want the earliest source's %d", since, earlier.Unix())
	}
	if since, _ := r.a.Offline(l, r.now); since == 0 {
		t.Error("no since")
	}
	r.a.Release("problem:dark")
	r.a.Release("operator")
	if _, down := r.a.Offline(l, r.now); down {
		t.Error("released everything but the device reads offline")
	}
	far := world.Listing{Name: "elsewhere.lab", Slot: "video-bar"}
	r.a.Derive("calendar", func(l world.Listing, now time.Time) (int64, bool) { return 42, l.Slot == "video-bar" })
	if since, down := r.a.Offline(far, r.now); !down || since != 42 {
		t.Errorf("a derivation answers for an unserved device: (%d, %v)", since, down)
	}
	var nilArb *Arbiter
	if _, down := nilArb.Offline(far, r.now); down {
		t.Error("a nil arbiter reads offline")
	}
}

func TestShutdownPutsEveryPinBack(t *testing.T) {
	r := newArbRig(t)
	r.a.Set("a", map[string]Want{"d-1": {Pins: []Pin{{Path: "level", Value: "1"}}}})
	r.settle(t)
	if errs := r.a.Shutdown([]*Device{r.d}); len(errs) != 0 {
		t.Fatal(errs)
	}
	if r.read("level") != "7" || len(r.a.Sources()) != 0 {
		t.Errorf("level %s, sources %v after shutdown", r.read("level"), r.a.Sources())
	}
}
