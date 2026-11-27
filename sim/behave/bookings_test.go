package behave_test

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

func TestBookingsReadTheBookedEntriesAlone(t *testing.T) {
	cal := pinnedCalendar()
	for _, tc := range []struct {
		name             string
		hms              string
		today, remaining int
		state            string
		now, next        string // organizer, "" for none
	}{
		{"before the day: four booked ahead", "07:00:00", 4, 4, "free", "", "Avery Chen"},
		{"booked meeting holds the room from its slot", "08:29:00", 4, 4, "booked", "Avery Chen", "Quinn Ashby"},
		{"a walk-in is no booking: the room reads free", "09:34:00", 4, 3, "free", "", "Quinn Ashby"},
		{"booked, ended early: holds its slot to the end, itself still left", "10:58:00", 4, 3, "booked", "Quinn Ashby", "Rowan Teague"},
		{"back to back walk-in is still free", "11:05:00", 4, 2, "free", "", "Rowan Teague"},
		{"booked, short", "12:50:00", 4, 2, "booked", "Rowan Teague", "Tobias Lund"},
		{"a no-show is still booked for its slot; the next is tomorrow's", "13:50:00", 4, 1, "booked", "Tobias Lund", "Noor Haddad"},
		{"after the last: the next is tomorrow's", "14:30:00", 4, 0, "free", "", "Noor Haddad"},
	} {
		b := behave.BookingsAt(cal, room, at(tc.hms))
		if b.Today != tc.today || b.Remaining != tc.remaining || b.State != tc.state || b.Booked != (tc.state == "booked") {
			t.Errorf("%s: %+v, want %d today, %d left, %s", tc.name, b, tc.today, tc.remaining, tc.state)
		}
		if got := organizer(b.Now); got != tc.now {
			t.Errorf("%s: now %q, want %q", tc.name, got, tc.now)
		}
		if got := organizer(b.Next); got != tc.next {
			t.Errorf("%s: next %q, want %q", tc.name, got, tc.next)
		}
	}
}

func organizer(b *behave.Booking) string {
	if b == nil {
		return ""
	}
	return b.Organizer
}

func TestBookingsWithNoCalendarAreFreeAndEmpty(t *testing.T) {
	b := behave.BookingsAt(nil, room, at("10:00:00"))
	if b.Today != 0 || b.Remaining != 0 || b.State != "free" || b.Now != nil || b.Next != nil {
		t.Fatalf("%+v", b)
	}
	if got := b.Summary(); got != (behave.RoomSummary{State: "free"}) {
		t.Fatalf("%+v", got)
	}
}

func TestRoomBookingFollowsTheCalendarAndHoldsAnOperatorsChange(t *testing.T) {
	cal := pinnedCalendar()
	free, booked := behave.BookingsAt(cal, room, at("09:34:00")), behave.BookingsAt(cal, room, at("08:29:00"))
	for _, tc := range []struct {
		name string
		prev behave.RoomBookingState
		b    behave.Bookings
		want string
	}{
		{"the calendar's change moves the report", behave.RoomBookingState{Booking: "free", CalBooking: "free"}, booked, "booked"},
		{"an operator's change holds while the calendar is steady", behave.RoomBookingState{Booking: "free", CalBooking: "booked"}, booked, "free"},
		{"until the calendar next changes", behave.RoomBookingState{Booking: "free", CalBooking: "booked"}, free, "free"},
		{"and then it does", behave.RoomBookingState{Booking: "booked", CalBooking: "booked"}, free, "free"},
	} {
		got := behave.RoomBooking(tc.prev, tc.b)
		if got.Booking != tc.want || got.CalBooking != tc.b.State {
			t.Errorf("%s: %+v, want %s", tc.name, got, tc.want)
		}
	}
	got := behave.RoomBooking(behave.RoomBookingState{}, booked)
	if want := fmt.Sprintf("%d %d Avery Chen", at("08:30:00").Unix(), at("09:00:00").Unix()); got.Current != want || got.Next == "none" {
		t.Errorf("current %q next %q in a booked room with another ahead, want %q", got.Current, got.Next, want)
	}
	if got := behave.RoomBooking(behave.RoomBookingState{}, behave.BookingsAt(nil, room, at("14:30:00"))); got.Current != "none" || got.Next != "none" {
		t.Errorf("no calendar: %q %q, want none", got.Current, got.Next)
	}
}

func TestSumBookingsTotalsTheRoomsAServiceSpeaksFor(t *testing.T) {
	cal := pinnedCalendar()
	rooms := []behave.Bookings{behave.BookingsAt(cal, room, at("08:29:00")), behave.BookingsAt(cal, room, at("09:34:00")), behave.BookingsAt(cal, room, at("13:50:00"))}
	if got := behave.SumBookings(99, rooms); got != (behave.ServiceBookings{Today: 12, Remaining: 8, InProgress: 2}) {
		t.Errorf("%+v", got)
	}
	if got := behave.SumBookings(7, nil); got != (behave.ServiceBookings{Today: 7}) {
		t.Errorf("a service with no room: %+v, want its day held and nothing running", got)
	}
}

func TestFixedDayIsTheServicesDayWithNoCalendar(t *testing.T) {
	ws, err := behave.ParseWindows([]string{"09:00-10:00", "10:30-11:30", "13:00-14:00", "15:00-16:30"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		hms   string
		state string
		left  int
	}{
		{"08:00:00", "free", 4}, {"09:00:00", "booked", 4}, {"10:00:00", "free", 3}, {"10:45:00", "booked", 3},
		{"12:00:00", "free", 2}, {"15:59:00", "booked", 1}, {"16:30:00", "free", 0},
	} {
		if s, n := behave.FixedDay(at(tc.hms), ws); s != tc.state || n != tc.left {
			t.Errorf("%s: %s with %d left, want %s with %d", tc.hms, s, n, tc.state, tc.left)
		}
	}
	for _, bad := range [][]string{{"9:00-10:00"}, {"09:00"}, {"10:00-09:00"}, {"25:00-26:00"}, {"09:61-10:00"}} {
		if _, err := behave.ParseWindows(bad); err == nil {
			t.Errorf("%v parsed", bad)
		}
	}
	if ws, err := behave.ParseWindows([]string{"23:00-24:00"}); err != nil || len(ws) != 1 {
		t.Errorf("24:00 as the day's end: %v %v", ws, err)
	}
}
