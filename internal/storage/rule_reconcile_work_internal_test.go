package storage

import (
	"reflect"
	"testing"
)

// TestReconcileWorkOrdersAndCapsTheRejudges: a pass works the series that
// drifted first, then the ones held for a retry (in a fixed order), then the
// history readers' touched series, capped, so a fleet of windowed series
// cannot spend the pass's deadline before a drifted one is healed (#1985).
// A series in more than one list is worked once, as its drift.
func TestReconcileWorkOrdersAndCapsTheRejudges(t *testing.T) {
	t.Parallel()
	cand := func(rule, comp string) ruleCandidate {
		return ruleCandidate{rule: EventRule{ID: rule + "-id", Name: rule}, component: comp}
	}
	key := func(rule, comp string) ruleSeries { return ruleSeries{rule + "-id", rule, comp, ""} }
	drifted := []ruleCandidate{cand("hot", "a"), cand("hot", "b")}
	retry := map[ruleSeries]int{key("cold", "z"): 1, key("cold", "y"): 2, key("hot", "a"): 1}
	rejudge := []ruleCandidate{cand("trend", "a"), cand("trend", "b"), cand("trend", "c"), cand("hot", "b")}

	var got []ruleSeries
	for _, w := range reconcileWork(drifted, retry, rejudge, 2) {
		got = append(got, w.key)
	}
	want := []ruleSeries{key("hot", "a"), key("hot", "b"), key("cold", "y"), key("cold", "z"), key("trend", "a"), key("trend", "b")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("work %v, want %v", got, want)
	}
	if w := reconcileWork(drifted, nil, nil, 2); w[0].c.rule.Name != "hot" || w[0].c.component != "a" {
		t.Errorf("a drifted series is worked as its candidate, got %+v", w[0])
	}
}
