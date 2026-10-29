package labfootprint

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// The harness reads the lab's ready file, which the lab writes as shell
// assignments; a value with a quote in it survives the round trip.
func TestParseReadyEnv(t *testing.T) {
	in := lab.FormatReadyEnv([][2]string{{"LAB_DB", "foot-1-db-1"}, {"LAB_LABELS", "a=b c=it's"}, {"LAB_TOKEN", ""}})
	got, err := ParseReadyEnv(in)
	if err != nil {
		t.Fatal(err)
	}
	if got["LAB_DB"] != "foot-1-db-1" || got["LAB_LABELS"] != "a=b c=it's" || got["LAB_TOKEN"] != "" {
		t.Fatalf("parsed %q", got)
	}
	if _, err := ParseReadyEnv("LAB_DB=unquoted\n"); err == nil {
		t.Fatal("an unquoted value parsed")
	}
}

// The series the harness reads are the sampled metrics of the first
// components that have any, a few per component, so a read covers more than
// one component's index range.
func TestPickSeries(t *testing.T) {
	comps := []ComponentMetrics{
		{Component: "a", Metrics: []string{"rtt", "cpu", "temp", "fan"}},
		{Component: "b"},
		{Component: "c", Metrics: []string{"rtt"}},
		{Component: "d", Metrics: []string{"rtt"}},
	}
	got := PickSeries(comps, 4, 2)
	want := []Series{{"a", "rtt"}, {"a", "cpu"}, {"c", "rtt"}, {"d", "rtt"}}
	if len(got) != len(want) {
		t.Fatalf("picked %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("picked %v, want %v", got, want)
		}
	}
}
