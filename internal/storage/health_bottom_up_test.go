package storage

import (
	"slices"
	"testing"
)

// TestBottomUpRecordsChildrenBeforeParents pins the order the location rollup
// depends on since #1562: a location folds its children's RECORDED verdicts,
// so every child in the chain has to be recorded before its parent reads it.
// The input arrives in id order (the lock order), which is unrelated to depth,
// and the ids here are chosen so that id order puts every parent FIRST: the
// one order that would roll each location up over its children's stale rows.
func TestBottomUpRecordsChildrenBeforeParents(t *testing.T) {
	t.Parallel()
	// campus > building > floor > two rooms, and a second building with no
	// floor, so two depths hold more than one location.
	locations := []ownerRef{
		{ID: "a-campus"}, {ID: "b-building"}, {ID: "c-floor"}, {ID: "d-room"}, {ID: "e-room"}, {ID: "f-building"},
	}
	parents := map[string]string{
		"b-building": "a-campus",
		"c-floor":    "b-building",
		"d-room":     "c-floor",
		"e-room":     "c-floor",
		"f-building": "a-campus",
	}
	got := refIDs(bottomUp(locations, parents))
	want := []string{"d-room", "e-room", "c-floor", "b-building", "f-building", "a-campus"}
	if !slices.Equal(got, want) {
		t.Errorf("bottomUp = %v, want %v (deepest first, by id within a depth)", got, want)
	}
	// The lock order is the caller's and must survive: bottomUp sorts a copy.
	if refIDs(locations)[0] != "a-campus" {
		t.Errorf("bottomUp reordered its input: %v", refIDs(locations))
	}
}

// TestBottomUpOfAChainAloneAndOfNothing covers the shapes a single write
// produces: one location with no parent (a root, or a chain the caller named
// alone) and an empty chain (a component placed nowhere).
func TestBottomUpOfAChainAloneAndOfNothing(t *testing.T) {
	t.Parallel()
	if got := bottomUp(nil, nil); len(got) != 0 {
		t.Errorf("bottomUp of nothing = %v, want nothing", got)
	}
	got := refIDs(bottomUp([]ownerRef{{ID: "root"}}, map[string]string{}))
	if !slices.Equal(got, []string{"root"}) {
		t.Errorf("bottomUp of a root = %v, want [root]", got)
	}
}
