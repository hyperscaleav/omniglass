package labfootprint

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestADriverCostTableReadsBackAsWritten(t *testing.T) {
	in := DriverCostTable{Date: "2026-11-13", Build: "v1", StepTasks: 400,
		Rows:        []DriverCost{{Driver: "http-json", MemoryPerTaskBytes: 2048, MillicoresPerTask: 0.01}},
		NotMeasured: []DriverSkip{{Driver: "ucc-webhook", Reason: SkipNoPolls}}}
	b, err := in.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, []byte("}\n")) {
		t.Fatal("the committed table ends in a newline")
	}
	out, err := ReadDriverCostTable(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Build != "v1" || len(out.Rows) != 1 || out.Rows[0].MemoryPerTaskBytes != 2048 || out.NotMeasured[0].Driver != "ucc-webhook" {
		t.Fatalf("read back %+v", out)
	}
}

// The docs list every shipped driver: measured, not measured with the
// sweep's reason, or one the plan skips with the plan's. A driver the plan
// gained after the table was measured reads as not yet measured rather than
// going missing.
func TestTheDocsListEveryDriverThePlanKnows(t *testing.T) {
	targets := []DriverTarget{
		{Driver: "a", Product: "pa", Polls: []time.Duration{time.Minute}},
		{Driver: "b", Product: "pb", Polls: []time.Duration{time.Minute}},
		{Driver: "c", Product: "pc", Polls: []time.Duration{time.Minute}},
	}
	skips := []DriverSkip{{Driver: "s", Reason: SkipNoPolls}}
	table := DriverCostTable{Date: "d", Build: "v", StepTasks: 400,
		Rows:        []DriverCost{{Driver: "a", Product: "pa", Polls: []string{"1m0s"}, MemoryPerTaskBytes: 3072, MillicoresPerTask: 0.02, CPUPerRunMillis: 1.2, ThreadCPUPerRunMillis: 0.6, ThreadShare: 0.5}},
		NotMeasured: []DriverSkip{{Driver: "b", Reason: "b cpu: the sweep does not rise"}, {Driver: "s", Reason: SkipNoPolls}}}
	f := driverCostFacts(targets, skips, table)
	if f.Build != "v" || f.Date != "d" || f.StepTasks != 400 {
		t.Fatalf("facts header = %+v", f)
	}
	got := map[string]DriverCostFact{}
	for _, d := range f.Drivers {
		got[d.Driver] = d
	}
	if len(got) != 4 {
		t.Fatalf("drivers = %+v, want a, b, c and s once each", f.Drivers)
	}
	if a := got["a"]; !a.Measured || a.MemoryPerTaskKiB != 3 || a.CPUPerRunMillis != 1.2 || a.ThreadShare != 0.5 {
		t.Fatalf("a = %+v, want its measured row", a)
	}
	if b := got["b"]; b.Measured || !strings.Contains(b.Reason, "does not rise") || b.Product != "pb" {
		t.Fatalf("b = %+v, want the sweep's reason and its product", b)
	}
	if c := got["c"]; c.Measured || c.Reason != NotYetMeasured {
		t.Fatalf("c = %+v, want not yet measured", c)
	}
	if s := got["s"]; s.Measured || s.Reason != SkipNoPolls {
		t.Fatalf("s = %+v, want the plan's reason", s)
	}
	if f.Drivers[0].Driver != "a" || f.Drivers[3].Driver != "s" {
		t.Fatalf("drivers in order %+v", f.Drivers)
	}
}

// The docs render the shipped table from docs/src/generated/drivercost.json,
// joined with the plan, so a stale render fails here.
func TestDriverCostFactsJSONIsCommitted(t *testing.T) {
	committed, err := os.ReadFile("../../docs/src/generated/drivercost.json")
	if err != nil {
		t.Fatalf("read committed drivercost.json (run make gen and commit it): %v", err)
	}
	want, err := DriverCostFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, want) {
		t.Error("docs/src/generated/drivercost.json drifted from the shipped driver cost table and plan: run make gen and commit the result")
	}
}
