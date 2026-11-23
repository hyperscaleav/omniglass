package api

import (
	"fmt"
	"testing"
)

func TestPlanRowsCountsEveryPiece(t *testing.T) {
	t.Parallel()
	in := &applyPlanInput{}
	in.Body.Standards = []string{"meeting-room"}
	in.Body.Locations = []planLocationBody{{Key: "b"}, {Key: "f"}}
	in.Body.Systems = []planSystemBody{{
		Properties: []planValueBody{{Name: "a"}},
		Components: []planSlotComponentBody{{Slot: "s", Properties: []planValueBody{{Name: "b"}, {Name: "c"}}}},
	}}
	in.Body.Components = []planComponentBody{{Properties: []planValueBody{{Name: "d"}}}}
	// 1 standard + 2 locations + 1 system + 1 value + 1 component + 2 values + 1 component + 1 value.
	if n := planRows(in); n != 10 {
		t.Fatalf("planRows = %d, want 10", n)
	}
}

func TestPlanNeedsWhatEachPieceNeeds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   func(*applyPlanInput)
		want string
	}{
		{"empty", func(*applyPlanInput) {}, "[]"},
		{"locations", func(in *applyPlanInput) { in.Body.Locations = []planLocationBody{{Key: "b"}} }, "[[location create]]"},
		{"bare system", func(in *applyPlanInput) { in.Body.Systems = []planSystemBody{{}} }, "[[system create]]"},
		{"system value", func(in *applyPlanInput) {
			in.Body.Systems = []planSystemBody{{Properties: []planValueBody{{Name: "a"}}}}
		}, "[[system create] [system update]]"},
		{"staffed system", func(in *applyPlanInput) {
			in.Body.Systems = []planSystemBody{{Components: []planSlotComponentBody{{Slot: "s"}, {Slot: "t"}}}}
		}, "[[system create] [component create] [system read] [system update] [component read] [component update]]"},
		{"standalone", func(in *applyPlanInput) { in.Body.Components = []planComponentBody{{}} }, "[[component create]]"},
		{"standalone value", func(in *applyPlanInput) {
			in.Body.Components = []planComponentBody{{Properties: []planValueBody{{Name: "a"}}}}
		}, "[[component create] [component update]]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := &applyPlanInput{}
			c.in(in)
			if got := fmt.Sprint(planNeeds(in)); got != c.want {
				t.Fatalf("planNeeds = %s, want %s", got, c.want)
			}
		})
	}
}
