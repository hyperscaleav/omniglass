package sim

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestCensusOccupancyBehaviorRenders exercises the tessellate-census-c1
// occupancy_drift behavior exactly as authored in the profile, over many
// ticks and several device seeds. It guards two things that a template
// author can easily get wrong:
//
//   - render failures: tickBehavior only logs render errors (slog.Warn)
//     and leaves state untouched, so a broken template would silently
//     freeze the field. We assert last_event is always a valid enum
//     ("increased"/"decreased"/"steady"), an empty/garbage value means
//     the value template failed to render (e.g. a gt/lt int64-vs-float64
//     kind mismatch).
//   - invariant drift: people_count must stay whole-numbered within
//     [0, 14], and vacant must agree with count==0.
func TestCensusOccupancyBehaviorRenders(t *testing.T) {
	muts := []profile.Mutation{
		{Path: "status.occupancy.people_count_prev", Value: "{{ .State.status.occupancy.people_count }}"},
		{Path: "status.occupancy.people_count", Value: "{{ round 0 (clamp 0.0 14.0 (randStep 2.0 .State.status.occupancy.people_count)) }}"},
		{Path: "status.occupancy.last_event", Value: "{{ $delta := sub .State.status.occupancy.people_count .State.status.occupancy.people_count_prev }}{{ if gt $delta 0 }}increased{{ else if lt $delta 0 }}decreased{{ else }}steady{{ end }}"},
		{Path: "status.occupancy.vacant", Value: "{{ if eq .State.status.occupancy.people_count 0 }}true{{ else }}false{{ end }}"},
	}

	validEvent := map[string]bool{"increased": true, "decreased": true, "steady": true}

	for _, id := range []string{"census-001", "census-002", "census-003"} {
		state := map[string]any{
			"status": map[string]any{
				"occupancy": map[string]any{
					"people_count":      0,
					"people_count_prev": 0,
					"last_event":        "steady",
					"vacant":            true,
				},
			},
		}
		d := newTestDevice(t, id, state)
		if err := d.AddBehaviorFromProfile(&profile.Behavior{
			Name:      "occupancy_drift",
			Schedule:  profile.Schedule{Interval: "20s"},
			Mutations: muts,
		}); err != nil {
			t.Fatalf("AddBehaviorFromProfile: %v", err)
		}

		for tick := 1; tick <= 50; tick++ {
			d.tickBehavior(&d.behaviors[0])
			occ := d.State()["status"].(map[string]any)["occupancy"].(map[string]any)

			ev, _ := occ["last_event"].(string)
			if !validEvent[ev] {
				t.Fatalf("%s tick %d: last_event = %q (%T), want a valid enum, likely a render failure",
					id, tick, occ["last_event"], occ["last_event"])
			}

			cnt, _, err := toFloat(occ["people_count"])
			if err != nil {
				t.Fatalf("%s tick %d: people_count = %v (%T) not numeric", id, tick, occ["people_count"], occ["people_count"])
			}
			if cnt < 0 || cnt > 14 {
				t.Fatalf("%s tick %d: people_count = %v out of [0,14]", id, tick, cnt)
			}
			if cnt != float64(int64(cnt)) {
				t.Fatalf("%s tick %d: people_count = %v not whole-numbered", id, tick, cnt)
			}

			vac, ok := occ["vacant"].(bool)
			if !ok {
				t.Fatalf("%s tick %d: vacant = %v (%T), want bool, likely a render failure", id, tick, occ["vacant"], occ["vacant"])
			}
			if want := cnt < 0.5; vac != want {
				t.Fatalf("%s tick %d: vacant = %v but people_count = %v", id, tick, vac, cnt)
			}
		}
	}
}
