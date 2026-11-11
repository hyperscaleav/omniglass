package labfootprint

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

var (
	gateHost  = Fingerprint{CPU: "AMD Ryzen 9 7940HS", Cores: 16, MemoryGiB: 32, Go: "go1.26.0"}
	otherHost = Fingerprint{CPU: "Intel Xeon Platinum 8370C", Cores: 4, MemoryGiB: 16, Go: "go1.26.0"}
)

// gateInputs are the inputs every fixture baseline was generated with.
func gateInputs() PerfInputs {
	return PerfInputs{Size: "ci", Scenario: "corporate", Systems: 300, WarmupS: 120, WindowS: 300, FleetReads: 20, PlanDigest: "plan-a"}
}

// cleanRun is one clean run of the ci size on the gate's host: every exact
// figure 0, every gated figure set.
func cleanRun() ScaleReport {
	gaps := make([]closedloop.GapTotal, 0, len(closedloop.GapNames()))
	for _, g := range closedloop.GapNames() {
		gaps = append(gaps, closedloop.GapTotal{Gap: g})
	}
	loss := 0.0
	host := gateHost
	return ScaleReport{
		Build: "v1", Scenario: "corporate", Systems: 300, WarmupS: 120, WindowS: 300,
		PlanDigest: "plan-a", Host: &host,
		MetricRowsPerMinute:   30000,
		PropertyRowsPerMinute: 600,
		DatabaseBytesPerDay:   3e9,
		WALBytesPerSample:     200,
		SamplesPerBatch:       40,
		Containers:            []ScaleContainer{{Name: "server", Millicores: 400, WorkingSetMiB: 300}, {Name: "db", Millicores: 200}},
		IngestLag:             Quantiles{P95: 900, N: 100},
		FleetView:             Quantiles{P95: 80, N: 20},
		Endpoints: &closedloop.EndpointReport{
			Protocols: []closedloop.EndpointProtocol{{Protocol: "http", Latency: closedloop.Quantiles{P95: 12}}},
			Nodes:     []closedloop.EndpointNode{{Node: "edge-hq"}},
			GapTotals: gaps,
		},
		Delivery: []DeliveryRow{{Hop: "ingest", Kind: "metric", In: 10, Out: 10, Loss: &loss}},
	}
}

// runsWith is n clean runs, each changed by edit with its index.
func runsWith(n int, edit func(i int, r *ScaleReport)) []ScaleReport {
	out := make([]ScaleReport, n)
	for i := range out {
		out[i] = cleanRun()
		if edit != nil {
			edit(i, &out[i])
		}
	}
	return out
}

// baselineOf is a baseline generated from runs, failing the test on a refusal.
func baselineOf(t *testing.T, runs []ScaleReport) Baseline {
	t.Helper()
	b, err := GenerateBaseline(gateInputs(), runs, nil)
	if err != nil {
		t.Fatalf("GenerateBaseline: %v", err)
	}
	return b
}

func figureNamed(t *testing.T, v PerfVerdict, name string) FigureVerdict {
	t.Helper()
	for _, f := range v.Figures {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("verdict has no figure %q; has %v", name, figureNames(v))
	return FigureVerdict{}
}

func figureNames(v PerfVerdict) []string {
	var out []string
	for _, f := range v.Figures {
		out = append(out, f.Name)
	}
	return out
}

func setServer(r *ScaleReport, millicores float64) {
	for i := range r.Containers {
		if r.Containers[i].Name == "server" {
			r.Containers[i].Millicores = millicores
		}
	}
}

// Every exact figure fails at any nonzero value, whatever the baseline holds
// and on any host.
func TestComparePerfExactFigureFailsAtAnyNonzeroValue(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	half := 0.5
	cases := []struct {
		name   string
		figure string
		edit   func(r *ScaleReport)
	}{
		{"a closed-loop gap", "closed_loop_loss/" + closedloop.GapSentNotReceived, func(r *ScaleReport) {
			r.Endpoints.GapTotals[1].Loss = 1
		}},
		{"no ingest lag measured", "ingest_lag_not_measured", func(r *ScaleReport) {
			r.IngestLagGap = &LagGap{Reason: lagGapReason}
		}},
		{"a redelivery", "redeliveries", func(r *ScaleReport) { r.Redeliveries.Abandoned = 2 }},
		{"a delivery loss", "delivery_loss/ingest/metric", func(r *ScaleReport) { r.Delivery[0].Loss = &half }},
		{"delivery read with no kind to account", "delivery_loss", func(r *ScaleReport) { r.Delivery = nil }},
		{"delivery read with no kind carrying items", "delivery_loss", func(r *ScaleReport) { r.Delivery[0].Loss = nil }},
		{"the closed loop unread", "closed_loop_loss/" + closedloop.GapConnectNotAccepted, func(r *ScaleReport) {
			r.Endpoints, r.EndpointsGap = nil, "no node answered"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runs := runsWith(3, func(i int, r *ScaleReport) {
				r.Host = &otherHost
				if i == 2 {
					tc.edit(r)
				}
			})
			v := ComparePerf(base, runs, nil)
			if v.Status != PerfFail || !v.Failed() {
				t.Fatalf("status %s, want fail", v.Status)
			}
			if f := figureNamed(t, v, tc.figure); f.Status != PerfFail || f.Class != ClassExact {
				t.Fatalf("%s reads %s (%s), want an exact fail", tc.figure, f.Status, f.Class)
			}
		})
	}
}

// A host-independent figure gates on any host: 25 percent over a 15 percent
// tolerance fails even on a host that is not the baseline's.
func TestComparePerfHostIndependentFigureGatesOnAnyHost(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(_ int, r *ScaleReport) {
		r.Host = &otherHost
		r.DatabaseBytesPerDay *= 1.25
	})
	v := ComparePerf(base, runs, nil)
	f := figureNamed(t, v, "database_bytes_per_day_per_system")
	if f.Status != PerfFail || f.Tolerance != 0.15 || v.Status != PerfFail {
		t.Fatalf("figure %s at tolerance %v, verdict %s; want fail at 0.15", f.Status, f.Tolerance, v.Status)
	}
	if f.Observed != 1.25e7 || f.Baseline != 1e7 {
		t.Fatalf("observed %v against %v, want the per-system 1.25e7 against 1e7", f.Observed, f.Baseline)
	}
}

// A host-dependent figure 30 percent over fails on the baseline's host and
// reads reported on any other, where it does not fail the check.
func TestComparePerfHostDependentFigureGatesOnlyOnItsHost(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	slow := func(host Fingerprint) []ScaleReport {
		return runsWith(3, func(_ int, r *ScaleReport) {
			h := host
			h.Go = "go1.27.0"
			r.Host = &h
			setServer(r, 520)
		})
	}
	v := ComparePerf(base, slow(gateHost), nil)
	if f := figureNamed(t, v, "server_millicores"); f.Status != PerfFail || v.Status != PerfFail {
		t.Fatalf("on the baseline's host: figure %s, verdict %s; want fail", f.Status, v.Status)
	}
	v = ComparePerf(base, slow(otherHost), nil)
	if f := figureNamed(t, v, "server_millicores"); f.Status != PerfReported {
		t.Fatalf("on another host: figure %s, want reported", f.Status)
	}
	if v.Status != PerfPass || v.Failed() {
		t.Fatalf("on another host: verdict %s, want pass", v.Status)
	}
}

// An excess past the tolerance but inside the combined spread (the
// baseline's plus the fresh runs') is noise the runs cannot tell from a
// regression, so it passes.
func TestComparePerfExcessInsideTheCombinedSpreadPasses(t *testing.T) {
	// Baseline millicores 370, 400, 430: median 400, spread 60. Tolerance 15
	// percent is 60.
	base := baselineOf(t, runsWith(3, func(i int, r *ScaleReport) { setServer(r, 370+30*float64(i)) }))
	// Fresh 455, 470, 480: median 470, excess 70 over 60, spread 25. The
	// combined spread 85 holds the excess.
	fresh := []float64{455, 470, 480}
	v := ComparePerf(base, runsWith(3, func(i int, r *ScaleReport) { setServer(r, fresh[i]) }), nil)
	f := figureNamed(t, v, "server_millicores")
	if f.Status != PerfPass {
		t.Fatalf("figure %s (excess %v, spreads %v and %v), want pass", f.Status, f.Delta, f.BaselineSpread, f.Spread)
	}
	if f.BaselineSpread != 60 || f.Spread != 25 || f.Delta != 70 {
		t.Fatalf("spreads %v and %v, delta %v; want 60, 25, 70", f.BaselineSpread, f.Spread, f.Delta)
	}
}

// A lower-is-better figure better than its tolerance reads improved, so the
// reviewer knows to refresh the baseline; it never fails.
func TestComparePerfImprovementReadsImproved(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	v := ComparePerf(base, runsWith(3, func(_ int, r *ScaleReport) { r.FleetView.P95 = 40 }), nil)
	if f := figureNamed(t, v, "fleet_view_p95_ms"); f.Status != PerfImproved {
		t.Fatalf("figure %s, want improved", f.Status)
	}
	if v.Status != PerfPass {
		t.Fatalf("verdict %s, want pass", v.Status)
	}
}

// A rate that falls past its tolerance is a change in what the lab does, not
// an improvement: rows a minute are two-sided.
func TestComparePerfRowsAMinuteAreTwoSided(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	v := ComparePerf(base, runsWith(3, func(_ int, r *ScaleReport) { r.MetricRowsPerMinute *= 0.5 }), nil)
	f := figureNamed(t, v, "metric_rows_per_minute_per_system")
	if f.Status != PerfFail || f.Tolerance != 0.02 {
		t.Fatalf("figure %s at %v, want fail at 0.02", f.Status, f.Tolerance)
	}
}

// Fresh runs whose own spread exceeds the tolerance could not have seen a
// regression of that size: the figure reads noisy, not pass, even with its
// median inside the tolerance. Noisy does not fail the check, and the table
// lists it first.
func TestComparePerfSpreadPastTheToleranceReadsNoisy(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	lag := []float64{700, 900, 1300}
	v := ComparePerf(base, runsWith(3, func(i int, r *ScaleReport) { r.IngestLag.P95 = lag[i] }), nil)
	f := figureNamed(t, v, "ingest_lag_p95_ms")
	if f.Status != PerfNoisy || f.Observed != 900 {
		t.Fatalf("figure %s at median %v, want noisy at 900", f.Status, f.Observed)
	}
	if v.Status != PerfNoisy || v.Failed() {
		t.Fatalf("verdict %s, want noisy and not failed", v.Status)
	}
	if v.Figures[0].Name != "ingest_lag_p95_ms" {
		t.Fatalf("first row %s, want the noisy figure first", v.Figures[0].Name)
	}
}

// A run that is not clean, or not the baseline's fleet, is invalid before
// any figure is compared, and fails the check.
func TestComparePerfInvalidRunIsRefusedBeforeAnyFigure(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	cases := []struct {
		name string
		edit func(r *ScaleReport)
		want string
	}{
		{"a node restarted", func(r *ScaleReport) { r.Endpoints.Nodes[0].Restarted = true }, "edge-hq restarted"},
		{"a node impaired", func(r *ScaleReport) { r.Endpoints.Nodes[0].Impaired = true }, "edge-hq carried an impairment"},
		{"an impaired gap", func(r *ScaleReport) { r.Endpoints.GapTotals[0].Impaired = 3 }, "impaired"},
		{"another scenario", func(r *ScaleReport) { r.Scenario, r.Systems = "dev", 0 },
			"the run measured dev at 0 systems; the baseline is corporate at 300"},
		{"another size", func(r *ScaleReport) { r.Systems = 1000 },
			"the run measured corporate at 1000 systems; the baseline is corporate at 300"},
		{"another window", func(r *ScaleReport) { r.WindowS = 60 }, "1m0s window"},
		{"another plan", func(r *ScaleReport) { r.PlanDigest = "plan-b" }, "device set"},
		{"no plan recorded", func(r *ScaleReport) { r.PlanDigest = "" }, "recorded no device set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runs := runsWith(3, func(i int, r *ScaleReport) {
				r.MetricRowsPerMinute *= 2
				if i == 1 {
					tc.edit(r)
				}
			})
			v := ComparePerf(base, runs, nil)
			if v.Status != PerfInvalid || !v.Failed() {
				t.Fatalf("status %s, want invalid", v.Status)
			}
			if len(v.Figures) != 0 {
				t.Fatalf("an invalid run compared %v", figureNames(v))
			}
			if !strings.Contains(strings.Join(v.Invalid, "\n"), tc.want) {
				t.Fatalf("reasons %q, want %q", v.Invalid, tc.want)
			}
		})
	}
	if v := ComparePerf(base, nil, nil); v.Status != PerfInvalid {
		t.Fatalf("no runs: status %s, want invalid", v.Status)
	}
	// A baseline that names no device set was not generated by this gate,
	// so nothing it holds can be said to measure the runs' fleet.
	unplanned := base
	unplanned.Inputs.PlanDigest = ""
	if v := ComparePerf(unplanned, runsWith(3, nil), nil); v.Status != PerfInvalid ||
		!strings.Contains(strings.Join(v.Invalid, "\n"), "baseline names no device set") {
		t.Fatalf("a baseline with no plan: status %s (%v), want invalid", v.Status, v.Invalid)
	}
}

// The same scenario at the same size compares.
func TestComparePerfSameSizeCompares(t *testing.T) {
	v := ComparePerf(baselineOf(t, runsWith(3, nil)), runsWith(3, nil), nil)
	if v.Status != PerfPass || len(v.Invalid) != 0 {
		t.Fatalf("status %s (%v), want pass", v.Status, v.Invalid)
	}
}

// A figure the baseline holds that the run did not measure fails; one the
// run measured that the baseline does not hold is reported.
func TestComparePerfFigureMissingFromEitherSide(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	v := ComparePerf(base, runsWith(3, func(_ int, r *ScaleReport) {
		r.FleetView = Quantiles{}
		r.Endpoints.Protocols = append(r.Endpoints.Protocols, closedloop.EndpointProtocol{Protocol: "ssh", Latency: closedloop.Quantiles{P95: 30}})
	}), nil)
	if f := figureNamed(t, v, "fleet_view_p95_ms"); f.Status != PerfFail || !strings.Contains(f.Note, "not measured") {
		t.Fatalf("missing figure %s (%q), want fail not measured", f.Status, f.Note)
	}
	if f := figureNamed(t, v, "endpoint_latency_p95_ms/ssh"); f.Status != PerfReported {
		t.Fatalf("new figure %s, want reported", f.Status)
	}
}

// A caller's tolerance overrides the table's for that family.
func TestComparePerfToleranceOverride(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(_ int, r *ScaleReport) { r.DatabaseBytesPerDay *= 1.06 })
	v := ComparePerf(base, runs, Tolerances{"database_bytes_per_day_per_system": 0.1})
	if f := figureNamed(t, v, "database_bytes_per_day_per_system"); f.Status != PerfPass {
		t.Fatalf("figure %s at 10 percent, want pass", f.Status)
	}
}

// Generation records each gated figure's median and spread over the runs and
// the host, and the file it writes compares every figure as pass against the
// runs it came from.
func TestGenerateBaselineRoundTripsThroughComparePerf(t *testing.T) {
	runs := runsWith(3, func(i int, r *ScaleReport) {
		setServer(r, 380+20*float64(i))
		r.MetricRowsPerMinute += float64(100 * i)
	})
	base := baselineOf(t, runs)
	if base.Host.CPU != gateHost.CPU || base.Host.Build != "v1" || base.Inputs != gateInputs() || base.Runs != 3 {
		t.Fatalf("baseline host %+v inputs %+v runs %d", base.Host, base.Inputs, base.Runs)
	}
	var mc BaselineFigure
	for _, f := range base.Figures {
		if f.Name == "server_millicores" {
			mc = f
		}
	}
	if mc.Median != 400 || mc.Spread != 40 || len(mc.Values) != 3 {
		t.Fatalf("server_millicores %+v, want median 400 spread 40 over 3 values", mc)
	}

	var buf bytes.Buffer
	if err := base.Write(&buf); err != nil {
		t.Fatal(err)
	}
	read, err := ReadBaseline(&buf)
	if err != nil {
		t.Fatal(err)
	}
	v := ComparePerf(read, runs, nil)
	if v.Status != PerfPass {
		t.Fatalf("round trip %s", v.Status)
	}
	gated := 0
	for _, f := range v.Figures {
		if f.Status != PerfPass {
			t.Errorf("%s reads %s against its own runs", f.Name, f.Status)
		}
		if f.Class != ClassExact {
			gated++
		}
	}
	if gated != len(base.Figures) {
		t.Fatalf("compared %d gated figures, baseline holds %d", gated, len(base.Figures))
	}
}

// Generation refuses, naming the figure, when a gated figure's runs spread
// past its tolerance; it refuses a run that fails an exact figure, and one
// that is not clean.
func TestGenerateBaselineRefuses(t *testing.T) {
	cases := []struct {
		name string
		runs []ScaleReport
		want string
	}{
		{"a spread past the tolerance", runsWith(3, func(i int, r *ScaleReport) { setServer(r, 300+100*float64(i)) }),
			"server_millicores"},
		{"a nonzero exact figure", runsWith(3, func(i int, r *ScaleReport) { r.Redeliveries.Unconfirmed = i }),
			"redeliveries"},
		{"an impaired node", runsWith(3, func(_ int, r *ScaleReport) { r.Endpoints.Nodes[0].Impaired = true }),
			"impairment"},
		{"another size", runsWith(3, func(_ int, r *ScaleReport) { r.Systems = 1000 }), "1000"},
		{"no runs", nil, "no runs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GenerateBaseline(gateInputs(), tc.runs, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want one naming %q", err, tc.want)
			}
		})
	}
}

// A baseline's runs are each from one host; a host-dependent baseline from
// runs on two hosts would fingerprint neither.
func TestGenerateBaselineRefusesMixedHosts(t *testing.T) {
	runs := runsWith(3, func(i int, r *ScaleReport) {
		if i == 2 {
			r.Host = &otherHost
		}
	})
	if _, err := GenerateBaseline(gateInputs(), runs, nil); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("err %v, want a refusal naming the host", err)
	}
}

// A sampling run's series (#1632) compares its last N samples by the same
// rules; a failed sample among them makes the check invalid.
func TestReadSampleReportsTakesTheLastN(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	var series bytes.Buffer
	at := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	for i, rows := range []float64{1, 30000, 30000, 30000} {
		rep := cleanRun()
		rep.MetricRowsPerMinute = rows
		line, err := ScaleSample{Seq: i + 1, At: at.Add(time.Duration(i) * 5 * time.Minute), ScaleReport: &rep}.Line()
		if err != nil {
			t.Fatal(err)
		}
		series.Write(line)
	}
	whole := series.Bytes()

	reports, err := ReadSampleReports(bytes.NewReader(whole), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 3 {
		t.Fatalf("read %d reports, want 3", len(reports))
	}
	if v := ComparePerf(base, reports, nil); v.Status != PerfPass {
		t.Fatalf("last 3 of the series: verdict %s, want pass", v.Status)
	}

	// The first sample, cut by the warmup, spreads the rows past their
	// tolerance once it is among the N read.
	reports, err = ReadSampleReports(bytes.NewReader(whole), 4)
	if err != nil {
		t.Fatal(err)
	}
	v := ComparePerf(base, reports, nil)
	if f := figureNamed(t, v, "metric_rows_per_minute_per_system"); f.Status != PerfNoisy {
		t.Fatalf("all 4: figure %s, want noisy", f.Status)
	}

	failed, _ := ScaleSample{Seq: 5, At: at.Add(time.Hour), Error: "sign in: 503"}.Line()
	reports, err = ReadSampleReports(bytes.NewReader(append(append([]byte{}, whole...), failed...)), 3)
	if err == nil || !strings.Contains(err.Error(), "sample 5") {
		t.Fatalf("a failed sample read as %d reports, err %v; want a refusal naming sample 5", len(reports), err)
	}

	if _, err := ReadSampleReports(bytes.NewReader(whole), 9); err == nil || !strings.Contains(err.Error(), "4 samples") {
		t.Fatalf("err %v, want a refusal naming the 4 samples held", err)
	}
}

// The verdict table names every figure with its class, its numbers and its
// status, noisy and failed rows before the rest.
func TestPerfVerdictTable(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	v := ComparePerf(base, runsWith(3, func(_ int, r *ScaleReport) { r.Redeliveries.Abandoned = 1 }), nil)
	var buf bytes.Buffer
	v.WriteTable(&buf)
	out := buf.String()
	for _, want := range []string{"perf gate: fail", "redeliveries", "exact", "metric_rows_per_minute_per_system", "host-independent", "server_millicores", "host-dependent"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "redeliveries") > strings.Index(out, "metric_rows_per_minute_per_system") {
		t.Errorf("the failed row is not first:\n%s", out)
	}

	inv := ComparePerf(base, runsWith(1, func(_ int, r *ScaleReport) { r.Scenario, r.Systems = "dev", 0 }), nil)
	buf.Reset()
	inv.WriteTable(&buf)
	if !strings.Contains(buf.String(), "invalid: the run measured dev at 0 systems; the baseline is corporate at 300") {
		t.Errorf("invalid table:\n%s", buf.String())
	}
	if _, err := json.Marshal(v); err != nil {
		t.Fatal(err)
	}
}

// The facts the docs render are the tables the gate runs: every family once,
// with its class and the tolerance it gates at.
func TestPerfGateFactsAreTheTables(t *testing.T) {
	facts := PerfGateFacts()
	seen := map[string]bool{}
	classes := map[PerfClass]int{}
	for _, f := range facts.Figures {
		if seen[f.Family] {
			t.Fatalf("family %s twice", f.Family)
		}
		seen[f.Family] = true
		classes[f.Class]++
		if f.Class != ClassExact && (f.Tolerance <= 0 || f.Tolerance > 0.25) {
			t.Errorf("%s gates at %v", f.Family, f.Tolerance)
		}
		if f.About == "" {
			t.Errorf("%s has no description", f.Family)
		}
	}
	if classes[ClassExact] == 0 || classes[ClassHostIndependent] == 0 || classes[ClassHostDependent] == 0 {
		t.Fatalf("classes %v, want all three", classes)
	}
	if len(facts.Sizes) != 2 || facts.Sizes[0].File != "corporate-300.ci.json" {
		t.Fatalf("sizes %+v", facts.Sizes)
	}
}

// The docs render the gate's tables from docs/src/generated/perfgate.json,
// written by make gen; a table changed without regenerating it fails here.
func TestPerfGateFactsJSONIsCommitted(t *testing.T) {
	committed, err := os.ReadFile("../../docs/src/generated/perfgate.json")
	if err != nil {
		t.Fatalf("read committed perfgate.json (run make gen and commit it): %v", err)
	}
	want, err := PerfGateFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(committed) != string(want) {
		t.Error("docs/src/generated/perfgate.json drifted from labfootprint.PerfGateFacts: run make gen and commit the result")
	}
}
