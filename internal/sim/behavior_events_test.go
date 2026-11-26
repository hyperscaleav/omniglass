package sim

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// A behavior may report named events (#2754), as a route does (#1145): a
// video bar whose call cycle runs on its own reports the call starting as a
// named occurrence on its live feed, not only as a change to its state. Each
// event renders against the state the tick left and is numbered in the same
// stream as the changes; an event whose data renders empty is not one this
// tick reports, so a template can say when it fires.
func TestABehaviorsNamedEventsStreamAfterItsTick(t *testing.T) {
	d := newTestDevice(t, "bar", map[string]any{"call": map[string]any{"phase": int64(0), "id": ""}})
	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:     "call_cycle",
		Schedule: profile.Schedule{Interval: "15s"},
		Mutations: []profile.Mutation{
			{Path: "call.phase", Value: `{{ if ge (.State | get "call" | get "phase") 1 }}0{{ else }}1{{ end }}`},
			{Path: "call.id", Value: "c9001"},
		},
		Events: []profile.RouteEvent{
			{Name: "call-started", Data: `{{ if eq (.State | get "call" | get "phase") 1 }}{"id":"{{ .State | get "call" | get "id" }}"}{{ end }}`},
			{Name: "call-ended", Data: `{{ if eq (.State | get "call" | get "phase") 0 }}{"id":"{{ .State | get "call" | get "id" }}"}{{ end }}`},
		},
	}); err != nil {
		t.Fatal(err)
	}
	d.SetEvents(profile.Events{Path: "/api/events", Paths: []string{"call.phase"}})

	d.tickBehavior(&d.behaviors[0])
	d.tickBehavior(&d.behaviors[0])

	var got []string
	for _, ev := range d.events.replayFrom("1") {
		got = append(got, ev.name+" "+ev.data)
	}
	want := []string{
		`change {"path":"call.phase","value":1}`,
		`call-started {"id":"c9001"}`,
		`change {"path":"call.phase","value":0}`,
		`call-ended {"id":"c9001"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the feed replays\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A behavior tick fans out only what it changed (#2754): a cycle that writes
// the same value it found (the call still idle, the id it already holds)
// streams no change and bumps no version, so a watched path that a timer
// rewrites every tick does not flood the feed with repeats.
func TestABehaviorTickStreamsOnlyWhatItChanged(t *testing.T) {
	d := newTestDevice(t, "bar", map[string]any{"call": map[string]any{"active": false, "level": int64(1)}})
	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name:      "steady",
		Schedule:  profile.Schedule{Interval: "15s"},
		Mutations: []profile.Mutation{{Path: "call.active", Value: false}, {Path: "call.level", Value: `{{ add (.State | get "call" | get "level") 1 }}`}},
	}); err != nil {
		t.Fatal(err)
	}
	d.SetEvents(profile.Events{Path: "/api/events"})
	var published []StateChange
	d.DiffPublisher = func(c StateChange) { published = append(published, c) }

	d.tickBehavior(&d.behaviors[0])

	if len(published) != 1 || len(published[0].Diffs) != 1 || published[0].Diffs["call.level"] != int64(2) {
		t.Fatalf("published %+v, want only call.level's change", published)
	}
	evs := d.events.replayFrom("1")
	if len(evs) != 1 || evs[0].data != `{"path":"call.level","value":2}` {
		t.Fatalf("the feed holds %+v, want only call.level's change", evs)
	}
}

// A behavior event needs a name a stream can carry, and data that parses.
func TestABehaviorEventIsCheckedAtLoad(t *testing.T) {
	for _, ev := range []profile.RouteEvent{{Name: "", Data: "{}"}, {Name: "a\nb", Data: "{}"}, {Name: "x", Data: "{{ .State "}} {
		d := newTestDevice(t, "bar", nil)
		err := d.AddBehaviorFromProfile(&profile.Behavior{
			Name: "b", Schedule: profile.Schedule{Interval: "15s"},
			Mutations: []profile.Mutation{{Path: "x", Value: 1}},
			Events:    []profile.RouteEvent{ev},
		})
		if err == nil {
			t.Errorf("event %+v loaded, want a refusal", ev)
		}
	}
	// Events alone are a behavior: a timer that only reports.
	d := newTestDevice(t, "bar", nil)
	if err := d.AddBehaviorFromProfile(&profile.Behavior{
		Name: "heartbeat", Schedule: profile.Schedule{Interval: "15s"},
		Events: []profile.RouteEvent{{Name: "log", Data: `{"text":"alive"}`}},
	}); err != nil {
		t.Fatalf("an events-only behavior: %v", err)
	}
}
