package lyra_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/device/lyraarc"
	"github.com/hyperscaleav/omniglass/sim/device/lyrahx"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The two Lyra families are one body over two rows (#2835); the tests here hold
// what the body does for each kind of microphone, over both.

var families = map[string]spec.Spec{"lyra-arc-a2": lyraarc.Spec(), "lyra-hx-1": lyrahx.Spec()}

func TestEachFamilyConforms(t *testing.T) {
	for name, s := range families {
		t.Run(name, func(t *testing.T) { spectest.Conform(t, s) })
	}
}

func route(t *testing.T, s spec.Spec, match string) spec.HTTPHandler {
	t.Helper()
	for _, r := range s.HTTP {
		if r.Match == match {
			return r.Handle
		}
	}
	t.Fatalf("%s serves no %s", s.Name, match)
	return nil
}

// Each microphone reports what its kind has: an array whether it is active, a
// wireless pack whether it transmits and how much charge it has left, in the
// order the document lists the keys.
func TestTheStatusDocumentReportsWhatEachKindHas(t *testing.T) {
	for product, want := range map[string]string{
		"lyra-arc-a2": `"mic": {"active":false,"level_db":-42,"mute":false}`,
		"lyra-hx-1":   `"mic": {"battery_percent":78,"level_db":-42,"mute":false,"transmitting":false}`,
	} {
		s := families[product]
		f := spectest.NewFake(s, product)
		body := string(route(t, s, "GET /api/status")(f, spec.HTTPRequest{}).Body.([]byte))
		if !strings.Contains(body, want) {
			t.Errorf("%s: status = %s, want it to carry %s", product, body, want)
		}
		if !strings.Contains(body, `"model": "`+f.Row.Model+`"`) {
			t.Errorf("%s: the status does not name its own model %q", product, f.Row.Model)
		}
	}
}

// Only a wireless pack answers a battery over SNMP.
func TestOnlyTheWirelessPackAnswersABattery(t *testing.T) {
	const battery = "1.3.6.1.4.1.55530.2.1.0"
	for product, want := range map[string]bool{"lyra-arc-a2": false, "lyra-hx-1": true} {
		s := families[product]
		served := false
		for _, o := range s.Surface(spectest.NewFake(s, product)).OIDs {
			if o.OID == battery {
				served = true
			}
		}
		if served != want {
			t.Errorf("%s answers the battery OID = %v, want %v", product, served, want)
		}
	}
}

// A mute is a switch: true or false, as text too, and anything else is refused
// with the validation document, writing nothing.
func TestMuteIsASwitch(t *testing.T) {
	s := families["lyra-arc-a2"]
	mute := route(t, s, "POST /api/mute")
	f := spectest.NewFake(s, "lyra-arc-a2")
	if res := mute(f, spec.HTTPRequest{Body: map[string]any{"mute": "true"}}); res.Status != 0 || f.Read("status.mic.mute") != true {
		t.Fatalf(`"true" answered %d and wrote %v, want the boolean true`, res.Status, f.Read("status.mic.mute"))
	}
	n := len(f.Writes)
	res := mute(f, spec.HTTPRequest{Body: map[string]any{"mute": "loud"}})
	var doc struct {
		Fields []struct{ Enum []string } `json:"fields"`
	}
	if err := json.Unmarshal(res.Body.([]byte), &doc); res.Status != 400 || err != nil || len(doc.Fields) != 1 || len(doc.Fields[0].Enum) != 2 || len(f.Writes) != n {
		t.Fatalf("a mute of %q answered %d %s, wrote %d times; want the validation document naming true and false and no write", "loud", res.Status, res.Body, len(f.Writes)-n)
	}
}

func tickOf(t *testing.T, s spec.Spec, name string) spec.Tick {
	t.Helper()
	for _, tk := range s.Ticks {
		if tk.Name == name {
			return tk
		}
	}
	t.Fatalf("%s has no %s tick", s.Name, name)
	return spec.Tick{}
}

// The array has no pack to drain: it has no battery behavior at all.
func TestAnArrayHasNoBatteryBehavior(t *testing.T) {
	for _, tk := range families["lyra-arc-a2"].Ticks {
		if tk.Name == "battery_drain" {
			t.Fatal("the ceiling array has a battery_drain behavior")
		}
	}
}

// A pack with no calendar drains a tenth of a percent a tick; with the room's
// meetings it is a function of them alone: full where none came, empty-bound
// through a meeting someone attends, charging after.
func TestThePackDrainsWithTheRoomsMeetingsAndOtherwiseByTheTick(t *testing.T) {
	s := families["lyra-hx-1"]
	drain := tickOf(t, s, "battery_drain")
	f := spectest.NewFake(s, "lyra-hx-1")
	now := f.Clock

	for i := 0; i < 3; i++ {
		for _, c := range drain.Run(f, spec.TickContext{Now: now}) {
			_ = f.Write(c.Path, c.Value)
		}
	}
	if got := f.Read("status.mic.battery_percent"); got != 77.7 {
		t.Errorf("three ticks with no calendar left %v, want 77.7", got)
	}

	room := calendar.RoomState{HasCalendar: true}
	meeting := behave.Span{Start: now.Add(-150 * time.Minute).Unix(), End: now.Add(-30 * time.Minute).Unix(), Actual: 4}
	noShow := behave.Span{Start: now.Add(-20 * time.Minute).Unix(), End: now.Add(-5 * time.Minute).Unix(), Actual: 0}
	ctx := spec.TickContext{Room: room, Now: now, Spans: func(_, _ time.Time) []behave.Span { return []behave.Span{meeting, noShow} }}
	got := drain.Run(f, ctx)[0].Value.(float64)
	want := behave.Battery(0, true, []behave.Span{meeting, noShow}, now, behave.DefaultBattery)
	if got != want {
		t.Errorf("the pack reads %v after a meeting and a no-show, the primitive says %v", got, want)
	}
	if got >= 100 || got < 5 {
		t.Errorf("the pack reads %v, want it drained by the meeting and charged since, short of full", got)
	}
}

// The call tick follows the room: active while someone attends, with the mute
// left alone otherwise, on the path its kind keeps it at.
func TestTheCallFollowsTheRoomOnTheKindsOwnPath(t *testing.T) {
	for product, path := range map[string]string{"lyra-arc-a2": "status.mic.active", "lyra-hx-1": "status.mic.transmitting"} {
		s := families[product]
		call := tickOf(t, s, "call_state")
		f := spectest.NewFake(s, product)
		in := calendar.RoomState{HasCalendar: true, Attendees: 3}
		var wrote bool
		for _, c := range call.Run(f, spec.TickContext{Room: in, Now: f.Clock, Rand: behave.NewSource(product)}) {
			if c.Path == path {
				wrote = c.Value == true
			}
		}
		if !wrote {
			t.Errorf("%s: with three people in the room %s was not set", product, path)
		}
	}
}
