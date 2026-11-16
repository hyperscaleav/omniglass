package labfootprint

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
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
	f := driverCostFacts(targets, skips, nil, table)
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
	f := driverCostFacts(targets, nil, nil, DriverCostTable{Rows: []DriverCost{{Driver: "a", Polls: []string{"1m0s"}, MemoryPerTaskBytes: 1024}}})
	b, err := json.Marshal(f.Drivers[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"thread_cpu_per_run_ms":0`, `"thread_share":0`, `"millicores_per_task":0`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s lacks %s", b, want)
		}
	}
	f = driverCostFacts(targets[:0], []DriverSkip{{Driver: "s", Reason: SkipNoPolls}}, nil, DriverCostTable{})
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

// Each row carries the step it was measured at, since a driver re-swept at
// a bigger step reads at that one, and a warm cost that did not show is a
// blank with its reason, not a zero.
func TestADriversFactsCarryItsStepAndAMissingWarm(t *testing.T) {
	targets := []DriverTarget{{Driver: "a", Product: "pa", Polls: []time.Duration{time.Minute}}}
	f := driverCostFacts(targets, nil, nil, DriverCostTable{Rows: []DriverCost{{Driver: "a", Polls: []string{"1m0s"}, StepTasks: 1600,
		MemoryPerTaskBytes: 4096, WarmNotMeasured: "a memory: the loaded line starts below the idle node"}}})
	d := f.Drivers[0]
	if d.StepTasks != 1600 {
		t.Fatalf("step %d, want 1600", d.StepTasks)
	}
	if d.MemoryWarmMiB != nil || d.MemoryPerTaskKiB == nil || !strings.Contains(d.Reason, "below the idle node") {
		t.Fatalf("fact %+v, want memory per task, no warm, and the warm's reason", d)
	}
}

// Each driver reads with its kind, the transport it speaks, and what one
// endpoint on it holds: a task per poll, so its memory per task times its
// polls. A held session is per endpoint, so that is the figure that sizes it.
func TestADriversFactsCarryItsKindAndItsEndpoint(t *testing.T) {
	targets := []DriverTarget{{Driver: "a", Product: "pa", Polls: []time.Duration{30 * time.Second, 30 * time.Second, 5 * time.Minute}}}
	skips := []DriverSkip{{Driver: "s", Reason: SkipNoPolls}}
	f := driverCostFacts(targets, skips, map[string]string{"a": "tcp", "s": "webhook"},
		DriverCostTable{Rows: []DriverCost{{Driver: "a", Polls: []string{"30s", "30s", "5m0s"}, MemoryPerTaskBytes: 60 * 1024, CPUPerRunMillis: 2}}})
	a, s := f.Drivers[0], f.Drivers[1]
	if a.Kind != "tcp" || s.Kind != "webhook" {
		t.Fatalf("kinds %q and %q, want tcp and webhook", a.Kind, s.Kind)
	}
	if a.MemoryPerEndpointKiB == nil || *a.MemoryPerEndpointKiB != 180 {
		t.Fatalf("a's endpoint = %v, want 60 KiB a task times 3 polls", a.MemoryPerEndpointKiB)
	}
	f = driverCostFacts(targets, nil, nil, DriverCostTable{Rows: []DriverCost{{Driver: "a", Polls: []string{"30s", "30s", "5m0s"}, MemoryNotMeasured: "a memory: not a line"}}})
	if f.Drivers[0].MemoryPerEndpointKiB != nil {
		t.Fatal("a driver with no memory per task has no memory per endpoint")
	}
}

// The kind table is keyed by kind and interval: each interval a kind's swept
// drivers poll at, with the median and range of its measured drivers' cost.
// A run's CPU is the kind's; the interval is how often it runs, so the
// millicores a task holds at an interval are its CPU per run over it.
func TestTheKindTableIsKeyedByKindAndInterval(t *testing.T) {
	m := func(d time.Duration) []time.Duration { return []time.Duration{d} }
	targets := []DriverTarget{
		{Driver: "h1", Product: "p", Polls: []time.Duration{5 * time.Minute, 30 * time.Second}},
		{Driver: "h2", Product: "p", Polls: m(30 * time.Second)},
		{Driver: "h3", Product: "p", Polls: m(30 * time.Second)},
		{Driver: "h4", Product: "p", Polls: m(30 * time.Second)},
		{Driver: "n1", Product: "p", Polls: m(time.Minute)},
	}
	skips := []DriverSkip{{Driver: "w", Reason: SkipNoPolls}}
	kinds := map[string]string{"h1": "https", "h2": "https", "h3": "https", "h4": "https", "n1": "snmp", "w": "webhook"}
	f := driverCostFacts(targets, skips, kinds, DriverCostTable{Rows: []DriverCost{
		{Driver: "h1", Polls: []string{"5m0s", "30s"}, MemoryPerTaskBytes: 4 * 1024, CPUPerRunMillis: 3},
		{Driver: "h2", Polls: []string{"30s"}, MemoryPerTaskBytes: 10 * 1024, CPUPerRunMillis: 6},
		{Driver: "h3", Polls: []string{"30s"}, MemoryPerTaskBytes: 8 * 1024, CPUPerRunMillis: 9, MemoryNotMeasured: "h3 memory: not a line"},
	}, NotMeasured: []DriverSkip{{Driver: "n1", Reason: "n1 cpu: the sweep does not rise"}}})

	type key struct{ kind, interval string }
	got := map[key]KindCostFact{}
	var order []key
	for _, k := range f.Kinds {
		got[key{k.Kind, k.Interval}] = k
		order = append(order, key{k.Kind, k.Interval})
	}
	want := []key{{"https", "30s"}, {"https", "5m0s"}, {"snmp", "1m0s"}, {"webhook", ""}}
	if !slices.Equal(order, want) {
		t.Fatalf("kind rows %v, want %v", order, want)
	}

	fast, slow := got[key{"https", "30s"}], got[key{"https", "5m0s"}]
	if !slices.Equal(fast.Drivers, []string{"h1", "h2", "h3"}) || !slices.Equal(fast.PolledBy, []string{"h1", "h2", "h3", "h4"}) {
		t.Fatalf("https 30s measured by %v and polled by %v", fast.Drivers, fast.PolledBy)
	}
	if !slices.Equal(slow.PolledBy, []string{"h1"}) || !slices.Equal(slow.Drivers, fast.Drivers) {
		t.Fatalf("https 5m measured by %v and polled by %v: a run costs the kind's, at any interval", slow.Drivers, slow.PolledBy)
	}
	if c := fast.CPUPerRunMillis; c == nil || *c != (Spread{Median: 6, Min: 3, Max: 9, N: 3}) {
		t.Fatalf("https cpu per run = %+v, want the median and range of 3, 6 and 9", c)
	}
	// h3's memory did not fit, so the kind's memory reads from h1 and h2:
	// an even count takes the middle two's mean. h1 polls twice an endpoint.
	if mem := fast.MemoryPerTaskKiB; mem == nil || *mem != (Spread{Median: 7, Min: 4, Max: 10, N: 2}) {
		t.Fatalf("https memory per task = %+v, want 4 and 10", mem)
	}
	if ep := fast.MemoryPerEndpointKiB; ep == nil || *ep != (Spread{Median: 9, Min: 8, Max: 10, N: 2}) {
		t.Fatalf("https memory per endpoint = %+v, want 8 and 10", ep)
	}
	if mc := fast.MillicoresPerTask; mc == nil || *mc != (Spread{Median: 0.2, Min: 0.1, Max: 0.3, N: 3}) {
		t.Fatalf("https millicores per task at 30s = %+v, want cpu per run over 30 s", mc)
	}
	if mc := slow.MillicoresPerTask; mc == nil || *mc != (Spread{Median: 0.02, Min: 0.01, Max: 0.03, N: 3}) {
		t.Fatalf("https millicores per task at 5m = %+v, want cpu per run over 300 s", mc)
	}
	// A kind whose drivers the sweep could not measure, or does not sweep,
	// keeps its rows with no figures, so every kind the seed ships reads.
	if n := got[key{"snmp", "1m0s"}]; n.CPUPerRunMillis != nil || len(n.Drivers) != 0 || !slices.Equal(n.PolledBy, []string{"n1"}) {
		t.Fatalf("snmp = %+v, want polled by n1 and no figures", n)
	}
	if w := got[key{"webhook", ""}]; w.CPUPerRunMillis != nil || len(w.PolledBy) != 0 {
		t.Fatalf("webhook = %+v, want a row with nothing polled and no figures", w)
	}
}

// A driver whose polls changed since the sweep measured it reads as not
// measured: its slope was fit at the polls it had, so its memory per endpoint
// and its place in the kind table would be a guess. The order a spec lists
// its polls in is not a change.
func TestADriverWhosePollsChangedSinceTheSweepIsNotMeasured(t *testing.T) {
	targets := []DriverTarget{
		{Driver: "a", Product: "pa", Polls: []time.Duration{30 * time.Second, 5 * time.Minute}},
		{Driver: "b", Product: "pb", Polls: []time.Duration{30 * time.Second, 5 * time.Minute}},
	}
	kinds := map[string]string{"a": "https", "b": "https"}
	f := driverCostFacts(targets, nil, kinds, DriverCostTable{Rows: []DriverCost{
		{Driver: "a", Polls: []string{"5m0s", "30s"}, MemoryPerTaskBytes: 4096, CPUPerRunMillis: 2},
		{Driver: "b", Polls: []string{"30s", "30s", "5m0s"}, MemoryPerTaskBytes: 4096, CPUPerRunMillis: 2},
	}})
	if a := f.Drivers[0]; !a.Measured || a.MemoryPerEndpointKiB == nil || *a.MemoryPerEndpointKiB != 8 {
		t.Fatalf("a = %+v, want measured: its polls are the same, in another order", a)
	}
	if b := f.Drivers[1]; b.Measured || b.MemoryPerTaskKiB != nil || b.Reason != PollsChanged {
		t.Fatalf("b = %+v, want not measured, its polls changed", b)
	}
	if k := f.Kinds[0]; len(k.Drivers) != 1 || k.Drivers[0] != "a" {
		t.Fatalf("kind row %+v, want a alone measured", k)
	}
}

// Every driver the docs list names its kind: one with none would roll up
// into a kind with no name, so the render refuses it at make gen.
func TestADriverWithNoKindIsNamed(t *testing.T) {
	targets := []DriverTarget{{Driver: "a", Product: "pa", Polls: []time.Duration{time.Minute}}}
	skips := []DriverSkip{{Driver: "s", Reason: SkipNoPolls}}
	if got := kindlessDrivers(driverCostFacts(targets, skips, map[string]string{"a": "http"}, DriverCostTable{})); !slices.Equal(got, []string{"s"}) {
		t.Fatalf("kindless = %v, want s", got)
	}
	if got := kindlessDrivers(driverCostFacts(targets, skips, map[string]string{"a": "http", "s": "webhook"}, DriverCostTable{})); len(got) != 0 {
		t.Fatalf("kindless = %v, want none", got)
	}
}

// A committed table can merge a re-sweep into a sweep: a row measured on
// another build than the table's names it, and the docs name each other
// build with the drivers measured on it, so no row reads as measured on a
// build it was not.
func TestARowMeasuredOnAnotherBuildIsNamed(t *testing.T) {
	targets := []DriverTarget{
		{Driver: "a", Product: "pa", Polls: []time.Duration{time.Minute}},
		{Driver: "b", Product: "pb", Polls: []time.Duration{time.Minute}},
		{Driver: "c", Product: "pc", Polls: []time.Duration{time.Minute}},
	}
	f := driverCostFacts(targets, nil, nil, DriverCostTable{Build: "v1", Rows: []DriverCost{
		{Driver: "a", Polls: []string{"1m0s"}, MemoryPerTaskBytes: 1024},
		{Driver: "b", Polls: []string{"1m0s"}, MemoryPerTaskBytes: 1024, Build: "v2"},
		{Driver: "c", Polls: []string{"1m0s"}, MemoryPerTaskBytes: 1024, Build: "v1"},
	}})
	if f.Build != "v1" {
		t.Fatalf("table build %q, want v1", f.Build)
	}
	for _, d := range f.Drivers {
		want := map[string]string{"a": "", "b": "v2", "c": ""}[d.Driver]
		if d.Build != want {
			t.Fatalf("%s build %q, want %q (empty is the table's)", d.Driver, d.Build, want)
		}
	}
	if len(f.OtherBuilds) != 1 || f.OtherBuilds[0].Build != "v2" || !slices.Equal(f.OtherBuilds[0].Drivers, []string{"b"}) {
		t.Fatalf("other builds %+v, want v2: [b]", f.OtherBuilds)
	}
	b, err := DriverCostTable{Build: "v1", Rows: []DriverCost{{Driver: "a"}}}.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"build": ""`) {
		t.Fatalf("a row on the table's build writes no build: %s", b)
	}
}

// The committed table is a measurement nothing regenerates, so a driver
// renamed in the seed has to move its row by hand; a row naming no shipped
// driver is a rename that left it behind and reads as not yet measured.
func TestTheCommittedTableNamesOnlyShippedDrivers(t *testing.T) {
	tab, err := ReadDriverCostTable(shippedDriverCost)
	if err != nil {
		t.Fatal(err)
	}
	shipped := seed.ShippedDriverPolls()
	for _, r := range tab.Rows {
		if _, ok := shipped[r.Driver]; !ok {
			t.Errorf("%s names %s, which is no shipped driver: move its row to the driver's new name", DriverCostPath, r.Driver)
		}
	}
	for _, s := range tab.NotMeasured {
		if _, ok := shipped[s.Driver]; !ok {
			t.Errorf("%s names %s, which is no shipped driver: move its entry to the driver's new name", DriverCostPath, s.Driver)
		}
	}
}
