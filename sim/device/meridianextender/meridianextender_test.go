package meridianextender

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v, want the %s family", r.Product, res.Definition, Name)
		}
		if res.Identity.Model != r.Model || res.Identity.DeviceType != "av-extender" || res.Identity.Summary != r.Summary() {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		got := string(statusRoute(f, spec.HTTPRequest{}).Body.([]byte))
		if !strings.Contains(got, `"model": "`+r.Model+`"`) || !strings.Contains(got, `"firmware": "`+r.Firmware+`"`) {
			t.Errorf("%s: status = %s", r.Product, got)
		}
	}
}

// An extender has no route: its API is a status and a reboot, and the
// reboot retrains a link a scenario pulled.
func TestAnExtenderTakesNoRouteAndARebootRetrainsTheLink(t *testing.T) {
	for _, r := range Spec().HTTP {
		if strings.Contains(r.Match, "route") {
			t.Fatalf("an extender serves %s", r.Match)
		}
	}
	f := spectest.NewFake(Spec(), "meridian-xl-1")
	_ = f.Write(pathLink, "down")
	for _, r := range Spec().HTTP {
		if r.Match == "POST /api/admin/reboot" {
			r.Handle(f, spec.HTTPRequest{})
		}
	}
	if f.Read(pathLink) != "up" || f.Read(pathBootEpoch) != f.Clock.Unix() {
		t.Fatalf("after a reboot: link %v, epoch %v", f.Read(pathLink), f.Read(pathBootEpoch))
	}
}

// The signal is the meeting's: present while a meeting someone attends is held,
// absent otherwise, and a room with no calendar keeps it. The link is the
// cable's, and no tick touches it.
func TestTheSignalFollowsTheMeetingAndTheLinkIsLeftAlone(t *testing.T) {
	f := spectest.NewFake(Spec(), "meridian-xl-1")
	tick := Spec().Ticks[0]
	_ = f.Write(pathLink, "down")
	step := func(room calendar.RoomState) {
		for _, ch := range tick.Run(f, spec.TickContext{Room: room}) {
			if ch.Path == pathLink {
				t.Fatalf("a tick wrote the link")
			}
			_ = f.Write(ch.Path, ch.Value)
		}
	}
	step(calendar.RoomState{HasCalendar: true, CallActive: true})
	if f.Read(pathSignal) != "present" {
		t.Fatalf("in a meeting: %v", f.Read(pathSignal))
	}
	step(calendar.RoomState{HasCalendar: true})
	if f.Read(pathSignal) != "absent" {
		t.Fatalf("between meetings: %v", f.Read(pathSignal))
	}
	step(spectest.IdleRoom())
	if f.Read(pathSignal) != "absent" || f.Read(pathLink) != "down" {
		t.Fatalf("no calendar: signal %v link %v, want both as they were", f.Read(pathSignal), f.Read(pathLink))
	}
}
