package spec_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// A device with no world behind it (a unit test's) reads no rooms: nothing
// served or covered, every room idle and free, no device listed, none offline.
func TestADeviceWithNoWorldReadsNoRooms(t *testing.T) {
	d := spectest.NewFake(spectest.Spec(), "")
	r := spec.RoomsOf(d)
	if len(r.Served()) != 0 || len(r.Covered()) != 0 || len(r.Listed()) != 0 || r.Offline("sys") {
		t.Errorf("a device with no world reads rooms: served %v covered %v listed %v", r.Served(), r.Covered(), r.Listed())
	}
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r1"}
	if st := r.State(room); st.HasCalendar || st.Phase != calendar.PhaseIdle {
		t.Errorf("a room with no calendar reads %+v, want an idle one", st)
	}
	if b := r.Bookings(room); b.State != "free" || b.Today != 0 {
		t.Errorf("a room with no calendar reads %+v booked, want it free", b)
	}
	if _, ok := r.MeetingNow(room); ok {
		t.Error("a room with no calendar holds a meeting")
	}
	if _, ok := r.Place("sys"); ok {
		t.Error("a system with no world is placed")
	}
}

// A device that cannot send a webhook or name an event drops them, so a handler
// is written once; the fake records them for a test to read.
func TestWebhooksAndEventsAreDroppedByADeviceThatCannotSendThem(t *testing.T) {
	bare := struct{ spec.Device }{spectest.NewFake(spectest.Spec(), "")}
	spec.Send(bare, spec.Post{URL: "http://x", Body: []byte("{}")})
	spec.Report(bare, "e", "{}")

	f := spectest.NewFake(spectest.Spec(), "")
	spec.Send(f, spec.Post{URL: "http://x/y", Body: []byte("{}")})
	spec.Send(f, spec.Post{Body: []byte("{}")})
	spec.Report(f, "recording-failed", `{"id":"r"}`)
	if len(f.Posts) != 1 || f.Posts[0].URL != "http://x/y" {
		t.Errorf("posts = %+v, want the one with a target", f.Posts)
	}
	if len(f.Reports) != 1 || f.Reports[0][0] != "recording-failed" {
		t.Errorf("reports = %+v", f.Reports)
	}
}
