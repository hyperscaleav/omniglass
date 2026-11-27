package scenario

import (
	"reflect"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// A room a cloud service covers from the headquarters' world reads the same
// calendar the room's own site's world gives it (#2757, central by
// derivation): the same meetings and the same phases, for every room of every
// site, over a working week.
func TestCoveredRoomsReadTheSameCalendarInEveryWorld(t *testing.T) {
	m, cat := corporate(t)
	plan, err := Resolve(m, cat, 20)
	if err != nil {
		t.Fatal(err)
	}
	hq, err := SiteWorld(m, cat, plan, plan.Sites[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	hqCal := hq.Calendar()
	from := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	checked, meetings := 0, 0
	for _, sp := range plan.Sites[1:] {
		own, err := SiteWorld(m, cat, plan, sp.Name)
		if err != nil {
			t.Fatal(err)
		}
		ownCal := own.Calendar()
		for _, sys := range sp.Systems {
			room := calendar.Room{Site: sp.Name, Building: sys.Building, Room: sys.Room}
			want := ownCal.Entries(room, from, from.Add(5*24*time.Hour), calendar.MeetingKinds)
			got := hqCal.Entries(room, from, from.Add(5*24*time.Hour), calendar.MeetingKinds)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("room %s of %s: the headquarters' world holds %d meetings, the site's own %d", sys.Room, sp.Name, len(got), len(want))
			}
			for at := from; at.Before(from.Add(5 * 24 * time.Hour)); at = at.Add(7 * time.Minute) {
				if g, w := hqCal.StateAt(room, at), ownCal.StateAt(room, at); g != w {
					t.Fatalf("room %s of %s at %s: the headquarters' world reads %+v, the site's own %+v", sys.Room, sp.Name, at, g, w)
				}
			}
			meetings += len(want)
			checked++
		}
	}
	t.Logf("%d rooms, %d meetings", checked, meetings)
	if checked == 0 {
		t.Fatal("the plan has no branch room to compare")
	}
}
