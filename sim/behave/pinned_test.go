package behave_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// The pinned room day every table reads (UTC, 3 Nov 2026), hand-checked in
// the calendar's own phase tests:
//
//	08:28:07-09:00:01  booked, held, 3 attend: wakes at 08:26:07, shares 08:38:45-08:49:23
//	09:32:41-09:45:00  walk-in, 1 attends
//	10:03:55-10:57:48  booked, ended early, 6 attend
//	11:00:58-11:15:00  walk-in, 2 attend: back to back, the cooldown of the one above runs into it
//	12:01:45-12:15:00  walk-in
//	12:34:09-13:00:12  booked, held, 26 minutes: too short to share
//	13:45:00-14:00:00  booked, no-show: holds its slot, nobody in it
//
// plus two scripted problems: 08:40-09:10 (over the first meeting's sharing)
// and 13:30-15:00 (over the no-show).
var (
	room = calendar.Room{Site: "hq", Building: "tower", Room: "tower-1-01"}
	day  = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
)

const pinnedSeed = 24

func pinnedKnobs() calendar.Knobs {
	return calendar.Knobs{BookingRate: 0.5, SlotMix: map[int]float64{15: 1, 30: 2, 60: 2}, NoShowRate: 0.2, EndEarlyRate: 0.2, EndLateRate: 0.15,
		Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.25, Hours: calendar.Hours{Start: 8, End: 14}, RecordingRate: 0.5}
}

func pinnedCalendar() *calendar.Calendar {
	c := calendar.New(pinnedSeed, func(calendar.Room) (calendar.Knobs, bool) { return pinnedKnobs(), true })
	return c.WithProblems(func(calendar.Room) []calendar.ProblemSpec {
		return []calendar.ProblemSpec{
			{Name: "morning", Cadence: "daily", At: "08:40", For: 30 * time.Minute, Scope: calendar.Scope{Role: "main-display"},
				Impairment: calendar.Impairment{Pin: &calendar.Pin{Path: "status.device.power", Value: "off"}}},
			{Name: "afternoon", Cadence: "daily", At: "13:30", For: 90 * time.Minute, Scope: calendar.Scope{Role: "main-display"},
				Impairment: calendar.Impairment{Pin: &calendar.Pin{Path: "status.device.power", Value: "off"}}},
		}
	})
}

// at is the instant hh:mm:ss on the pinned day.
func at(hms string) time.Time {
	tm, err := time.Parse("15:04:05", hms)
	if err != nil {
		panic(err)
	}
	return day.Add(time.Duration(tm.Hour())*time.Hour + time.Duration(tm.Minute())*time.Minute + time.Duration(tm.Second())*time.Second)
}

// roomAt is the room's state at hh:mm:ss on the pinned day.
func roomAt(hms string) calendar.RoomState { return pinnedCalendar().StateAt(room, at(hms)) }

// noCalendar is what a device whose room holds no calendar reads.
var noCalendar = calendar.RoomState{Phase: calendar.PhaseIdle, Occupancy: calendar.PhaseIdle}

// stub is a Source that answers every draw the same way, so a table row can
// name the draw it wants: chance is every Chance, frac where in [lo, hi] every
// Uniform lands, and pickHi whether IntN lands on hi or lo.
type stub struct {
	chance bool
	frac   float64
	pickHi bool
	draws  int
}

func (s *stub) Uniform(lo, hi float64) float64 { s.draws++; return lo + s.frac*(hi-lo) }
func (s *stub) IntN(lo, hi int64) int64 {
	s.draws++
	if s.pickHi {
		return max(lo, hi)
	}
	return min(lo, hi)
}
func (s *stub) Chance(float64) bool { s.draws++; return s.chance }

// never draws nothing and fails the test if asked to.
type never struct{ t *testing.T }

func (n never) Uniform(float64, float64) float64 { n.t.Helper(); n.t.Fatal("drew a uniform"); return 0 }
func (n never) IntN(int64, int64) int64          { n.t.Helper(); n.t.Fatal("drew an int"); return 0 }
func (n never) Chance(float64) bool              { n.t.Helper(); n.t.Fatal("drew a chance"); return false }

var _ behave.Source = (*stub)(nil)
var _ behave.Source = never{}
