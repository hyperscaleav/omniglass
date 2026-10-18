package simfleet

import (
	"strings"
	"testing"
)

// within is a toy type tree: display-flat sits under display.
func within(t, ancestor string) bool {
	return t == ancestor || (t == "display-flat" && ancestor == "display")
}

var products = []Product{
	{Name: "bar-1", ComponentType: "video-bar"},
	{Name: "panel-a", ComponentType: "touch-panel"},
	{Name: "panel-b", ComponentType: "touch-panel"},
	{Name: "screen-55", ComponentType: "display-flat"},
	{Name: "screen-86", ComponentType: "display-flat"},
	{Name: "codec-1", ComponentType: "codec"},
	{Name: "mic-1", ComponentType: "ceiling-mic"}, // a product nothing simulates
}

func simulated(p string) bool { return p != "mic-1" }

func planOf(t *testing.T, roles []Role) SystemPlan {
	t.Helper()
	return PlanSystem(roles, products, SystemCatalog{Simulated: simulated, Within: within})
}

func fillOf(p SystemPlan, role string) (RoleFill, bool) {
	for _, f := range p.Roles {
		if f.Role == role {
			return f, true
		}
	}
	return RoleFill{}, false
}

// Each role takes a simulated product whose type fits, as many as its quorum
// (at least one), in role order; a role nothing simulated fits is listed
// unstaffed with why.
func TestPlanSystemStaffsEachRoleFromWhatFits(t *testing.T) {
	p := planOf(t, []Role{
		{Name: "video-bar", Quorum: 1, AcceptedTypes: []string{"video-bar"}},
		{Name: "main-display", Quorum: 2, AcceptedTypes: []string{"display"}},
		{Name: "touch-control", Quorum: 0, AcceptedTypes: []string{"touch-panel"}},
		{Name: "room-mic", Quorum: 2, AcceptedTypes: []string{"ceiling-mic"}},
	})
	if f, _ := fillOf(p, "video-bar"); f.Product != "bar-1" || f.Count != 1 {
		t.Errorf("video-bar = %+v", f)
	}
	if f, _ := fillOf(p, "main-display"); f.Product != "screen-55" || f.Count != 2 {
		t.Errorf("main-display = %+v, want two of the first fitting display (a subtype counts)", f)
	}
	if f, _ := fillOf(p, "touch-control"); f.Product != "panel-a" || f.Count != 1 {
		t.Errorf("touch-control = %+v, want one even at quorum 0", f)
	}
	f, _ := fillOf(p, "room-mic")
	if f.Product != "" || f.Count != 0 || f.Missing != 2 || !strings.Contains(f.Reason, "ceiling-mic") {
		t.Errorf("room-mic = %+v, want unstaffed, short two, saying which type has no simulated product", f)
	}
	if p.Components() != 4 {
		t.Errorf("components = %d, want 4", p.Components())
	}
}

// A pinned role takes a pinned product, in pin order, and only one that is
// simulated and fits.
func TestPlanSystemHonorsPins(t *testing.T) {
	p := planOf(t, []Role{{Name: "main-display", Quorum: 1, AcceptedTypes: []string{"display"}, PinnedProducts: []string{"screen-86", "screen-55"}}})
	if f, _ := fillOf(p, "main-display"); f.Product != "screen-86" {
		t.Errorf("pinned = %+v, want the first pin", f)
	}
	p = planOf(t, []Role{{Name: "main-display", Quorum: 1, AcceptedTypes: []string{"display"}, PinnedProducts: []string{"screen-99"}}})
	if f, _ := fillOf(p, "main-display"); f.Product != "" || !strings.Contains(f.Reason, "screen-99") {
		t.Errorf("unsimulated pin = %+v, want unstaffed naming the pin", f)
	}
}

// A capacity below quorum caps the count.
func TestPlanSystemCapsAtCapacity(t *testing.T) {
	one := 1
	p := planOf(t, []Role{{Name: "panel", Quorum: 3, Capacity: &one, AcceptedTypes: []string{"display"}}})
	if f, _ := fillOf(p, "panel"); f.Count != 1 {
		t.Errorf("capped = %+v, want 1", f)
	}
}

// Of a choice's alternates, the plan takes one: the first (by name) whose
// roles can all be staffed, else the one that staffs the most; the other
// alternates' roles are left out, not reported short.
func TestPlanSystemTakesOneAlternatePerChoice(t *testing.T) {
	p := planOf(t, []Role{
		{Name: "main-display", Quorum: 1, AcceptedTypes: []string{"display"}},
		{Name: "conf-bar", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Alternate: "conferencing/all-in-one"},
		{Name: "conf-codec", Quorum: 1, AcceptedTypes: []string{"codec"}, Alternate: "conferencing/component-system"},
		{Name: "conf-mic", Quorum: 1, AcceptedTypes: []string{"ceiling-mic"}, Alternate: "conferencing/component-system"},
	})
	if _, ok := fillOf(p, "conf-bar"); !ok {
		t.Fatalf("plan %+v, want the fully staffable all-in-one alternate", p.Roles)
	}
	for _, r := range []string{"conf-codec", "conf-mic"} {
		if _, ok := fillOf(p, r); ok {
			t.Errorf("%s from the alternate not taken is in the plan", r)
		}
	}
	if p.Alternates["conferencing"] != "all-in-one" {
		t.Errorf("alternates = %v", p.Alternates)
	}
}

// An empty accepted-type list takes any simulated product.
func TestPlanSystemUntypedRoleTakesAnything(t *testing.T) {
	p := planOf(t, []Role{{Name: "anything", Quorum: 1}})
	if f, _ := fillOf(p, "anything"); f.Product == "" {
		t.Errorf("untyped = %+v, want some simulated product", f)
	}
}
