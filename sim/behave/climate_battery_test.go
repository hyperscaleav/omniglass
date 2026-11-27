package behave_test

import (
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

func TestClimateMovesTowardTheLoadOfTheRoomsAttendees(t *testing.T) {
	still := &stub{frac: 0.5} // no noise
	base := behave.ClimateState{TemperatureC: 21.5, ClimateC: 21.5, SetpointC: 21.5, LoadCPerPerson: 0.25}
	for _, tc := range []struct {
		name string
		prev behave.ClimateState
		hms  string
		want float64
	}{
		{"booked of 6: a fifth of the way to setpoint plus 1.5", base, "10:20:00", 21.8},
		{"booked of 3 (sharing stretch): its own load", base, "08:45:00", 21.65},
		{"waking: in use, nobody yet, the setpoint", base, "08:27:00", 21.5},
		{"cooldown: in use, nobody, relaxing from warm", behave.ClimateState{ClimateC: 23, SetpointC: 21.5, LoadCPerPerson: 0.25}, "09:03:00", 22.7},
		{"idle: relaxing from warm", behave.ClimateState{ClimateC: 23, SetpointC: 21.5, LoadCPerPerson: 0.25}, "09:20:00", 22.7},
		{"no-show: nobody", behave.ClimateState{ClimateC: 23, SetpointC: 21.5, LoadCPerPerson: 0.25}, "13:50:00", 22.7},
		{"the load is capped", behave.ClimateState{ClimateC: 21.5, SetpointC: 21.5, LoadCPerPerson: 5}, "10:20:00", 22.3},
	} {
		got := behave.Climate(tc.prev, roomAt(tc.hms), behave.DefaultClimate, still)
		if math.Abs(got.ClimateC-tc.want) > 1e-9 {
			t.Errorf("%s: climate %v, want %v", tc.name, got.ClimateC, tc.want)
		}
		if got.TemperatureC != behave.Round(1, tc.prev.ClimateC) {
			t.Errorf("%s: reading %v is not the value before the move", tc.name, got.TemperatureC)
		}
	}
}

func TestClimateWithNoCalendarSitsAtTheSetpointWithNoise(t *testing.T) {
	prev := behave.ClimateState{ClimateC: 24, SetpointC: 21.5}
	got := behave.Climate(prev, noCalendar, behave.DefaultClimate, &stub{frac: 1})
	if got.ClimateC != 21.5 || got.TemperatureC != 24.1 {
		t.Fatalf("%+v, want the setpoint and a reading of 24 plus 0.1 of noise", got)
	}
}

func TestBatteryDrainsInMeetingsAndChargesBetween(t *testing.T) {
	now := at("10:00:00")
	min := func(m int) int64 { return now.Add(time.Duration(m) * time.Minute).Unix() }
	for _, tc := range []struct {
		name     string
		meetings []behave.Span
		want     float64
	}{
		{"no meetings: full", nil, 100},
		{"half an hour that ended ten minutes ago: 12 down, 8 back", []behave.Span{{Start: min(-40), End: min(-10), Actual: 3}}, 96},
		{"running for an hour: 24 down", []behave.Span{{Start: min(-60), End: min(30), Actual: 3}}, 76},
		{"a no-show drains nothing", []behave.Span{{Start: min(-60), End: min(30), Actual: 0}}, 100},
		{"two with a gap: 12 down, 4 back, 4 down, 4 back", []behave.Span{{Start: min(-50), End: min(-20), Actual: 2}, {Start: min(-15), End: min(-5), Actual: 2}}, 92},
		{"a meeting that began before the window counts from its start", []behave.Span{{Start: now.Add(-49 * time.Hour).Unix(), End: now.Add(-47 * time.Hour).Unix(), Actual: 2}}, 100},
	} {
		got := behave.Battery(50, true, tc.meetings, now, behave.DefaultBattery)
		if got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBatteryIsIndependentOfItsPreviousReadingWithACalendar(t *testing.T) {
	now := at("10:00:00")
	a := behave.Battery(12, true, nil, now, behave.DefaultBattery)
	b := behave.Battery(99, true, nil, now, behave.DefaultBattery)
	if a != b {
		t.Fatalf("%v and %v: a calendar battery depends on what it read before", a, b)
	}
}

func TestBatteryWithNoCalendarDrainsATenthAMinute(t *testing.T) {
	for _, tc := range []struct{ prev, want float64 }{{78, 77.9}, {0.05, 0}, {100, 99.9}} {
		if got := behave.Battery(tc.prev, false, nil, at("10:00:00"), behave.DefaultBattery); got != tc.want {
			t.Errorf("%v: %v, want %v", tc.prev, got, tc.want)
		}
	}
}

func TestSpansBetweenReadsTheRoomsMeetingsOfThePinnedDay(t *testing.T) {
	spans := behave.SpansBetween(pinnedCalendar(), room, day, day.Add(24*time.Hour))
	if len(spans) != 7 {
		t.Fatalf("%d meetings, want the pinned day's 7", len(spans))
	}
	first, last := spans[0], spans[6]
	if first.Start != at("08:28:07").Unix() || first.End != at("09:00:01").Unix() || first.Actual != 3 {
		t.Errorf("first %+v", first)
	}
	if last.Start != at("13:45:00").Unix() || last.Actual != 0 {
		t.Errorf("the no-show %+v, want nobody", last)
	}
}
