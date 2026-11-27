package calendar

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	day0  = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC) // a Monday
	roomA = Room{Site: "hq", Building: "tower", Room: "r101"}
	roomB = Room{Site: "hq", Building: "tower", Room: "r102"}
)

func knobs() Knobs {
	return Knobs{
		BookingRate:  0.5,
		SlotMix:      map[int]float64{15: 1, 30: 2, 60: 1},
		NoShowRate:   0.15,
		EndEarlyRate: 0.2,
		EndLateRate:  0.1,
		Attendance:   Range{Min: 2, Max: 8},
		WalkInRate:   0.25,
	}
}

func TestValidateNamesTheField(t *testing.T) {
	cases := map[string]func(*Knobs){
		"booking_rate":     func(k *Knobs) { k.BookingRate = 1.5 },
		"no_show_rate":     func(k *Knobs) { k.NoShowRate = -0.1 },
		"walk_in_rate":     func(k *Knobs) { k.WalkInRate = 2 },
		"ended_early_rate": func(k *Knobs) { k.EndEarlyRate = 0.7; k.EndLateRate = 0.5 },
		"slot_mix":         func(k *Knobs) { k.SlotMix = map[int]float64{45: 1} },
		"slot_mix ":        func(k *Knobs) { k.SlotMix = map[int]float64{} },
		"attendance":       func(k *Knobs) { k.Attendance = Range{Min: 5, Max: 2} },
		"attendance.min":   func(k *Knobs) { k.Attendance = Range{Min: 0, Max: 2} },
		"booking_rate nan": func(k *Knobs) { k.BookingRate = math.NaN() },
		"slot_mix inf":     func(k *Knobs) { k.SlotMix = map[int]float64{30: math.Inf(1)} },
		"hours":            func(k *Knobs) { k.Hours = Hours{Start: 10, End: 9} },
		"slot_mix weight":  func(k *Knobs) { k.SlotMix = map[int]float64{30: -1} },
	}
	for want, mut := range cases {
		k := knobs()
		mut(&k)
		err := k.Validate()
		if err == nil {
			t.Errorf("%s: invalid knobs accepted", want)
			continue
		}
		if !strings.Contains(err.Error(), strings.Split(strings.Fields(want)[0], ".")[0]) {
			t.Errorf("%s: error %q does not name the field", want, err)
		}
	}
	if err := knobs().Validate(); err != nil {
		t.Fatalf("valid knobs refused: %v", err)
	}
}

// A room's day is one thing for its seed: the same across runs, across two
// calendars, and whatever window it is asked through.
func TestADayIsIdenticalForItsSeedAcrossRunsAndCalendars(t *testing.T) {
	k := knobs()
	from, to := day0.Add(24*time.Hour), day0.Add(48*time.Hour)
	first := Generate(roomA, k, 42, from, to)
	if len(first) == 0 {
		t.Fatal("a day with no entries")
	}
	if got := Generate(roomA, k, 42, from, to); !reflect.DeepEqual(first, got) {
		t.Fatal("two runs differ for one seed")
	}
	c1 := New(42, func(Room) (Knobs, bool) { return k, true })
	c2 := New(42, func(Room) (Knobs, bool) { return k, true })
	if !reflect.DeepEqual(c1.Entries(roomA, from, to, MeetingKinds), c2.Entries(roomA, from, to, MeetingKinds)) {
		t.Fatal("two calendars differ for one seed")
	}
	if !reflect.DeepEqual(first, c1.Entries(roomA, from, to, MeetingKinds)) {
		t.Fatal("the calendar differs from the generator")
	}
	// Asked through a wider window, the day is the same entries.
	wide := Generate(roomA, k, 42, day0, day0.Add(96*time.Hour))
	var inDay []Entry
	for _, e := range wide {
		if e.End.After(from) && e.Start.Before(to) {
			inDay = append(inDay, e)
		}
	}
	if !reflect.DeepEqual(first, inDay) {
		t.Fatal("a day differs with the window it is asked through")
	}
	if reflect.DeepEqual(first, Generate(roomA, k, 43, from, to)) {
		t.Fatal("another seed gave the same day")
	}
	if reflect.DeepEqual(first, Generate(roomB, k, 42, from, to)) {
		t.Fatal("another room gave the same day")
	}
}

func TestAnUnknownRoomHasNoEntries(t *testing.T) {
	c := New(1, func(Room) (Knobs, bool) { return Knobs{}, false })
	if got := c.Entries(roomA, day0, day0.Add(24*time.Hour), MeetingKinds); len(got) != 0 {
		t.Fatalf("entries for a room with no knobs: %v", got)
	}
	if _, ok := c.Now(roomA, day0, MeetingKinds); ok {
		t.Fatal("a meeting in a room with no knobs")
	}
	var nilCal *Calendar
	if _, ok := nilCal.Next(roomA, day0, MeetingKinds); ok {
		t.Fatal("a meeting in a nil calendar")
	}
}

func TestNowAndNext(t *testing.T) {
	k := knobs()
	c := New(7, func(Room) (Knobs, bool) { return k, true })
	es := c.Entries(roomA, day0, day0.Add(24*time.Hour), MeetingKinds)
	if len(es) < 3 {
		t.Fatalf("only %d entries", len(es))
	}
	e := es[1]
	mid := e.Start.Add(e.End.Sub(e.Start) / 2)
	got, ok := c.Now(roomA, mid, MeetingKinds)
	if !ok || !got.Start.Equal(e.Start) {
		t.Fatalf("Now(mid) = %v, %v; want entry at %v", got, ok, e.Start)
	}
	if _, ok := c.Now(roomA, e.End, MeetingKinds); ok {
		next, _ := c.Now(roomA, e.End, MeetingKinds)
		if next.Start.Equal(e.Start) {
			t.Fatal("an entry is still on at its own end")
		}
	}
	nxt, ok := c.Next(roomA, mid, MeetingKinds)
	if !ok || !nxt.Start.After(mid) {
		t.Fatalf("Next(mid) = %v, %v", nxt, ok)
	}
	if nxt.Start.Before(e.End) {
		t.Fatal("Next starts before the running entry ends")
	}
	// Past the last entry of a day, next is the next day's first.
	last := es[len(es)-1]
	n2, ok := c.Next(roomA, last.End, MeetingKinds)
	if !ok || !n2.Start.After(last.End.Add(-time.Second)) || n2.Start.Before(day0.Add(24*time.Hour)) {
		t.Fatalf("Next after the day's last = %v, %v", n2, ok)
	}
}

func TestNoOverlapAndJitterBoundsOverAMonth(t *testing.T) {
	k := knobs()
	k.Hours = Hours{Start: 8, End: 18}
	from, to := day0, day0.AddDate(0, 0, 30)
	for _, r := range []Room{roomA, roomB, {Site: "br1", Building: "main", Room: "huddle-2"}} {
		es := Generate(r, k, 99, from, to)
		for i, e := range es {
			if _, ok := e.AsMeeting(); !ok {
				t.Fatalf("%v: entry %d is no meeting", r, i)
			}
			if !e.End.After(e.Start) {
				t.Fatalf("%v: entry %d ends before it starts", r, i)
			}
			if i > 0 && e.Start.Before(es[i-1].End) {
				t.Fatalf("%v: entry %d starts %v before the one before ends %v", r, i, e.Start, es[i-1].End)
			}
			h := e.Start.Hour()
			if e.Start.Before(day0) || h < 8 || e.End.After(time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 18, 0, 0, 0, time.UTC)) {
				t.Fatalf("%v: entry %d %v to %v outside the hours", r, i, e.Start, e.End)
			}
			m := mt(e)
			if m.Origin == OriginBooked {
				d := m.SlotStart.Sub(e.Start)
				if d > MaxEarlyStart || -d > MaxLateStart {
					t.Fatalf("%v: entry %d start %v from its slot %v is past the jitter", r, i, e.Start, m.SlotStart)
				}
				mins := m.SlotEnd.Sub(m.SlotStart)
				if mins != 15*time.Minute && mins != 30*time.Minute && mins != time.Hour {
					t.Fatalf("slot of %v", mins)
				}
				if m.SlotStart.UnixNano()%int64(mins) != 0 {
					t.Fatalf("slot %v not on a wall-clock boundary of %v", m.SlotStart, mins)
				}
				if e.End.Sub(m.SlotEnd) > MaxLateEnd || m.SlotEnd.Sub(e.End) > MaxEarlyEnd {
					t.Fatalf("end %v beyond the jitter of slot end %v", e.End, m.SlotEnd)
				}
			}
		}
	}
}

// Rates are measured over a simulated month of many rooms against what the
// knobs say, each at the unit it is defined over.
func TestConfiguredRatesHoldOverAMonth(t *testing.T) {
	k := knobs()
	from, to := day0, day0.AddDate(0, 0, 30)
	var booked, noShow, early, late, walkIns, bookedCells, openCells, freeCells int
	rooms := 12
	for i := 0; i < rooms; i++ {
		r := Room{Site: "hq", Building: "tower", Room: "r" + string(rune('a'+i))}
		es := Generate(r, k, 5, from, to)
		for _, e := range es {
			m := mt(e)
			if m.Origin == OriginWalkIn {
				walkIns++
				continue
			}
			booked++
			bookedCells += int(m.SlotEnd.Sub(m.SlotStart) / SlotUnit)
			switch m.Outcome {
			case OutcomeNoShow:
				noShow++
			case OutcomeEndedEarly:
				early++
			case OutcomeEndedLate:
				late++
			}
		}
		// Cells no booking touches are where a walk-in may land.
		for c := from; c.Before(to); c = c.Add(SlotUnit) {
			touched := false
			for _, e := range es {
				if mt(e).Origin == OriginBooked && e.Start.Before(c.Add(SlotUnit)) && e.End.After(c) {
					touched = true
					break
				}
			}
			if !touched {
				freeCells++
			}
		}
		openCells += 30 * 24 * 4
	}
	near := func(name string, got, want, tol float64) {
		t.Helper()
		if math.Abs(got-want) > tol {
			t.Errorf("%s = %.3f, want %.3f +- %.3f", name, got, want, tol)
		}
	}
	// A slot opening at a free boundary is booked with BookingRate: the
	// boundaries opened are the bookings made plus the cells left free.
	near("booking rate", float64(booked)/float64(booked+openCells-bookedCells), k.BookingRate, 0.03)
	near("no-show rate", float64(noShow)/float64(booked), k.NoShowRate, 0.03)
	near("ended early", float64(early)/float64(booked-noShow), k.EndEarlyRate, 0.03)
	near("ended late", float64(late)/float64(booked-noShow), k.EndLateRate, 0.03)
	near("walk-in rate", float64(walkIns)/float64(freeCells), k.WalkInRate, 0.03)
}

func TestNoShowsAreEmptyAndHeldAreAttended(t *testing.T) {
	k := knobs()
	seenNoShow, seenHeld := false, false
	for _, e := range Generate(roomA, k, 3, day0, day0.AddDate(0, 0, 30)) {
		m := mt(e)
		switch {
		case m.Outcome == OutcomeNoShow:
			seenNoShow = true
			if m.Actual != 0 || m.Origin != OriginBooked {
				t.Fatalf("no-show %+v", m)
			}
			if e.Start.Before(m.SlotStart) || e.Start.Sub(m.SlotStart) > MaxLateStart || !e.End.Equal(m.SlotEnd) {
				t.Fatal("a no-show holds its slot as booked")
			}
		default:
			seenHeld = true
			if m.Actual < 1 {
				t.Fatalf("held with nobody: %+v", m)
			}
			if m.Origin == OriginBooked && (m.Expected < k.Attendance.Min || m.Expected > k.Attendance.Max || m.Organizer == "") {
				t.Fatalf("booked meeting %+v outside the knobs", m)
			}
		}
	}
	if !seenNoShow || !seenHeld {
		t.Fatal("a month without both a no-show and a held meeting")
	}
}

func TestZeroRatesProduceNothingAndZeroValueIsInert(t *testing.T) {
	if got := Generate(roomA, Knobs{}, 1, day0, day0.Add(48*time.Hour)); len(got) != 0 {
		t.Fatalf("zero knobs gave %d entries", len(got))
	}
	k := knobs()
	k.BookingRate, k.WalkInRate = 0, 0
	if got := Generate(roomA, k, 1, day0, day0.Add(48*time.Hour)); len(got) != 0 {
		t.Fatalf("zero rates gave %d entries", len(got))
	}
	if got := Generate(roomA, knobs(), 1, day0, day0); len(got) != 0 {
		t.Fatal("an empty window gave entries")
	}
}

func TestMapShape(t *testing.T) {
	k := knobs()
	e := Generate(roomA, k, 3, day0, day0.AddDate(0, 0, 7))[0]
	m := e.Map()
	for _, key := range []string{"kind", "origin", "organizer", "expected", "actual", "outcome", "start", "end", "slot_start", "slot_end", "site", "building", "room"} {
		if _, ok := m[key]; !ok {
			t.Errorf("map lacks %q", key)
		}
	}
	if m["start"] != e.Start.Unix() || m["kind"] != "meeting" {
		t.Fatalf("map = %v", m)
	}
}

// mt is the entry's meeting, or the zero meeting for another kind.
func mt(e Entry) Meeting { m, _ := e.AsMeeting(); return m }
