package scope_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// AdmitsChain judges one row against a resolved set from the row's ancestry
// alone (#1341): the queue-time recipient filter runs it once per candidate
// against one ancestor walk, so it must answer exactly what the gateway's
// inScopeTree answers. Chain is the row first, then its ancestors.
func TestAdmitsChainMatchesTheTreeWalk(t *testing.T) {
	chain := []string{"leaf", "mid", "root"}
	cases := []struct {
		name string
		set  scope.Set
		want bool
	}{
		{"all", scope.Set{All: true}, true},
		{"empty", scope.Set{}, false},
		{"the row is a subtree root", scope.Set{IDs: []string{"leaf"}}, true},
		{"an ancestor is a subtree root", scope.Set{IDs: []string{"root"}}, true},
		{"an unrelated root", scope.Set{IDs: []string{"other"}}, false},
		{"a self root on the row", scope.Set{SelfIDs: []string{"leaf"}}, true},
		{"a self root on an ancestor admits only that ancestor", scope.Set{SelfIDs: []string{"mid"}}, false},
		{"an excluded root strictly above", scope.Set{IDs: []string{"mid"}, ExcludeRootIDs: []string{"mid"}}, true},
		{"an excluded root that is the row", scope.Set{IDs: []string{"leaf"}, ExcludeRootIDs: []string{"leaf"}}, false},
		{"an excluded row a self root re-adds", scope.Set{IDs: []string{"leaf"}, ExcludeRootIDs: []string{"leaf"}, SelfIDs: []string{"leaf"}}, true},
	}
	for _, c := range cases {
		if got := c.set.AdmitsChain(chain); got != c.want {
			t.Errorf("%s: AdmitsChain = %v, want %v", c.name, got, c.want)
		}
	}
	if (scope.Set{IDs: []string{"leaf"}}).AdmitsChain(nil) {
		t.Error("an empty chain (no such row) was admitted")
	}
	if !(scope.Set{All: true}).AdmitsChain(nil) {
		t.Error("an all scope refused a row")
	}
}
