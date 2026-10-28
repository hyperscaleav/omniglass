package systemplan

import (
	"strings"
	"testing"
)

func within(t, ancestor string) bool {
	return t == ancestor || (t == "display-flat" && ancestor == "display")
}

var catalog = []Product{
	{Name: "bar-1", ComponentType: "video-bar"},
	{Name: "codec-1", ComponentType: "codec"},
	{Name: "cam-1", ComponentType: "camera"},
	{Name: "screen-55", ComponentType: "display-flat"},
	{Name: "panel-a", ComponentType: "touch-panel"},
}

// fills is a template's candidate set: the one product it names per role.
func fills(m map[string]string) Catalog {
	return Catalog{
		Candidate: func(role, product string) bool { return m[role] == product },
		Within:    within,
		Noun:      "template",
	}
}

var huddle = []Role{
	{Name: "display", Quorum: 2, AcceptedTypes: []string{"display"}},
	{Name: "bar", AcceptedTypes: []string{"video-bar"}, Alternate: "conf/all-in-one"},
	{Name: "codec", AcceptedTypes: []string{"codec"}, Alternate: "conf/kit"},
	{Name: "camera", AcceptedTypes: []string{"camera"}, Alternate: "conf/kit"},
}

// Candidates per role: the planner takes exactly the product each role is
// filled with, to quorum, and the alternate the fills answer, even when it
// is not the first by name.
func TestPerRoleCandidatesStaffTheAlternateTheyFill(t *testing.T) {
	plan := PlanSystem(huddle, catalog, fills(map[string]string{
		"display": "screen-55", "codec": "codec-1", "camera": "cam-1",
	}))
	if plan.Alternates["conf"] != "kit" {
		t.Fatalf("alternate = %q, want kit (the one the fills answer)", plan.Alternates["conf"])
	}
	got := map[string]string{}
	for _, f := range plan.Roles {
		got[f.Role] = f.Product
		if f.Role == "display" && f.Count != 2 {
			t.Errorf("display count = %d, want its quorum 2", f.Count)
		}
	}
	if len(got) != 3 || got["display"] != "screen-55" || got["codec"] != "codec-1" || got["camera"] != "cam-1" {
		t.Fatalf("roles = %v, want display, codec and camera filled", got)
	}
	if plan.Components() != 4 {
		t.Fatalf("components = %d, want 4", plan.Components())
	}
}

// A fill whose product is not a type its role accepts leaves the role short,
// with the reason in the candidates' own words.
func TestAFillOfTheWrongTypeIsShort(t *testing.T) {
	plan := PlanSystem(huddle[:2], catalog, fills(map[string]string{"display": "panel-a", "bar": "bar-1"}))
	for _, f := range plan.Roles {
		if f.Role == "display" {
			if f.Count != 0 || f.Missing != 2 || !strings.Contains(f.Reason, "no template product is a display") {
				t.Fatalf("display = %+v, want short 2 with the template's reason", f)
			}
			return
		}
	}
	t.Fatal("display not planned")
}

// Capacity caps the count below a quorum.
func TestCapacityCapsTheCount(t *testing.T) {
	two := 1
	plan := PlanSystem([]Role{{Name: "display", Quorum: 3, Capacity: &two}}, catalog, fills(map[string]string{"display": "screen-55"}))
	if plan.Roles[0].Count != 1 {
		t.Fatalf("count = %d, want the capacity 1", plan.Roles[0].Count)
	}
}
