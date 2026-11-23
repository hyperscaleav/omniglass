package storage

import "testing"

// A component's selection by a rule is the rule's selector read against the
// component's id and its type's lineage: an empty selector takes every
// component, a listed id takes that one, and a named type takes every
// component whose type is that type or falls beneath it.
func TestComponentSelectionSelectedBy(t *testing.T) {
	t.Parallel()
	bar := componentSelection{componentID: "c1", lineage: []string{"collaboration-device", "video-bar"}}
	untyped := componentSelection{componentID: "c1"}
	for name, tc := range map[string]struct {
		sel  componentSelection
		rule EventRuleSelector
		want bool
	}{
		"an empty selector takes every component": {bar, EventRuleSelector{}, true},
		"an empty selector takes an untyped one":  {untyped, EventRuleSelector{}, true},
		"its own type":                            {bar, EventRuleSelector{ComponentTypes: []string{"video-bar"}}, true},
		"an ancestor of its type":                 {bar, EventRuleSelector{ComponentTypes: []string{"collaboration-device"}}, true},
		"a type off its lineage":                  {bar, EventRuleSelector{ComponentTypes: []string{"display"}}, false},
		"one of several types":                    {bar, EventRuleSelector{ComponentTypes: []string{"display", "video-bar"}}, true},
		"listed by id":                            {bar, EventRuleSelector{Components: []string{"c1"}}, true},
		"another component listed":                {bar, EventRuleSelector{Components: []string{"c2"}}, false},
		"untyped against a type":                  {untyped, EventRuleSelector{ComponentTypes: []string{"video-bar"}}, false},
		"untyped but listed":                      {untyped, EventRuleSelector{ComponentTypes: []string{"video-bar"}, Components: []string{"c1"}}, true},
	} {
		if got := tc.sel.selectedBy(tc.rule); got != tc.want {
			t.Errorf("%s: selectedBy = %v, want %v", name, got, tc.want)
		}
	}
}

// The names a reselect bounds its rules by are the union of both lineages,
// each name once: a component moved between two types considers the rules
// naming either, and one whose type did not change considers its own.
func TestReselectNames(t *testing.T) {
	t.Parallel()
	before := componentSelection{lineage: []string{"collaboration-device", "video-bar"}}
	after := componentSelection{lineage: []string{"display", "display-device", "interactive-display"}}
	got := reselectNames(before, after)
	want := []string{"collaboration-device", "display", "display-device", "interactive-display", "video-bar"}
	if len(got) != len(want) {
		t.Fatalf("reselectNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reselectNames = %v, want %v", got, want)
		}
	}
	if got := reselectNames(before, before); len(got) != 2 {
		t.Errorf("an unchanged lineage: reselectNames = %v, want its two names once each", got)
	}
	if got := reselectNames(componentSelection{}, componentSelection{}); len(got) != 0 {
		t.Errorf("untyped before and after: reselectNames = %v, want none", got)
	}
}
