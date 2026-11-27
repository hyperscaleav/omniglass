package scenario

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/world"
)

// #2773: the plan resolves to one world per site, deterministically, and the
// worlds hold every planned room, device and service coverage, at the
// corporate scenario's smallest size and at the scale target.
func TestSiteWorldsCoverTheWholePlan(t *testing.T) {
	m, cat := corporate(t)
	for _, size := range []int{MinSystems(m), 20, 3000, MaxSystems} {
		plan, err := Resolve(m, cat, size)
		if err != nil {
			t.Fatal(err)
		}
		worlds, err := SiteWorlds(m, cat, plan)
		if err != nil {
			t.Fatal(err)
		}
		again, err := SiteWorlds(m, cat, plan)
		if err != nil {
			t.Fatal(err)
		}
		if len(worlds) != len(plan.Sites) {
			t.Fatalf("size %d: %d worlds for %d sites", size, len(worlds), len(plan.Sites))
		}
		for _, sp := range plan.Sites {
			w := worlds[SiteOutpost(sp.Name)]
			if w == nil {
				t.Fatalf("size %d: no world for site %s", size, sp.Name)
			}
			if !sameWorld(t, w, again[SiteOutpost(sp.Name)]) {
				t.Fatalf("size %d: site %s resolves to a different world each time", size, sp.Name)
			}
			if err := w.Validate(); err != nil {
				t.Fatalf("size %d: %v", size, err)
			}
			// The site that runs the fleet's polled service also holds every other
			// site's rooms, with no devices, which the service covers (#2765).
			wantSites := 1
			for _, p := range sp.Pushers {
				if p.Service {
					wantSites = len(plan.Sites)
				}
			}
			if len(w.Sites) != wantSites || w.Sites[0].Name != sp.Name || len(w.Sites[0].Rooms) != len(sp.Systems) {
				t.Fatalf("size %d: site %s world holds %d rooms, plan has %d systems", size, sp.Name, len(w.Sites[0].Rooms), len(sp.Systems))
			}
			devices := 0
			for i, sys := range sp.Systems {
				r := w.Sites[0].Rooms[i]
				if r.Name != sys.Room || r.Building != sys.Building || r.Standard != sys.Standard || r.System != sys.Name {
					t.Fatalf("size %d: room %d = %+v, plan system %+v", size, i, r, sys)
				}
				want := 0
				for _, f := range sys.Slots {
					want += f.Count
				}
				if len(r.Devices) != want {
					t.Fatalf("size %d: %s holds %d devices, plan has %d", size, r.Name, len(r.Devices), want)
				}
				devices += want
				if k, ok := m.MeetingKnobs(sys.Standard); ok && r.Capacity != k.Attendance.Max {
					t.Fatalf("size %d: %s capacity %d, its standard's attendance max is %d", size, r.Name, r.Capacity, k.Attendance.Max)
				}
			}
			if devices != sp.Totals.Components {
				t.Fatalf("size %d: site %s world holds %d devices, plan has %d components", size, sp.Name, devices, sp.Totals.Components)
			}
			names := map[string]bool{}
			for _, r := range w.Sites[0].Rooms {
				for _, d := range r.Devices {
					if names[d.Name] {
						t.Fatalf("size %d: the world names %s twice", size, d.Name)
					}
					names[d.Name] = true
				}
			}
			if len(w.Services) != len(sp.Pushers) {
				t.Fatalf("size %d: site %s world holds %d services, plan has %d pushers", size, sp.Name, len(w.Services), len(sp.Pushers))
			}
			for i, p := range sp.Pushers {
				if len(w.Services[i].Covers) != len(p.Systems) {
					t.Fatalf("size %d: %s covers %d rooms, plan scopes it to %d systems", size, p.Name, len(w.Services[i].Covers), len(p.Systems))
				}
				for j, sys := range p.Systems {
					got, ok := w.PlaceSystem(sys)
					c := w.Services[i].Covers[j]
					if !ok || c.System != sys || c.Room != got.Room || c.Site != got.Site || c.Building != got.Building {
						t.Fatalf("size %d: %s's cover %d = %+v, system %s sits at %+v", size, p.Name, j, c, sys, got)
					}
				}
			}
		}
	}
}

// Every standard with meeting knobs reaches the world, and the calendar the
// world builds equals the one built straight from the manifest.
func TestSiteWorldCarriesTheMeetingKnobsAndASeedOfTheScenario(t *testing.T) {
	m, cat := corporate(t)
	plan, err := Resolve(m, cat, 20)
	if err != nil {
		t.Fatal(err)
	}
	w, err := SiteWorld(m, cat, plan, plan.Sites[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, sm := range m.Calendar.Meetings {
		if got, ok := w.Meetings[sm.Standard.Name]; !ok || !reflect.DeepEqual(got, sm.Knobs) {
			t.Errorf("standard %s: world knobs %+v, manifest %+v", sm.Standard.Name, got, sm.Knobs)
		}
	}
	other, err := Resolve(m, cat, 40)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := SiteWorld(m, cat, other, other.Sites[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if w.CalendarSeed != w2.CalendarSeed || w.CalendarSeed == 0 {
		t.Errorf("the seed moved with the lab's size: %d then %d", w.CalendarSeed, w2.CalendarSeed)
	}
	if _, err := SiteWorld(m, cat, plan, "nowhere"); err == nil {
		t.Error("a site the plan lacks resolved to a world")
	}
}

// sameWorld compares two worlds by their file bytes, the form that is
// delivered.
func sameWorld(t *testing.T, a, b *world.World) bool {
	t.Helper()
	x, err := a.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	y, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}

// A name the platform would refuse, or one the world gave twice, is refused
// when the world is planned, not when the seed meets it.
func TestSiteWorldRefusesANameThePlatformWouldRefuse(t *testing.T) {
	m, cat := corporate(t)
	plan, err := Resolve(m, cat, 20)
	if err != nil {
		t.Fatal(err)
	}
	site := plan.Sites[0].Name
	long := *plan
	long.Sites = append([]SitePlan(nil), plan.Sites...)
	long.Sites[0].Systems = append([]SystemPlan(nil), plan.Sites[0].Systems...)
	long.Sites[0].Systems[0].Name = strings.Repeat("x", 95)
	if _, err := SiteWorld(m, cat, &long, site); err == nil || !strings.Contains(err.Error(), "over 100") {
		t.Fatalf("a 95-character system gave %v, want a refusal for the name's length", err)
	}
	dup := *plan
	dup.Sites = append([]SitePlan(nil), plan.Sites...)
	dup.Sites[0].Systems = append([]SystemPlan(nil), plan.Sites[0].Systems...)
	dup.Sites[0].Systems[1].Name = dup.Sites[0].Systems[0].Name
	if _, err := SiteWorld(m, cat, &dup, site); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("two systems of one name gave %v, want a refusal", err)
	}
}
