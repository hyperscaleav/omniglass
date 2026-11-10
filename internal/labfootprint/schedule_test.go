package labfootprint

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The schedule read (#1693) sums each node's runs due and runs made per kind
// over the window, from the schedule hop's own instances of the two count
// keys, one `instance|due|made` a line.
func TestScheduleQueryReadsTheScheduleHopInstances(t *testing.T) {
	q := ScheduleQuery(at(0), at(600))
	for _, want := range []string{"'" + writerclass.DeliveryInCount + "'", "'" + writerclass.DeliveryOutCount + "'",
		"like 'schedule/%'", "to_timestamp(" + epoch(at(0)) + ")", "to_timestamp(" + epoch(at(600)) + ")", "from node"} {
		if !strings.Contains(q, want) {
			t.Errorf("the schedule query has no %q:\n%s", want, q)
		}
	}
}

// The parse keeps the schedule hop's kinds, in kind order, and drops what is
// no schedule instance; a count that is not a number is an error.
func TestParseSchedule(t *testing.T) {
	got, err := ParseSchedule("schedule/property|3|2\nschedule/metric|4|3\nnode/metric|9|9\nschedule/bogus|1|1\n\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []ScheduleCount{{Kind: delivery.Metric, Due: 4, Made: 3}, {Kind: delivery.Property, Due: 3, Made: 2}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("parsed %+v, want %+v", got, want)
	}
	if _, err := ParseSchedule("schedule/metric|four|3\n"); err == nil {
		t.Error("a count that is no number parsed")
	}
}

// A kind's schedule row is its dues in and its runs made out, absent when
// nothing fell due; the rows lead each kind's other hops on the node lane,
// and the push lane's rows, which no schedule produced, follow the node lane's.
func TestScheduleRowsLeadTheirKind(t *testing.T) {
	sched := ScheduleRowsOf([]ScheduleCount{{Kind: delivery.Property, Due: 5, Made: 5}, {Kind: delivery.Log}})
	if len(sched) != 1 || sched[0].Loss == nil || *sched[0].Loss != 0 || sched[0].Cause() != "" {
		t.Fatalf("rows = %+v, want one property row reading 0, no cause", sched)
	}
	ingest := []DeliveryRow{{Lane: LaneNode, Hop: delivery.Ingest, Kind: delivery.Metric}, {Lane: LaneNode, Hop: delivery.Ingest, Kind: delivery.Property},
		{Lane: LanePush, Hop: delivery.Ingest, Kind: delivery.Metric}}
	rows := WithSchedule(sched, ingest)
	var order []string
	for _, r := range rows {
		order = append(order, string(r.Lane)+"/"+string(r.Kind)+"/"+string(r.Hop))
	}
	if got := strings.Join(order, " "); got != "node/metric/ingest node/property/schedule node/property/ingest push/metric/ingest" {
		t.Errorf("rows in order %s", got)
	}
}
