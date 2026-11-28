package tessellatecensus

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
	"github.com/hyperscaleav/omniglass/sim/surface"
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

// The head-count is the raw-TCP band's alone: no HTTP document carries it, and
// the event feed says where to read it.
func TestTheHeadCountIsNotOnHTTP(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write(pathCount, int64(7))
	for _, m := range []string{"GET /api/status", "GET /api/occupancy", "GET /api/events"} {
		var doc map[string]any
		body := route(t, m).Handle(f, spec.HTTPRequest{}).Body.([]byte)
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("%s is not JSON: %v", m, err)
		}
		if strings.Contains(string(body), `"people_count"`) || strings.Contains(string(body), "7") {
			t.Errorf("%s carries the head-count:\n%s", m, body)
		}
	}
	if body := string(route(t, "GET /api/events").Handle(f, spec.HTTPRequest{}).Body.([]byte)); !strings.Contains(body, "COUNT? / SUBSCRIBE COUNT, port 5000") {
		t.Errorf("the event feed does not say where the count is:\n%s", body)
	}
}

// The control band answers a query, subscribes and unsubscribes by the subject
// word, and pushes "#<token>=<value>" with the value as JSON: a string quoted, a
// number and a flag bare.
func TestTheControlBandAnswersAndPushes(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write(pathCount, int64(5))
	_ = f.Write(pathVacant, false)
	sf := Spec().Surface(f)
	c := sf.Commanders[0]
	for line, want := range map[string]string{"COUNT?": "#COUNT=5\n", "VACANT?": "#VACANT=false\n"} {
		if r, ok := c.Handle(line); !ok || r.Text != want || r.Action != surface.ActionNone {
			t.Errorf("%s = %+v (%v), want %q", line, r, ok, want)
		}
	}
	for _, tc := range []struct {
		line, path string
		action     surface.Action
	}{
		{"SUBSCRIBE COUNT", pathCount, surface.ActionSubscribe}, {"SUBSCRIBE VACANT", pathVacant, surface.ActionSubscribe},
		{"SUBSCRIBE EVENT", pathEvent, surface.ActionSubscribe},
		{"UNSUBSCRIBE COUNT", pathCount, surface.ActionUnsubscribe}, {"UNSUBSCRIBE VACANT", pathVacant, surface.ActionUnsubscribe},
	} {
		r, ok := c.Handle(tc.line)
		if !ok || r.Action != tc.action || r.Subject != tc.path || !strings.HasPrefix(r.Text, "#ACK ") {
			t.Errorf("%s = %+v (%v)", tc.line, r, ok)
		}
	}
	for _, line := range []string{"UNSUBSCRIBE EVENT", "count?", "BOGUS", ""} {
		if _, ok := c.Handle(line); ok {
			t.Errorf("%q is answered", line)
		}
	}
	push := map[string]func(any) string{}
	for _, s := range sf.Subjects["control"] {
		push[s.Path] = s.Push
	}
	if got := push[pathCount](int64(4)) + push[pathVacant](true) + push[pathEvent]("decreased"); got != `#COUNT=4#VACANT=true#EVENT="decreased"` {
		t.Errorf("pushes = %q", got)
	}
}

// The binary band answers the fixed poll with the count as one raw byte, its low
// eight bits, behind the magic and the response opcode.
func TestTheHexPollAnswersTheCountAsOneByte(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	c := Spec().Surface(f).Commanders[0]
	for count, want := range map[int64]string{0: "\xaa\x81\x00", 3: "\xaa\x81\x03", 13: "\xaa\x81\r", 255: "\xaa\x81\xff", 256: "\xaa\x81\x00"} {
		_ = f.Write(pathCount, count)
		if r, ok := c.Handle("\xaa\x01"); !ok || r.Text != want {
			t.Errorf("a poll at %d answers %q (%v), want %q", count, r.Text, ok, want)
		}
	}
	if _, ok := c.Handle("\xaa\x02"); ok {
		t.Error("another opcode is answered")
	}
}

// The webhook target is stored for display and never posted to: a reboot leaves
// it, a factory reset clears it, and either puts the occupancy telemetry back.
func TestRebootKeepsTheWebhookAndFactoryResetClearsIt(t *testing.T) {
	run := func(match string) *spectest.Fake {
		f := spectest.NewFake(Spec(), Name)
		_ = f.Write(pathWebhook, "https://collector.example/x")
		_ = f.Write(pathCount, int64(9))
		_ = f.Write(pathEvent, "increased")
		_ = f.Write(pathVacant, false)
		if res := route(t, match).Handle(f, spec.HTTPRequest{}); res.Status != 0 {
			t.Fatalf("%s: status %d", match, res.Status)
		}
		return f
	}
	f := run("POST /api/admin/reboot")
	if f.Read(pathWebhook) != "https://collector.example/x" || f.Read(pathCount) != 0 || f.Read(pathEvent) != "steady" || f.Read(pathVacant) != true || f.Read(pathBootEpoch) != f.Clock.Unix() {
		t.Errorf("after a reboot: webhook %v count %v event %v vacant %v epoch %v", f.Read(pathWebhook), f.Read(pathCount), f.Read(pathEvent), f.Read(pathVacant), f.Read(pathBootEpoch))
	}
	if f = run("POST /api/admin/factory-reset"); f.Read(pathWebhook) != "" {
		t.Errorf("a factory reset left the webhook %v", f.Read(pathWebhook))
	}
}

// The tick is the primitive over the state: the count is the calendar's attendees
// (a walk with none), the previous count and the signal follow it, and vacant is
// the count being zero.
func TestTheTickIsThePeopleCountPrimitive(t *testing.T) {
	tick := Spec().Ticks[0]
	f := spectest.NewFake(Spec(), Name)
	src, twin := behave.NewSource("d"), behave.NewSource("d")
	meeting := calendar.RoomState{HasCalendar: true, Scheduled: true, CallActive: true, Attendees: 5}
	prev := behave.OccupancyState{}
	for i, room := range []calendar.RoomState{meeting, meeting, {HasCalendar: true, Scheduled: true}, {}, {}, meeting} {
		want := behave.PeopleCount(prev, room, 0, behave.DefaultOccupancy, twin)
		for _, ch := range tick.Run(f, spec.TickContext{Room: room, Rand: src}) {
			_ = f.Write(ch.Path, ch.Value)
		}
		got := behave.OccupancyState{Count: int(spec.Int64(f, pathCount)), Prev: int(spec.Int64(f, pathPrev)), LastEvent: spec.String(f, pathEvent), Vacant: spec.Bool(f, pathVacant)}
		if got != want {
			t.Fatalf("tick %d: state %+v, the primitive says %+v", i, got, want)
		}
		prev = want
	}
}

// The dashboard is html/template: what the sensor holds is escaped, and its own
// script keeps its comments.
func TestTheDashboardEscapesTheWebhookAndKeepsItsScript(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write(pathWebhook, `https://c.example/?a=1&b=<2>`)
	_ = f.Write(pathCount, int64(3))
	_ = f.Write(pathVacant, false)
	page := string(dashboardRoute(f, spec.HTTPRequest{}).Body.([]byte))
	for _, want := range []string{"a=1&amp;b=&lt;2&gt;", `<div class="count">3<small>`, `<span class="pill occupied">OCCUPIED</span>`, "// Poll the live occupancy feed"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, "<2>") {
		t.Error("the webhook target reached the page unescaped")
	}
	var doc map[string]any
	if err := json.Unmarshal(route(t, "GET /api/status").Handle(f, spec.HTTPRequest{}).Body.([]byte), &doc); err != nil {
		t.Errorf("the status is not JSON: %v", err)
	}
}
