package devseed_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// A component of the example fleet is numbered within its slot, as the scale
// lab's are (#2757): a slot of several reads "<Type> <n>", a lone one reads the
// type alone, and a component in no slot is left to the shipped rule. The
// platform's counter spans a whole stem in a room, so the room's only wireless
// microphone read "Wireless Microphone 3" after two ceiling ones.
func TestSlotLabelsNumberWithinTheSlotAndLeaveALoneOneBare(t *testing.T) {
	t.Parallel()
	doc := devseed.Doc{
		Components: []devseed.Component{
			{Key: "m1", Product: "ceil"}, {Key: "m2", Product: "ceil"}, {Key: "w", Product: "wire"},
			{Key: "pinned", Product: "ceil", Label: "Stage Mic"}, {Key: "loose", Product: "ceil"}, {Key: "unknown", Product: "mystery"},
			{Key: "shared", Product: "dsp"},
		},
		SlotAssignments: []devseed.SlotAssignment{
			{System: "a", Slot: "room-mic", Component: "m1"}, {System: "a", Slot: "room-mic", Component: "m2"},
			{System: "a", Slot: "wireless-mic", Component: "w"},
			{System: "a", Slot: "stage", Component: "pinned"}, {System: "a", Slot: "stage", Component: "unknown"},
			{System: "a", Slot: "dsp", Component: "shared"}, {System: "b", Slot: "dsp", Component: "shared"},
		},
	}
	got := devseed.SlotLabels(doc, map[string]string{"ceil": "Ceiling Microphone", "wire": "Wireless Microphone", "dsp": "DSP"})
	want := map[string]string{"m1": "Ceiling Microphone 1", "m2": "Ceiling Microphone 2", "w": "Wireless Microphone", "shared": "DSP"}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	for _, k := range []string{"pinned", "loose", "unknown"} {
		if l, ok := got[k]; ok {
			t.Errorf("%s = %q, want the fixture's own label, the shipped rule or nothing", k, l)
		}
	}
}

func labelOf(t *testing.T, ctx context.Context, conn *pgx.Conn, room, name string) string {
	t.Helper()
	var label string
	if err := conn.QueryRow(ctx, `select c.label from component c join location l on l.id = c.location_id where l.name = $1 and c.name = $2`, room, name).Scan(&label); err != nil {
		t.Fatalf("no component %q in %q: %v", name, room, err)
	}
	return label
}

// The seeded boardroom's only wireless microphone reads bare, its two ceiling
// microphones 1 and 2, its two displays 1 and 2, the huddle's lone display bare.
func TestSeededComponentsNumberWithinTheirSlot(t *testing.T) {
	t.Parallel()
	ctx, conn, _ := seededFleet(t)
	for _, tc := range []struct{ room, name, want string }{
		{"boardroom-a", "mic-3", "Wireless Microphone"},
		{"boardroom-a", "mic-1", "Ceiling Microphone 1"},
		{"boardroom-a", "mic-2", "Ceiling Microphone 2"},
		{"boardroom-a", "display-1", "Display 1"},
		{"boardroom-a", "display-2", "Display 2"},
		{"huddle", "display-1", "Display"},
	} {
		if got := labelOf(t, ctx, conn, tc.room, tc.name); got != tc.want {
			t.Errorf("%s/%s reads %q, want %q", tc.room, tc.name, got, tc.want)
		}
	}
}
