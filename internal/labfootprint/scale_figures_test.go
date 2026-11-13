package labfootprint

import (
	"fmt"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// unreadScaleFigures names each figure a scale report left unread, for the
// fleet the run was asked for: what the scale integrations assert of a healthy
// corporate lab (#1719), read and never valued. It is a pure function over the
// report so a unit test proves it names a figure that is missing, without a lab.
func unreadScaleFigures(rep ScaleReport, scenario string, systems int) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	if rep.Scenario != scenario || rep.Systems != systems {
		add("the run names %s at %d systems, want %s at %d from the lab's labels", rep.Scenario, rep.Systems, scenario, systems)
	}
	if rep.Build == "" || rep.Sites == 0 || rep.Components == 0 || rep.Tasks == 0 {
		add("the run's conditions are unread: build %q, %d sites, %d components, %d tasks", rep.Build, rep.Sites, rep.Components, rep.Tasks)
	}
	if rep.MetricRowsPerMinute <= 0 || rep.BatchesLanded == 0 || rep.SamplesLanded == 0 {
		add("metric rows a minute %v, %d batches, %d samples: want ingest read", rep.MetricRowsPerMinute, rep.BatchesLanded, rep.SamplesLanded)
	}
	if rep.DatabaseBytesPerDay <= 0 || rep.WALBytesPerDay <= 0 {
		add("database %v, WAL %v a day: want growth", rep.DatabaseBytesPerDay, rep.WALBytesPerDay)
	}
	if len(rep.Containers) == 0 {
		add("no container's cpu or memory was read")
	}
	for _, c := range rep.Containers {
		if c.Millicores <= 0 || c.WorkingSetMiB <= 0 {
			add("%s: cpu %v memory %v, want both read", c.Name, c.Millicores, c.WorkingSetMiB)
		}
	}
	if rep.IngestLagGap != nil {
		add("ingest lag not measured: %+v", rep.IngestLagGap)
	}
	if !rep.ConsumerLane.Measured {
		add("the consumer lane is unread: %s", rep.ConsumerLane.Reason)
	}
	if rep.FleetView.N == 0 {
		add("fleet view %+v, want the reads timed", rep.FleetView)
	}
	if rep.Absolutes.DatabaseBytes <= 0 {
		add("absolutes %+v, want the database's size read", rep.Absolutes)
	}
	if rep.ReceiptBytesPerDay <= 0 {
		add("receipts a day %v, want the receipts' growth read", rep.ReceiptBytesPerDay)
	}
	if rep.DeliveryNotRead != "" {
		add("delivery not read: %s", rep.DeliveryNotRead)
	} else if !slices.ContainsFunc(rep.Delivery, func(d DeliveryRow) bool { return d.Loss != nil }) {
		add("no kind carried a delivery ratio: %+v", rep.Delivery)
	}
	return out
}

// A scale report every figure of which was read names nothing; one that
// left any single figure unread names it, so the integrations' "every figure
// is read" fails on the figure the lab did not give.
func TestUnreadScaleFiguresNamesEachMissingFigure(t *testing.T) {
	read := func() ScaleReport {
		ratio := 0.0
		return ScaleReport{
			Build: "b1", Scenario: lab.MeasureScenario, Systems: lab.SmokeSystems,
			Sites: 3, Components: 60, Tasks: 180,
			MetricRowsPerMinute: 900, BatchesLanded: 40, SamplesLanded: 1600,
			DatabaseBytesPerDay: 1 << 30, WALBytesPerDay: 2 << 30, ReceiptBytesPerDay: 1 << 20,
			Containers:   []ScaleContainer{{Name: "server", Millicores: 50, WorkingSetMiB: 90}},
			ConsumerLane: Lane{Measured: true},
			FleetView:    Quantiles{P50: 4, N: 2},
			Absolutes:    Absolutes{DatabaseBytes: 1 << 26},
			Delivery:     []DeliveryRow{{Loss: &ratio}},
		}
	}
	if got := unreadScaleFigures(read(), lab.MeasureScenario, lab.SmokeSystems); len(got) != 0 {
		t.Fatalf("a fully read report names %q, want nothing", got)
	}
	for name, unread := range map[string]func(*ScaleReport){
		"a dev scenario":           func(r *ScaleReport) { r.Scenario = lab.DefaultScenario },
		"another size":             func(r *ScaleReport) { r.Systems = 300 },
		"no build":                 func(r *ScaleReport) { r.Build = "" },
		"no metric rows":           func(r *ScaleReport) { r.MetricRowsPerMinute = 0 },
		"no WAL growth":            func(r *ScaleReport) { r.WALBytesPerDay = 0 },
		"no receipt growth":        func(r *ScaleReport) { r.ReceiptBytesPerDay = 0 },
		"no container read":        func(r *ScaleReport) { r.Containers = nil },
		"a container's cpu unread": func(r *ScaleReport) { r.Containers[0].Millicores = 0 },
		"no ingest lag":            func(r *ScaleReport) { r.IngestLagGap = &LagGap{Reason: "none landed"} },
		"no consumer lane":         func(r *ScaleReport) { r.ConsumerLane = Lane{Reason: "one tally"} },
		"no fleet view":            func(r *ScaleReport) { r.FleetView = Quantiles{} },
		"no database size":         func(r *ScaleReport) { r.Absolutes = Absolutes{} },
		"no receipts read":         func(r *ScaleReport) { r.DeliveryNotRead = "no receipts read" },
		"no kind carried a ratio":  func(r *ScaleReport) { r.Delivery = []DeliveryRow{{}} },
	} {
		r := read()
		unread(&r)
		if got := unreadScaleFigures(r, lab.MeasureScenario, lab.SmokeSystems); len(got) != 1 {
			t.Errorf("%s: names %q, want the one figure", name, got)
		}
	}
}
