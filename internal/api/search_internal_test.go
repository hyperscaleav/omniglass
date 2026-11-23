package api

import (
	"reflect"
	"testing"
)

func TestRankCatalogOrdersExactThenPrefixThenRest(t *testing.T) {
	all := []searchCatalogBody{
		{Kind: "metric", Name: "ambient-panel-temp", Label: "Ambient Panel Temp"},
		{Kind: "product", Name: "panel-x", Label: "Panel X"},
		{Kind: "driver", Name: "panel", Label: "Panel"},
		{Kind: "metric", Name: "unrelated", Label: "Unrelated"},
	}
	var got []string
	for _, b := range rankCatalog(all, " PANEL ", 8) {
		got = append(got, b.Name)
	}
	want := []string{"panel", "panel-x", "ambient-panel-temp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
	if n := len(rankCatalog(all, "panel", 1)); n != 1 {
		t.Errorf("limit 1 kept %d", n)
	}
}

func TestLocationPathOfIsTheLocationChainAboveTheEntity(t *testing.T) {
	cases := []struct {
		segs []string
		loc  bool
		want string
	}{
		{[]string{"hq", "floor-2", "aud"}, true, "hq / floor-2"},
		{[]string{"hq"}, true, ""},
		{[]string{"hq", "aud", "$comp", "panel-1"}, false, "hq / aud"},
		{[]string{"$comp", "panel-1"}, false, ""},
	}
	for _, c := range cases {
		if got := locationPathOf(c.segs, c.loc); got != c.want {
			t.Errorf("locationPathOf(%v) = %q, want %q", c.segs, got, c.want)
		}
	}
}
