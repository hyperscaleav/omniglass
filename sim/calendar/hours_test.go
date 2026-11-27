package calendar

import (
	"testing"
	"time"
)

// A room's hours are its knobs' hours, whole UTC hours, and read as open
// between the start (inclusive) and the end (exclusive). Hours that are not
// declared are the whole day, and a room with no knobs has none to report.
func TestAHoursSpanSaysWhenTheRoomIsOpen(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		h      Hours
		at     time.Duration
		want   bool
		sH, eH int
	}{
		{Hours{Start: 8, End: 18}, 7*time.Hour + 59*time.Minute, false, 8, 18},
		{Hours{Start: 8, End: 18}, 8 * time.Hour, true, 8, 18},
		{Hours{Start: 8, End: 18}, 17*time.Hour + 59*time.Minute, true, 8, 18},
		{Hours{Start: 8, End: 18}, 18 * time.Hour, false, 8, 18},
		{Hours{}, 3 * time.Hour, true, 0, 24},
	} {
		if got := c.h.Open(day.Add(c.at)); got != c.want {
			t.Errorf("%+v at %s: open %v, want %v", c.h, c.at, got, c.want)
		}
		if s, e := c.h.Span(); s != c.sH || e != c.eH {
			t.Errorf("%+v span %d to %d, want %d to %d", c.h, s, e, c.sH, c.eH)
		}
	}
}

func TestACalendarReportsARoomsHours(t *testing.T) {
	with := Room{Site: "hq", Building: "tower", Room: "r1"}
	cal := New(1, func(r Room) (Knobs, bool) {
		if r == with {
			return Knobs{Hours: Hours{Start: 7, End: 19}}, true
		}
		return Knobs{}, false
	})
	if h, ok := cal.Hours(with); !ok || h != (Hours{Start: 7, End: 19}) {
		t.Fatalf("hours = %+v %v", h, ok)
	}
	if _, ok := cal.Hours(Room{Site: "hq", Building: "tower", Room: "r2"}); ok {
		t.Fatal("a room with no knobs reports hours")
	}
	var none *Calendar
	if _, ok := none.Hours(with); ok {
		t.Fatal("a nil calendar reports hours")
	}
}
