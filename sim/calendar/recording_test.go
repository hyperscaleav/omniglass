package calendar

import (
	"math"
	"strings"
	"testing"
	"time"
)

// A meeting is recorded or not as a fact of the lab calendar (#2775): a
// share of the meetings that took place, a pure function of the seed, the
// room and the meeting, never accumulated state.

func recKnobs(rate float64) Knobs {
	k := knobs()
	k.RecordingRate = rate
	return k
}

func TestRecordingRateIsValidated(t *testing.T) {
	for _, bad := range []float64{-0.1, 1.5, math.NaN()} {
		k := recKnobs(bad)
		err := k.Validate()
		if err == nil || !strings.Contains(err.Error(), "recording_rate") {
			t.Errorf("recording_rate %v: err %v, want one naming recording_rate", bad, err)
		}
	}
	for _, ok := range []float64{0, 0.3, 1} {
		k := recKnobs(ok)
		if err := k.Validate(); err != nil {
			t.Errorf("recording_rate %v refused: %v", ok, err)
		}
	}
}

// A no-show is never recorded; rate 1 records every meeting that happened;
// rate 0 (the zero value) records none.
func TestNoShowIsNeverRecordedAndTheExtremesHold(t *testing.T) {
	for _, tc := range []struct {
		rate     float64
		wantHeld bool // every meeting that took place is recorded
	}{{0, false}, {1, true}} {
		var held, recorded int
		for _, e := range Generate(roomA, recKnobs(tc.rate), 4, day0, day0.AddDate(0, 0, 14)) {
			if mt(e).Outcome == OutcomeNoShow {
				if mt(e).Recorded {
					t.Fatalf("a no-show at %s is recorded", e.Start)
				}
				continue
			}
			held++
			if mt(e).Recorded {
				recorded++
			}
		}
		if held == 0 {
			t.Fatal("no meetings held")
		}
		if tc.wantHeld && recorded != held || !tc.wantHeld && recorded != 0 {
			t.Fatalf("rate %v: %d of %d held meetings recorded", tc.rate, recorded, held)
		}
	}
}

// Over a simulated month across rooms the recorded share of held meetings
// holds near the rate, and recording a meeting never moves any other draw:
// the same entries, times and attendance with the knob on and off.
func TestRecordedShareHoldsOverAMonthAndMovesNothingElse(t *testing.T) {
	const rate = 0.35
	month := day0.AddDate(0, 0, 30)
	var held, recorded int
	for r := 0; r < 40; r++ {
		room := Room{Site: "hq", Building: "tower", Room: "r" + string(rune('a'+r%26)) + string(rune('a'+r/26))}
		on := Generate(room, recKnobs(rate), 9, day0, month)
		off := Generate(room, recKnobs(0), 9, day0, month)
		if len(on) != len(off) {
			t.Fatalf("recording_rate changed the number of entries: %d vs %d", len(on), len(off))
		}
		for i, e := range on {
			o := off[i]
			if !e.Start.Equal(o.Start) || !e.End.Equal(o.End) || mt(e).Actual != mt(o).Actual || mt(e).Outcome != mt(o).Outcome || mt(e).Organizer != mt(o).Organizer {
				t.Fatalf("recording_rate moved entry %d: %+v vs %+v", i, e, o)
			}
			if mt(e).Outcome != OutcomeNoShow {
				held++
				if mt(e).Recorded {
					recorded++
				}
			}
		}
	}
	if held < 1000 {
		t.Fatalf("only %d held meetings to measure", held)
	}
	if got := float64(recorded) / float64(held); math.Abs(got-rate) > 0.04 {
		t.Fatalf("recorded share %.3f of %d held meetings, want near %v", got, held, rate)
	}
}

// Whether a meeting is recorded is a function of the calendar alone: asked
// through any window, by another calendar of the same seed, the answer is the
// same; and another seed answers differently.
func TestRecordedIsTheSameWhateverTheWindow(t *testing.T) {
	k := recKnobs(0.5)
	whole := Generate(roomA, k, 5, day0, day0.AddDate(0, 0, 7))
	byDay := map[time.Time]bool{}
	for _, e := range whole {
		byDay[e.Start] = mt(e).Recorded
	}
	for d := 0; d < 7; d++ {
		from := day0.AddDate(0, 0, d)
		for _, e := range Generate(roomA, k, 5, from.Add(9*time.Hour), from.Add(11*time.Hour)) {
			if byDay[e.Start] != mt(e).Recorded {
				t.Fatalf("%s: recorded %v through a two-hour window, %v through the week", e.Start, mt(e).Recorded, byDay[e.Start])
			}
		}
	}
	differs := false
	for _, e := range whole {
		if recordedDraw(6, roomA, mt(e).SlotStart) != recordedDraw(5, roomA, mt(e).SlotStart) {
			differs = true
		}
	}
	if !differs {
		t.Fatal("two seeds draw the same")
	}
}

func TestMapCarriesRecorded(t *testing.T) {
	for _, e := range Generate(roomA, recKnobs(1), 3, day0, day0.AddDate(0, 0, 3)) {
		if mt(e).Outcome == OutcomeNoShow {
			continue
		}
		if m := e.Map(); m["recorded"] != true {
			t.Fatalf("map recorded = %v, want true", m["recorded"])
		}
		return
	}
	t.Fatal("no held meeting")
}

// Prev is the last entry that has ended at or before t.
func TestPrev(t *testing.T) {
	c := New(2, func(Room) (Knobs, bool) { return knobs(), true })
	es := c.Entries(roomA, day0, day0.AddDate(0, 0, 2), MeetingKinds)
	if len(es) < 3 {
		t.Fatal("too few entries")
	}
	at := es[2].End
	got, ok := c.Prev(roomA, at, MeetingKinds)
	if !ok || !got.End.Equal(at) || !got.Start.Equal(es[2].Start) {
		t.Fatalf("Prev at the end of entry 2 = %v %v, want entry 2", got, ok)
	}
	if got, ok := c.Prev(roomA, at.Add(-time.Second), MeetingKinds); !ok || got.End.After(at.Add(-time.Second)) {
		t.Fatalf("Prev before an entry's end returned one still running: %v", got)
	}
	var none *Calendar
	if _, ok := none.Prev(roomA, at, MeetingKinds); ok {
		t.Fatal("a nil calendar has a previous entry")
	}
}
