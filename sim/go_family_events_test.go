//go:build linux

package sim

import (
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// A family reports named events on its device's live feed (#2834): a route
// publishes one after the writes it follows, a tick names the events it reports
// from the state its changes left, an event with no name is not the tick's, and
// a route marked private is listed as one. A device whose face declares no feed
// drops them.
func eventFamily() spec.Spec {
	return spec.Spec{
		Name:     "testfamily-events",
		Identity: spec.Identity{Manufacturer: "Testfamily", Model: "Events", DeviceType: "display", Summary: "A device that reports events"},
		Models:   []spec.Model{{Product: "tf-events-1", Model: "Events 1"}},
		Interfaces: []profile.Interface{{
			Name: "api", Protocol: "http", Port: 8081, Doc: profile.Doc{Summary: "API"},
			Events: profile.Events{Path: "/api/events", Paths: []string{"status.count"}},
		}},
		State: map[string]any{"status": map[string]any{"count": 0}},
		HTTP: []spec.HTTPRoute{
			{Match: "POST /api/bump", Summary: "Count one", Handle: func(d spec.Device, _ spec.HTTPRequest) spec.HTTPResponse {
				_ = d.Write("status.count", spec.Int(d, "status.count")+1)
				d.Publish(spec.Event{Name: "bumped", Data: `{"count":` + spec.String(d, "status.count") + `}`})
				return spec.Text("ok")
			}},
			{Match: "GET /api/secret", Summary: "Not advertised", Private: true, Handle: func(spec.Device, spec.HTTPRequest) spec.HTTPResponse { return spec.Text("shh") }},
		},
		Ticks: []spec.Tick{{
			Name: "count_up", Interval: time.Minute, Writes: []string{"status.count"},
			Run: func(d spec.Device, _ spec.TickContext) []surface.Change {
				return []surface.Change{{Path: "status.count", Value: spec.Int(d, "status.count") + 10}}
			},
			Emit: func(d spec.Device, _ spec.TickContext) []spec.Event {
				if spec.Int(d, "status.count")%20 == 0 {
					return []spec.Event{{Name: "round", Data: spec.String(d, "status.count")}}
				}
				return []spec.Event{{}}
			},
		}},
	}
}

func TestAFamilyReportsNamedEventsOnTheFeedAfterTheChangesTheyFollow(t *testing.T) {
	e := familyEngine(t, eventFamily())
	snap := DeviceSnapshot{DeviceID: "ev-1", Hostname: "ev-1", LoopbackIP: "127.0.0.1", ProfileName: "testfamily-events", Product: "tf-events-1", Version: 1}
	if err := e.Sync([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.StopBehaviors()
	if d.events == nil {
		t.Fatal("the device has no feed")
	}
	named := func() []string {
		var out []string
		for _, ev := range d.events.ring {
			out = append(out, ev.name+" "+ev.data)
		}
		return out
	}
	req := asConsole(httptest.NewRequest("POST", "/api/bump", nil))
	req.RemoteAddr = "console"
	d.ServeHTTP(httptest.NewRecorder(), req)
	if got, want := named(), []string{`change {"path":"status.count","value":1}`, `bumped {"count":1}`}; !slices.Equal(got, want) {
		t.Fatalf("a route's event follows its change: got %q want %q", got, want)
	}
	d.TickBehavior("count_up") // 11: no event
	d.TickBehavior("count_up") // 21: no event
	d.SetState(withCount(d, 10), d.Version+1)
	d.TickBehavior("count_up") // 20: a round
	if got := named(); got[len(got)-1] != "round 20" || got[len(got)-2] != `change {"path":"status.count","value":20}` {
		t.Fatalf("a tick's event follows its change and the quiet turn adds none: %q", got)
	}
	for _, ev := range named() {
		if ev == "round 11" || ev == "round 21" {
			t.Fatalf("an event with no name was published: %q", named())
		}
	}
}

func TestARoutePrivateToTheFamilyIsListedAsPrivate(t *testing.T) {
	f, err := spec.NewFamily(eventFamily())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range f.Surfaces()[0].Routes {
		if got, want := r.Visibility == "private", r.Match == "GET /api/secret"; got != want {
			t.Errorf("%s: private %v, want %v", r.Match, got, want)
		}
	}
}

func withCount(d *Device, n int) map[string]any {
	s := d.State()
	s["status"].(map[string]any)["count"] = n
	return s
}
