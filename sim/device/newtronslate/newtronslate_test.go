package newtronslate

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
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
		if res.Identity.Model != r.Model || res.Identity.DeviceType != "scheduling-panel" || res.Identity.Summary != r.Summary() {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		if spec.String(f, "configuration.firmware.version") != r.Firmware || spec.String(f, "configuration.device.model") != r.Model {
			t.Errorf("%s: state does not carry its row", r.Product)
		}
	}
}

func ask(t *testing.T, f *spectest.Fake, line string) (string, bool) {
	t.Helper()
	for _, c := range surfaceOf(f).Commanders {
		if r, ok := c.Handle(line); ok {
			for _, ch := range r.Changes {
				_ = f.Write(ch.Path, ch.Value)
			}
			return r.Text, true
		}
	}
	return "", false
}

// The panel answers its room's booking and nothing of a touch panel's room or a
// processor's program.
func TestThePanelAnswersTheBookingAndNothingElse(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-slate-5")
	for _, tc := range [][2]string{
		{"GET BOOKING", "BOOKING free"}, {"GET NEXT", "NEXT none"}, {"GET CURRENT", "CURRENT none"},
		{"BOOK", "BOOKING booked"}, {"GET BOOKING", "BOOKING booked"}, {"RELEASE", "BOOKING free"},
	} {
		if got, ok := ask(t, f, tc[0]); !ok || got != tc[1] {
			t.Errorf("%s = %q (%v), want %q", tc[0], got, ok, tc[1])
		}
	}
	for _, line := range []string{"GET INPUT", "SET SCENE idle", "GET PROGRAM", "book", "BOOK now"} {
		if _, ok := ask(t, f, line); ok {
			t.Errorf("%q is answered; a scheduling panel answers the booking alone", line)
		}
	}
}

// The panel follows the calendar's booked-or-free reading when it changes, and
// a button holds until then; the meetings it shows are the calendar's.
func TestTheButtonHoldsUntilTheCalendarMoves(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-slate-5")
	tick := Spec().Ticks[0]
	free := behave.Bookings{State: behave.StateFree}
	booked := behave.Bookings{State: behave.StateBooked, Booked: true,
		Now:  &behave.Booking{SlotStart: 100, SlotEnd: 200, Organizer: "Avery Chen"},
		Next: &behave.Booking{SlotStart: 300, SlotEnd: 400, Organizer: "Quinn Ashby"}}
	step := func(b behave.Bookings) {
		for _, ch := range tick.Run(f, spec.TickContext{Bookings: func() behave.Bookings { return b }}) {
			_ = f.Write(ch.Path, ch.Value)
		}
	}
	read := func(path string) string { return spec.String(f, path) }

	step(free)
	ask(t, f, "BOOK")
	step(free) // the calendar still reads free: the button holds
	if read(pathBooking) != "booked" {
		t.Fatalf("the calendar overrode a button press with no change: booking %q", read(pathBooking))
	}
	step(booked) // the calendar books the room
	if read(pathBooking) != "booked" || read(pathCalendar) != "booked" || read(pathCurrent) != "100 200 Avery Chen" || read(pathNext) != "300 400 Quinn Ashby" {
		t.Fatalf("booked: %q %q %q %q", read(pathBooking), read(pathCalendar), read(pathCurrent), read(pathNext))
	}
	ask(t, f, "RELEASE")
	step(booked) // no change in the calendar: the release holds
	if read(pathBooking) != "free" {
		t.Fatalf("the calendar overrode a release with no change: booking %q", read(pathBooking))
	}
	step(free) // the calendar frees the room
	if read(pathBooking) != "free" || read(pathCurrent) != "none" || read(pathNext) != "none" {
		t.Fatalf("free: %q %q %q", read(pathBooking), read(pathCurrent), read(pathNext))
	}
}

func TestTheBookingIsPublishedAsAnEventOnItsFace(t *testing.T) {
	subjects := surfaceOf(spectest.NewFake(Spec(), "")).Subjects["nvp"]
	if len(subjects) != 1 || subjects[0].Path != pathBooking || subjects[0].Push("booked") != "EVT BOOKING booked" {
		t.Fatalf("subjects = %+v", subjects)
	}
}
