package kestrelrelay

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func route(t *testing.T, match string) spec.HTTPRoute {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r
		}
	}
	t.Fatalf("no route %s", match)
	return spec.HTTPRoute{}
}

func call(t *testing.T, f *spectest.Fake, match string, body map[string]any) spec.HTTPResponse {
	t.Helper()
	return route(t, match).Handle(f, spec.HTTPRequest{Body: body})
}

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name || res.Identity.Model != r.Model || res.Identity.DeviceType != "video-codec" {
			t.Fatalf("%s resolves to %+v %+v", r.Product, res.Definition, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		var doc systemDoc
		if err := json.Unmarshal(call(t, f, "GET /api/system", nil).Body.([]byte), &doc); err != nil || doc.Model != r.Model || doc.Firmware != r.Firmware || doc.Manufacturer != "Kestrel" {
			t.Errorf("%s: system = %+v (%v)", r.Product, doc, err)
		}
	}
}

// A dialed call is the body's count, two when it names none or names zero, and a
// count that is not one is refused with nothing written.
func TestDialTakesTheBodysCountOrTwo(t *testing.T) {
	for _, tc := range []struct {
		body map[string]any
		want any
		ok   bool
	}{
		{map[string]any{"participants": float64(3)}, int64(3), true},
		{map[string]any{"participants": "9"}, int64(9), true},
		{map[string]any{"participants": 3.5}, 3.5, true},
		{map[string]any{"participants": float64(0)}, int64(2), true},
		{map[string]any{"participants": ""}, int64(2), true},
		{map[string]any{}, int64(2), true},
		{nil, int64(2), true},
		{map[string]any{"participants": "many"}, nil, false},
		{map[string]any{"participants": true}, nil, false},
		{map[string]any{"participants": []any{1}}, nil, false},
	} {
		f := spectest.NewFake(Spec(), "kestrel-relay-c1")
		res := call(t, f, "POST /api/call/dial", tc.body)
		if (res.Status == 0) != tc.ok {
			t.Errorf("%v: status %d, want ok=%v", tc.body, res.Status, tc.ok)
			continue
		}
		if !tc.ok {
			if len(f.Writes) != 0 {
				t.Errorf("%v: a refused dial wrote %v", tc.body, f.Writes)
			}
			continue
		}
		if f.Read(pathParticipants) != tc.want || f.Read(pathActive) != true || f.Read(pathDialed) != true || f.Read(pathCall) != "" {
			t.Errorf("%v: participants %v active %v dialed %v call %q", tc.body, f.Read(pathParticipants), f.Read(pathActive), f.Read(pathDialed), f.Read(pathCall))
		}
	}
}

func TestDisconnectEndsTheCallAndMarksTheHangup(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-relay-c1")
	call(t, f, "POST /api/call/dial", map[string]any{"participants": float64(4)})
	call(t, f, "POST /api/content/share", map[string]any{"source": "usbc"})
	call(t, f, "POST /api/call/disconnect", nil)
	for path, want := range map[string]any{pathActive: false, pathParticipants: 0, pathSharing: false, pathSource: "", pathDialed: false, pathHungup: true} {
		if got := f.Read(path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
}

func TestSharingNamesASourceAndStopEmptiesIt(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-relay-c1")
	if res := call(t, f, "POST /api/content/share", map[string]any{"source": "vga"}); res.Status != 400 || len(f.Writes) != 0 {
		t.Fatalf("a source the codec lacks: %d, %d writes", res.Status, len(f.Writes))
	}
	call(t, f, "POST /api/content/share", map[string]any{"source": "wireless"})
	if f.Read(pathSharing) != true || f.Read(pathSource) != "wireless" {
		t.Fatalf("sharing %v from %v", f.Read(pathSharing), f.Read(pathSource))
	}
	call(t, f, "POST /api/content/stop", nil)
	if f.Read(pathSharing) != false || f.Read(pathSource) != "" {
		t.Fatalf("after stop: %v from %v", f.Read(pathSharing), f.Read(pathSource))
	}
}

func TestRebootClearsTheCallAndRestartsUptime(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-relay-c1")
	call(t, f, "POST /api/call/dial", nil)
	call(t, f, "POST /api/admin/reboot", nil)
	if f.Read(pathActive) != false || f.Read(pathDialed) != false || f.Read(pathBootEpoch) != f.Clock.Unix() {
		t.Fatalf("after a reboot: active %v dialed %v anchor %v", f.Read(pathActive), f.Read(pathDialed), f.Read(pathBootEpoch))
	}
}

// With no calendar the codec never calls by itself, and with a meeting running
// it is in a call that follows who came; an operator's dial holds against the
// calendar.
func TestTheCallCycleKeepsNoScheduleOfItsOwn(t *testing.T) {
	var cycle spec.Tick
	for _, tk := range Spec().Ticks {
		cycle = tk
	}
	f := spectest.NewFake(Spec(), "kestrel-relay-c1")
	rng := behave.NewSource("relay")
	ctx := spec.TickContext{Room: spectest.IdleRoom(), Now: f.Clock, Rand: rng}
	for i := 0; i < 200; i++ {
		for _, ch := range cycle.Run(f, ctx) {
			_ = f.Write(ch.Path, ch.Value)
		}
		if f.Read(pathActive) != false {
			t.Fatalf("a codec with no calendar called at tick %d", i)
		}
	}
	call(t, f, "POST /api/call/dial", map[string]any{"participants": float64(5)})
	for i := 0; i < 50; i++ {
		for _, ch := range cycle.Run(f, ctx) {
			_ = f.Write(ch.Path, ch.Value)
		}
	}
	if f.Read(pathActive) != true || f.Read(pathDialed) != true {
		t.Fatalf("a dialed call did not hold: active %v dialed %v", f.Read(pathActive), f.Read(pathDialed))
	}
}
