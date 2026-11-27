package calendar

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

func TestNewEntryRefusesAKindAndPayloadThatDisagree(t *testing.T) {
	cases := []struct {
		name    string
		kind    Kind
		payload Payload
		ok      bool
	}{
		{"meeting kind, meeting payload", KindMeeting, Meeting{Actual: 3}, true},
		{"problem kind, problem payload", KindProblem, Problem{Name: "p"}, true},
		{"meeting kind, problem payload", KindMeeting, Problem{Name: "p"}, false},
		{"problem kind, meeting payload", KindProblem, Meeting{Actual: 3}, false},
		{"meeting kind, no payload", KindMeeting, nil, false},
		{"unknown kind, meeting payload", Kind("scene"), Meeting{}, false},
	}
	for _, c := range cases {
		e, err := NewEntry(c.kind, t0, t0.Add(time.Hour), roomA, c.payload)
		if (err == nil) != c.ok {
			t.Fatalf("%s: err = %v, want ok = %v", c.name, err, c.ok)
		}
		if !c.ok && (e.Kind != "" || !e.Start.IsZero()) {
			t.Fatalf("%s: a refused entry is not the zero entry: %+v", c.name, e)
		}
	}
}

func TestEachAccessorReportsFalseForAnotherKind(t *testing.T) {
	m := MeetingEntry(t0, t0.Add(time.Hour), roomA, Meeting{Organizer: "Avery Chen", Actual: 4})
	p := ProblemEntry(t0, t0.Add(time.Hour), roomA, Problem{Name: "dead-mic", Role: "mic"})

	if got, ok := m.AsMeeting(); !ok || got.Organizer != "Avery Chen" || got.Actual != 4 {
		t.Fatalf("a meeting entry reads %+v, %v", got, ok)
	}
	if _, ok := m.AsProblem(); ok {
		t.Fatal("a meeting entry read as a problem")
	}
	if got, ok := p.AsProblem(); !ok || got.Name != "dead-mic" || got.Role != "mic" {
		t.Fatalf("a problem entry reads %+v, %v", got, ok)
	}
	if _, ok := p.AsMeeting(); ok {
		t.Fatal("a problem entry read as a meeting")
	}
	var zero Entry
	if _, ok := zero.AsMeeting(); ok {
		t.Fatal("the zero entry read as a meeting")
	}
	if _, ok := zero.AsProblem(); ok {
		t.Fatal("the zero entry read as a problem")
	}
	// A kind rewritten after construction does not let a payload read as the other kind.
	m.Kind = KindProblem
	if _, ok := m.AsMeeting(); ok {
		t.Fatal("an entry whose kind was changed still read as a meeting")
	}
}

func TestAnAccessorHandsBackACopy(t *testing.T) {
	e := MeetingEntry(t0, t0.Add(time.Hour), roomA, Meeting{Actual: 4})
	m, _ := e.AsMeeting()
	m.Actual = 99
	if again, _ := e.AsMeeting(); again.Actual != 4 {
		t.Fatal("writing the value an accessor returned changed the entry")
	}
}

func TestEntriesReadsOnlyTheKindsAskedFor(t *testing.T) {
	k := Knobs{BookingRate: 0.6, SlotMix: map[int]float64{30: 1}, Attendance: Range{Min: 2, Max: 4}, Hours: Hours{Start: 8, End: 18}}
	c := New(3, func(Room) (Knobs, bool) { return k, true }).WithProblems(func(Room) []ProblemSpec {
		return []ProblemSpec{spec(func(*ProblemSpec) {})}
	})
	day := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	end := day.AddDate(0, 0, 1)
	count := func(kinds Kinds) (meetings, problems int) {
		for _, e := range c.Entries(rm, day, end, kinds) {
			if _, ok := e.AsMeeting(); ok {
				meetings++
			}
			if _, ok := e.AsProblem(); ok {
				problems++
			}
		}
		return
	}
	if m, p := count(MeetingKinds); m == 0 || p != 0 {
		t.Fatalf("meeting kinds read %d meetings and %d problems", m, p)
	}
	if m, p := count(ProblemKinds); m != 0 || p != 1 {
		t.Fatalf("problem kinds read %d meetings and %d problems", m, p)
	}
	if m, p := count(AllKinds); m == 0 || p != 1 {
		t.Fatalf("all kinds read %d meetings and %d problems", m, p)
	}
	if got := c.Entries(rm, day, end, nil); len(got) != 0 {
		t.Fatalf("no kinds read %d entries, want none", len(got))
	}
	if _, ok := c.Now(rm, day.Add(10*time.Hour), ProblemKinds); !ok {
		if _, ok := c.Next(rm, day, ProblemKinds); !ok {
			t.Fatal("the problem kind reads neither now nor next on its day")
		}
	}
}

// Entry's payload is not an exported field, so a reader cannot dereference it
// and a new kind cannot nil-deref one that does not know it.
func TestEntryExportsNoPayloadField(t *testing.T) {
	rt := reflect.TypeOf(Entry{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		switch f.Type.Kind() {
		case reflect.Ptr, reflect.Interface:
			t.Errorf("Entry.%s is an exported %s: a payload reaches a reader through AsMeeting or AsProblem", f.Name, f.Type.Kind())
		}
	}
}
