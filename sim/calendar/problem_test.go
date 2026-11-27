package calendar

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var rm = Room{Site: "hq", Building: "tower", Room: "tower-1-01"}

// november 2026 starts on a Sunday: the 1st is Sunday, Tuesdays fall on
// 3, 10, 17 and 24, Thursdays on 5, 12, 19 and 26.
var nov = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
var dec = nov.AddDate(0, 1, 0)

func starts(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Start.Format("01-02 15:04")+">"+e.End.Format("01-02 15:04"))
	}
	return out
}

func spec(f func(*ProblemSpec)) ProblemSpec {
	p := ProblemSpec{Name: "p", Cadence: "daily", At: "13:30", For: 90 * time.Minute,
		Scope:      Scope{Standard: "mr86", Role: "main-display"},
		Impairment: Impairment{Pin: &Pin{Path: "status.device.power", Value: "off"}}}
	f(&p)
	return p
}

// #2769: each cadence fires and clears exactly on its schedule across a
// simulated month.
func TestProblemCadencesFireExactlyOnSchedule(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    ProblemSpec
		want []string
	}{
		{"weekly on a weekday", spec(func(p *ProblemSpec) { p.Cadence, p.Weekday = "weekly", "tuesday" }),
			[]string{"11-03 13:30>11-03 15:00", "11-10 13:30>11-10 15:00", "11-17 13:30>11-17 15:00", "11-24 13:30>11-24 15:00"}},
		{"monthly on a date", spec(func(p *ProblemSpec) { p.Cadence, p.Day = "monthly", 15 }),
			[]string{"11-15 13:30>11-15 15:00"}},
		{"monthly on the nth weekday", spec(func(p *ProblemSpec) { p.Cadence, p.Nth, p.Weekday = "monthly", 2, "thursday" }),
			[]string{"11-12 13:30>11-12 15:00"}},
		{"a window across midnight", spec(func(p *ProblemSpec) {
			p.Cadence, p.Weekday, p.At, p.For = "weekly", "friday", "23:00", 2*time.Hour
		}), []string{"11-06 23:00>11-07 01:00", "11-13 23:00>11-14 01:00", "11-20 23:00>11-21 01:00", "11-27 23:00>11-28 01:00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := starts(tc.p.entries(rm, 1, nov, dec))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v\nwant %v", got, tc.want)
			}
		})
	}
	t.Run("daily", func(t *testing.T) {
		es := spec(func(*ProblemSpec) {}).entries(rm, 1, nov, dec)
		if len(es) != 30 {
			t.Fatalf("daily fired %d times in November, want 30", len(es))
		}
		for i, e := range es {
			if want := nov.AddDate(0, 0, i).Add(13*time.Hour + 30*time.Minute); !e.Start.Equal(want) || e.End.Sub(e.Start) != 90*time.Minute {
				t.Fatalf("day %d: %v to %v", i+1, e.Start, e.End)
			}
		}
	})
}

// A window opens exactly at its start and closes exactly at its end: the
// entry is active in [start, end), never a second either side.
func TestProblemWindowOpensAndClosesOnTheSecond(t *testing.T) {
	c := New(1, nil).WithProblems(func(Room) []ProblemSpec { return []ProblemSpec{spec(func(*ProblemSpec) {})} })
	open := time.Date(2026, 11, 4, 13, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		at   time.Time
		want int
	}{
		{open.Add(-time.Second), 0}, {open, 1}, {open.Add(89*time.Minute + 59*time.Second), 1}, {open.Add(90 * time.Minute), 0},
	} {
		if got := len(c.Active(rm, tc.at, ProblemKinds)); got != tc.want {
			t.Errorf("at %v: %d problems active, want %d", tc.at.Format("15:04:05"), got, tc.want)
		}
	}
}

// What is on the calendar does not depend on how it is asked for, nor on the
// node asking: a month asked whole equals the same month asked day by day,
// seeded offsets and all, and a second calendar of the same seed agrees.
func TestProblemsAreTheSameWhateverTheWindow(t *testing.T) {
	p := spec(func(p *ProblemSpec) { p.Spread = 45 * time.Minute })
	whole := p.entries(rm, 7, nov, dec)
	var days []Entry
	for d := nov; d.Before(dec); d = d.AddDate(0, 0, 1) {
		days = append(days, p.entries(rm, 7, d, d.AddDate(0, 0, 1))...)
	}
	if !reflect.DeepEqual(starts(whole), starts(days)) {
		t.Fatalf("a month asked whole differs from asked by day:\n%v\n%v", starts(whole), starts(days))
	}
	offsets := map[time.Duration]bool{}
	for _, e := range whole {
		off := e.Start.Sub(time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 13, 30, 0, 0, time.UTC))
		if off < 0 || off > 45*time.Minute || off%time.Minute != 0 {
			t.Fatalf("offset %v is outside the cadence's spread or off the minute", off)
		}
		offsets[off] = true
	}
	if len(offsets) < 5 {
		t.Errorf("the spread produced only %d distinct offsets over a month", len(offsets))
	}
	other := p.entries(rm, 7, nov, dec)
	if !reflect.DeepEqual(starts(whole), starts(other)) {
		t.Fatal("the same seed gave a different calendar")
	}
	if reflect.DeepEqual(starts(whole), starts(p.entries(rm, 8, nov, dec))) {
		t.Error("a different seed gave the same offsets")
	}
	if reflect.DeepEqual(starts(whole), starts(p.entries(Room{Site: "hq", Building: "tower", Room: "tower-1-02"}, 7, nov, dec))) {
		t.Error("two rooms broke at the same instants every day: the spread is not per room")
	}
}

// A problem is its own kind beside the meetings, and a calendar with none is
// untouched: Entries of the meeting kind is meetings alone, of every kind is everything in start order.
func TestACalendarCarriesProblemsBesideMeetings(t *testing.T) {
	k := Knobs{BookingRate: 0.6, SlotMix: map[int]float64{30: 1}, Attendance: Range{Min: 2, Max: 4}, Hours: Hours{Start: 8, End: 18}}
	knobs := func(Room) (Knobs, bool) { return k, true }
	plain := New(3, knobs)
	with := plain.WithProblems(func(Room) []ProblemSpec { return []ProblemSpec{spec(func(*ProblemSpec) {})} })

	day := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	if !reflect.DeepEqual(plain.Entries(rm, day, day.AddDate(0, 0, 1), MeetingKinds), with.Entries(rm, day, day.AddDate(0, 0, 1), MeetingKinds)) {
		t.Fatal("problems moved the meetings")
	}
	all := with.Entries(rm, day, day.AddDate(0, 0, 1), AllKinds)
	var meetings, problems int
	for i, e := range all {
		if i > 0 && e.Start.Before(all[i-1].Start) {
			t.Fatal("every kind is not in start order")
		}
		switch e.Kind {
		case KindMeeting:
			meetings++
			_, isProblem := e.AsProblem()
			if _, ok := e.AsMeeting(); !ok || isProblem {
				t.Fatal("a meeting entry with the wrong payload")
			}
		case KindProblem:
			problems++
			_, isMeeting := e.AsMeeting()
			if p, ok := e.AsProblem(); !ok || isMeeting || p.Role != "main-display" || p.Name != "p" {
				t.Fatalf("a problem entry with the wrong payload: %+v", e)
			}
		}
	}
	if meetings == 0 || problems != 1 {
		t.Fatalf("%d meetings and %d problems in a day, want some and one", meetings, problems)
	}
	if got := plain.Entries(rm, day, day.AddDate(0, 0, 1), AllKinds); len(got) != meetings {
		t.Fatalf("a calendar with no problems holds %d entries, want %d", len(got), meetings)
	}
	var nilCal *Calendar
	if nilCal.Entries(rm, day, day.AddDate(0, 0, 1), AllKinds) != nil || nilCal.Active(rm, day, AllKinds) != nil {
		t.Fatal("a nil calendar holds entries")
	}
}

func TestAProblemEntryReadsAsAMap(t *testing.T) {
	e := spec(func(p *ProblemSpec) { p.Impairment = Impairment{Unplugged: true} }).entries(rm, 1, nov, nov.AddDate(0, 0, 1))[0]
	m := e.Map()
	if m["kind"] != "problem" || m["name"] != "p" || m["role"] != "main-display" || m["unplugged"] != true || m["site"] != "hq" {
		t.Fatalf("map = %v", m)
	}
	if _, ok := m["outcome"]; ok {
		t.Error("a problem carries a meeting's fields")
	}
	pin := spec(func(*ProblemSpec) {}).entries(rm, 1, nov, nov.AddDate(0, 0, 1))[0].Map()
	if pin["pin_path"] != "status.device.power" || pin["pin_value"] != "off" {
		t.Fatalf("pin map = %v", pin)
	}
}

func TestProblemSpecsAreRefusedNamingTheField(t *testing.T) {
	for name, tc := range map[string]struct {
		f    func(*ProblemSpec)
		want string
	}{
		"no name":            {func(p *ProblemSpec) { p.Name = "" }, "name"},
		"an unknown cadence": {func(p *ProblemSpec) { p.Cadence = "hourly" }, "cadence"},
		"weekly no weekday":  {func(p *ProblemSpec) { p.Cadence = "weekly" }, "weekday"},
		"a bad weekday":      {func(p *ProblemSpec) { p.Cadence, p.Weekday = "weekly", "someday" }, "weekday"},
		"daily with a day":   {func(p *ProblemSpec) { p.Day = 3 }, "day"},
		"monthly nothing":    {func(p *ProblemSpec) { p.Cadence = "monthly" }, "day or an nth weekday"},
		"monthly both":       {func(p *ProblemSpec) { p.Cadence, p.Day, p.Nth, p.Weekday = "monthly", 3, 2, "monday" }, "not both"},
		"day 31":             {func(p *ProblemSpec) { p.Cadence, p.Day = "monthly", 31 }, "day"},
		"nth 5":              {func(p *ProblemSpec) { p.Cadence, p.Nth, p.Weekday = "monthly", 5, "monday" }, "nth"},
		"a bad time":         {func(p *ProblemSpec) { p.At = "25:00" }, "at"},
		"no duration":        {func(p *ProblemSpec) { p.For = 0 }, "for"},
		"a duration of days": {func(p *ProblemSpec) { p.For = 25 * time.Hour }, "for"},
		"a negative spread":  {func(p *ProblemSpec) { p.Spread = -time.Minute }, "spread"},
		"no scope":           {func(p *ProblemSpec) { p.Scope = Scope{Role: "main-display"} }, "scope"},
		"no role":            {func(p *ProblemSpec) { p.Scope.Role = "" }, "role"},
		"no impairment":      {func(p *ProblemSpec) { p.Impairment = Impairment{} }, "impairment"},
		"two impairments":    {func(p *ProblemSpec) { p.Impairment.Unplugged = true }, "exactly one"},
		"loss past 100":      {func(p *ProblemSpec) { p.Impairment = Impairment{LossPct: 101} }, "loss_pct"},
		"a pin with no path": {func(p *ProblemSpec) { p.Impairment = Impairment{Pin: &Pin{Value: "1"}} }, "pin"},
	} {
		t.Run(name, func(t *testing.T) {
			p := spec(tc.f)
			err := p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
	if err := spec(func(*ProblemSpec) {}).Validate(); err != nil {
		t.Fatalf("a good spec is refused: %v", err)
	}
}
