package labfootprint

import (
	"bytes"
	"encoding/json"
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
		{Driver: "e", Product: "pe", Polls: []time.Duration{time.Minute}},
	}
	skips := []DriverSkip{{Driver: "s", Reason: SkipNoPolls}}
	table := DriverCostTable{Date: "d", Build: "v", StepTasks: 400,
		Rows: []DriverCost{{Driver: "a", Product: "pa", Polls: []string{"1m0s"}, MemoryPerTaskBytes: 3072, MillicoresPerTask: 0.02, CPUPerRunMillis: 1.2, ThreadCPUPerRunMillis: 0.6, ThreadShare: 0.5, MemoryWarmBytes: 2 << 20},
			{Driver: "e", Product: "pe", Polls: []string{"1m0s"}, MillicoresPerTask: 0.03, CPUPerRunMillis: 1.8, ThreadCPUPerRunMillis: 0.9, ThreadShare: 0.5, MemoryNotMeasured: "e memory: the sweep is not a line"}},
		NotMeasured: []DriverSkip{{Driver: "b", Reason: "b cpu: the sweep does not rise"}, {Driver: "s", Reason: SkipNoPolls}}}
	f := driverCostFacts(targets, skips, table)
	if f.Build != "v" || f.Date != "d" || f.StepTasks != 400 {
		t.Fatalf("facts header = %+v", f)
	}
	got := map[string]DriverCostFact{}
	for _, d := range f.Drivers {
		got[d.Driver] = d
	}
	if len(got) != 5 {
		t.Fatalf("drivers = %+v, want a, b, c, e and s once each", f.Drivers)
	}
	if a := got["a"]; !a.Measured || *a.MemoryPerTaskKiB != 3 || *a.MemoryWarmMiB != 2 || *a.CPUPerRunMillis != 1.2 || *a.ThreadShare != 0.5 || a.Reason != "" {
		t.Fatalf("a = %+v, want its measured row", a)
	}
	// A driver measured in CPU but not memory lists its CPU, no memory
	// figure, and why.
	if e := got["e"]; !e.Measured || e.MemoryPerTaskKiB != nil || e.MemoryWarmMiB != nil || *e.CPUPerRunMillis != 1.8 || !strings.Contains(e.Reason, "memory") {
		t.Fatalf("e = %+v, want its cpu, no memory, and the memory's reason", e)
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
	if f.Drivers[0].Driver != "a" || f.Drivers[4].Driver != "s" {
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

// A measured figure of zero is a measurement: the docs read it as 0, not as
// a blank the unmeasured rows leave.
func TestAMeasuredZeroReadsAsZero(t *testing.T) {
	targets := []DriverTarget{{Driver: "a", Product: "pa", Polls: []time.Duration{time.Minute}}}
	f := driverCostFacts(targets, nil, DriverCostTable{Rows: []DriverCost{{Driver: "a", MemoryPerTaskBytes: 1024}}})
	b, err := json.Marshal(f.Drivers[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"thread_cpu_per_run_ms":0`, `"thread_share":0`, `"millicores_per_task":0`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s lacks %s", b, want)
		}
	}
	f = driverCostFacts(targets[:0], []DriverSkip{{Driver: "s", Reason: SkipNoPolls}}, DriverCostTable{})
	if b, _ = json.Marshal(f.Drivers[0]); strings.Contains(string(b), "thread_share") {
		t.Fatalf("an unmeasured driver has no figures: %s", b)
	}
}

// A table that measured nothing still writes its lists, as empty ones.
func TestAnEmptyTableWritesEmptyLists(t *testing.T) {
	b, err := DriverCostTable{Date: "d"}.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rows": []`, `"not_measured": []`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s lacks %s", b, want)
		}
	}
}
