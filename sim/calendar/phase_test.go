package calendar

import (
	"strings"
	"testing"
	"time"
)

// phaseKnobs and phaseSeed pin a room day with every case the phase has to
// tell apart, hand-checked below against the printed entries (UTC, 3 Nov 2026):
//
//	08:28:07-09:00:01  booked, held, 3 attend, 31m54s: wakes 2m before, shares its middle third
//	09:32:41-09:45:00  walk-in, held: no lead, too short to share
//	10:03:55-10:57:48  booked, ended early, 6 attend, 53m53s: shares its middle third
//	11:00:58-11:15:00  walk-in, 3m10s after the one above: its cooldown runs into this start
//	12:01:45-12:15:00  walk-in
//	12:34:09-13:00:12  booked, held, 26m03s: too short to share
//	13:45:00-14:00:00  booked, no-show: holds its slot, wakes and cools nothing
func phaseKnobs() Knobs {
	return Knobs{BookingRate: 0.5, SlotMix: map[int]float64{15: 1, 30: 2, 60: 2}, NoShowRate: 0.2, EndEarlyRate: 0.2, EndLateRate: 0.15,
		Attendance: Range{Min: 2, Max: 6}, WalkInRate: 0.25, Hours: Hours{Start: 8, End: 14}, RecordingRate: 0.5}
}

const phaseSeed = 24

var phaseDay = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

// phaseCal is the pinned room day plus two scripted problems: a morning one
// over the first meeting and its cooldown, and an afternoon one over the
// no-show.
func phaseCal(k Knobs) *Calendar {
	c := New(phaseSeed, func(Room) (Knobs, bool) { return k, true })
	return c.WithProblems(func(Room) []ProblemSpec {
		return []ProblemSpec{
			spec(func(p *ProblemSpec) { p.Name, p.At, p.For = "morning", "08:40", 30*time.Minute }),
			spec(func(p *ProblemSpec) { p.Name, p.At, p.For = "afternoon", "13:30", 90*time.Minute }),
		}
	})
}

// spans reads "HH:MM phase" lines as the phase from that minute until the
// next line, to 15:30, one value per minute of the day's 08:00 to 15:30.
func spans(t *testing.T, lines string) map[int]Phase {
	t.Helper()
	type seg struct {
		min int
		p   Phase
	}
	var segs []seg
	for _, l := range strings.Split(strings.TrimSpace(lines), "\n") {
		f := strings.Fields(l)
		tm, err := time.Parse("15:04", f[0])
		if err != nil {
			t.Fatal(err)
		}
		segs = append(segs, seg{tm.Hour()*60 + tm.Minute(), Phase(f[1])})
	}
	out := map[int]Phase{}
	for i, s := range segs {
		end := 15*60 + 30
		if i+1 < len(segs) {
			end = segs[i+1].min
		}
		for m := s.min; m < end; m++ {
			out[m] = s.p
		}
	}
	return out
}

// The oracle, worked out by hand from the entries above with a 120 s wake
// lead, a 300 s cooldown and the middle third of a meeting of 30 minutes or
// more sharing: the room's occupancy at the top of every minute, no problem
// considered.
const occupancyOracle = `
08:00 idle
08:27 waking
08:29 in-meeting
08:39 sharing
08:50 in-meeting
09:01 cooldown
09:06 idle
09:33 in-meeting
09:45 cooldown
09:50 idle
10:02 waking
10:04 in-meeting
10:22 sharing
10:40 in-meeting
10:58 cooldown
11:01 in-meeting
11:15 cooldown
11:20 idle
12:02 in-meeting
12:15 cooldown
12:20 idle
12:33 waking
12:35 in-meeting
13:01 cooldown
13:06 idle
`

// The same day with the two scripted problems read as the impaired phase:
// 08:40 to 09:10 (over the first meeting's sharing and cooldown) and 13:30 to
// 15:00 (over the no-show).
const phaseOracle = `
08:00 idle
08:27 waking
08:29 in-meeting
08:39 sharing
08:40 impaired
09:10 idle
09:33 in-meeting
09:45 cooldown
09:50 idle
10:02 waking
10:04 in-meeting
10:22 sharing
10:40 in-meeting
10:58 cooldown
11:01 in-meeting
11:15 cooldown
11:20 idle
12:02 in-meeting
12:15 cooldown
12:20 idle
12:33 waking
12:35 in-meeting
13:01 cooldown
13:06 idle
13:30 impaired
15:00 idle
`

func TestRoomPhaseTimelineMatchesTheHandCheckedOracleAtEveryMinute(t *testing.T) {
	c := phaseCal(phaseKnobs())
	occ, ph := spans(t, occupancyOracle), spans(t, phaseOracle)
	for m := 8 * 60; m < 15*60+30; m++ {
		at := phaseDay.Add(time.Duration(m) * time.Minute)
		s := c.StateAt(rm, at)
		if s.Occupancy != occ[m] {
			t.Errorf("%s: occupancy %s, want %s", at.Format("15:04"), s.Occupancy, occ[m])
		}
		if s.Phase != ph[m] {
			t.Errorf("%s: phase %s, want %s", at.Format("15:04"), s.Phase, ph[m])
		}
		if s.Impaired != (ph[m] == PhaseImpaired) {
			t.Errorf("%s: impaired %v in phase %s", at.Format("15:04"), s.Impaired, ph[m])
		}
		if want := occ[m] != PhaseIdle; s.InUse != want {
			t.Errorf("%s: in use %v, want %v", at.Format("15:04"), s.InUse, want)
		}
		if want := occ[m] == PhaseInMeeting || occ[m] == PhaseSharing; s.CallActive != want {
			t.Errorf("%s: call active %v, want %v", at.Format("15:04"), s.CallActive, want)
		}
		if want := occ[m] == PhaseSharing; s.Sharing != want {
			t.Errorf("%s: sharing %v, want %v", at.Format("15:04"), s.Sharing, want)
		}
		if !s.HasCalendar {
			t.Errorf("%s: a room with knobs reads no calendar", at.Format("15:04"))
		}
	}
}

// The seconds the minute marks cannot see: the edges of the first meeting,
// the one the cooldown runs into, and the no-show.
func TestRoomPhaseEdgesAreExactToTheSecond(t *testing.T) {
	c := New(phaseSeed, func(Room) (Knobs, bool) { return phaseKnobs(), true })
	at := func(hms string) time.Time {
		tm, err := time.Parse("15:04:05", hms)
		if err != nil {
			t.Fatal(err)
		}
		return phaseDay.Add(time.Duration(tm.Hour())*time.Hour + time.Duration(tm.Minute())*time.Minute + time.Duration(tm.Second())*time.Second)
	}
	for _, tc := range []struct {
		at   string
		want Phase
		who  int
	}{
		{"08:26:06", PhaseIdle, 0}, {"08:26:07", PhaseWaking, 0}, {"08:28:06", PhaseWaking, 0}, {"08:28:07", PhaseInMeeting, 3},
		{"08:38:44", PhaseInMeeting, 3}, {"08:38:45", PhaseSharing, 3}, {"08:49:22", PhaseSharing, 3}, {"08:49:23", PhaseInMeeting, 3},
		{"09:00:00", PhaseInMeeting, 3}, {"09:00:01", PhaseCooldown, 0}, {"09:05:00", PhaseCooldown, 0}, {"09:05:01", PhaseIdle, 0},
		{"09:32:41", PhaseInMeeting, 1}, // a walk-in wakes nothing ahead of itself
		{"09:32:40", PhaseIdle, 0},
		{"10:57:47", PhaseInMeeting, 6}, {"10:57:48", PhaseCooldown, 0}, {"11:00:57", PhaseCooldown, 0}, {"11:00:58", PhaseInMeeting, 2},
		{"13:44:00", PhaseIdle, 0}, {"13:45:00", PhaseIdle, 0}, {"14:00:00", PhaseIdle, 0}, // the no-show uses nothing
	} {
		s := c.StateAt(rm, at(tc.at))
		if s.Phase != tc.want || s.Attendees != tc.who {
			t.Errorf("%s: phase %s with %d attending, want %s with %d", tc.at, s.Phase, s.Attendees, tc.want, tc.who)
		}
	}
	// the no-show is the meeting running, with nobody in it
	s := c.StateAt(rm, at("13:50:00"))
	if !s.HasMeeting || s.CallActive || s.Attendees != 0 || s.InUse {
		t.Errorf("no-show: meeting %v call %v attendees %d in use %v", s.HasMeeting, s.CallActive, s.Attendees, s.InUse)
	}
	if s := c.StateAt(rm, at("08:28:08")); s.UseUntil != at("09:00:01").Add(300*time.Second).Unix() {
		t.Errorf("use until %v, want the end plus the cooldown", s.UseUntil)
	}
}

// The wake lead, the cooldown and the sharing stretch come from the knobs,
// and unset ones read today's defaults.
func TestRoomPhaseTimingComesFromTheKnobs(t *testing.T) {
	if tm := (Knobs{}).Timing(); tm.WakeLead != 120*time.Second || tm.CooldownTail != 300*time.Second ||
		tm.RecordingTail != 600*time.Second || tm.Sharing != (Sharing{MinMinutes: 30, Parts: 3, FromPart: 1, ToPart: 2}) {
		t.Fatalf("defaults = %+v", tm)
	}
	i := func(v int) *int { return &v }
	k := phaseKnobs()
	k.WakeLead, k.CooldownTail = i(600), i(0)
	k.Sharing = &Sharing{MinMinutes: 20, Parts: 2, FromPart: 0, ToPart: 1}
	c := New(phaseSeed, func(Room) (Knobs, bool) { return k, true })
	at := func(h, m, s int) time.Time {
		return phaseDay.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second)
	}
	for _, tc := range []struct {
		at   time.Time
		want Phase
	}{
		{at(8, 18, 6), PhaseIdle}, {at(8, 18, 7), PhaseWaking}, // ten minutes of lead
		{at(8, 28, 7), PhaseSharing}, // the first half of the meeting shares
		{at(8, 44, 3), PhaseSharing}, {at(8, 44, 4), PhaseInMeeting},
		{at(9, 0, 1), PhaseIdle}, // no cooldown
		{at(9, 33, 0), PhaseInMeeting},
	} {
		if got := c.StateAt(rm, tc.at).Occupancy; got != tc.want {
			t.Errorf("%s: %s, want %s", tc.at.Format("15:04:05"), got, tc.want)
		}
	}
}

func TestRoomPhaseTimingIsValidated(t *testing.T) {
	i := func(v int) *int { return &v }
	good := phaseKnobs()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Knobs){
		"negative lead":      func(k *Knobs) { k.WakeLead = i(-1) },
		"a lead past a day":  func(k *Knobs) { k.WakeLead = i(86401) },
		"negative cooldown":  func(k *Knobs) { k.CooldownTail = i(-5) },
		"negative recording": func(k *Knobs) { k.RecordingTail = i(-5) },
		"no sharing minimum": func(k *Knobs) { k.Sharing = &Sharing{Parts: 3, FromPart: 1, ToPart: 2} },
		"no parts":           func(k *Knobs) { k.Sharing = &Sharing{MinMinutes: 30, FromPart: 0, ToPart: 1} },
		"span past parts":    func(k *Knobs) { k.Sharing = &Sharing{MinMinutes: 30, Parts: 3, FromPart: 1, ToPart: 4} },
		"empty span":         func(k *Knobs) { k.Sharing = &Sharing{MinMinutes: 30, Parts: 3, FromPart: 2, ToPart: 2} },
	} {
		k := phaseKnobs()
		mut(&k)
		if err := k.Validate(); err == nil {
			t.Errorf("%s: knobs validate", name)
		}
	}
}

// A room with no knobs, and no calendar, is idle with HasCalendar false: the
// fallback every reader keys on.
func TestRoomPhaseOfARoomWithNoCalendarIsIdleAndSaysSo(t *testing.T) {
	var nilCal *Calendar
	c := New(1, func(Room) (Knobs, bool) { return Knobs{}, false })
	for name, cal := range map[string]*Calendar{"nil": nilCal, "no knobs": c} {
		s := cal.StateAt(rm, phaseDay.Add(10*time.Hour))
		if s.HasCalendar || s.InUse || s.Phase != PhaseIdle || s.HasMeeting || s.Scheduled {
			t.Errorf("%s: %+v", name, s)
		}
		if m := s.Map(); m["has_calendar"] != false || m["meeting"] != nil || m["phase"] != "idle" {
			t.Errorf("%s: map %v", name, m)
		}
	}
}

// A recorded meeting is still pending for the recording tail after it ends.
func TestRoomPhaseRecordingStaysPendingForTheTail(t *testing.T) {
	c := New(phaseSeed, func(Room) (Knobs, bool) { k := phaseKnobs(); k.RecordingRate = 1; return k, true })
	end := time.Date(2026, 11, 3, 9, 0, 1, 0, time.UTC)
	for _, tc := range []struct {
		at   time.Time
		want bool
	}{{end.Add(-time.Second), false}, {end, true}, {end.Add(600 * time.Second), true}, {end.Add(601 * time.Second), false}} {
		if got := c.StateAt(rm, tc.at).RecordingPending; got != tc.want {
			t.Errorf("%s: pending %v, want %v", tc.at.Format("15:04:05"), got, tc.want)
		}
	}
}

// The two readings every room device shared before the phase, now the phase's
// own: a held meeting of half an hour or more shares through its middle third,
// and a room is in use from the wake lead before a booked meeting someone
// attends (at a walk-in's start) to the cooldown tail after it.
func TestMeetingSharesThroughTheMiddleThirdOfALongHeldMeeting(t *testing.T) {
	u := func(s int64) time.Time { return time.Unix(s, 0).UTC() }
	state := func(m Meeting, start, end, at int64) RoomState {
		return meetingPhase(RoomState{Phase: PhaseIdle, Occupancy: PhaseIdle},
			[]Entry{MeetingEntry(u(start), u(end), rm, m)}, u(at), (Knobs{}).Timing())
	}
	held := Meeting{Outcome: OutcomeHeld, Origin: OriginBooked}
	for _, c := range []struct {
		at   int64
		want bool
	}{{1000, false}, {1000 + 1199, false}, {1000 + 1200, true}, {1000 + 2399, true}, {1000 + 2400, false}, {1000 + 3600, false}, {900, false}} {
		if got := state(held, 1000, 1000+3600, c.at).Sharing; got != c.want {
			t.Errorf("at +%ds: shares %v, want %v", c.at-1000, got, c.want)
		}
	}
	if state(held, 0, 15*60, 7*60).Sharing {
		t.Error("a 15-minute meeting shares content")
	}
	if state(Meeting{Outcome: OutcomeNoShow, Origin: OriginBooked}, 0, 3600, 1800).Sharing {
		t.Error("a no-show shares content")
	}
}

func TestRoomInUseWindow(t *testing.T) {
	u := func(s int64) time.Time { return time.Unix(s, 0).UTC() }
	ms := []Entry{
		MeetingEntry(u(1000), u(2000), rm, Meeting{Outcome: OutcomeHeld, Origin: OriginBooked}),
		MeetingEntry(u(5000), u(5500), rm, Meeting{Outcome: OutcomeHeld, Origin: OriginWalkIn}),
		MeetingEntry(u(8000), u(9000), rm, Meeting{Outcome: OutcomeNoShow, Origin: OriginBooked}),
	}
	for _, c := range []struct {
		at   int64
		want bool
	}{{879, false}, {880, true}, {2299, true}, {2300, false}, {4999, false}, {5000, true}, {5799, true}, {5800, false}, {8500, false}} {
		got := meetingPhase(RoomState{Phase: PhaseIdle, Occupancy: PhaseIdle}, ms, u(c.at), (Knobs{}).Timing()).InUse
		if got != c.want {
			t.Errorf("at %d: in use %v, want %v", c.at, got, c.want)
		}
	}
}
