//go:build labintegration

// The lab footprint against a real Docker daemon: the integration test that
// closes the harness's capability gate (the cgroup reads through the Engine,
// psql in the database container, the live CPU limit and the teardown check
// are what a fake cannot prove). It needs the binary and image `make` builds
// and runs through make test-lab, on short windows: it proves every figure is
// read, never what the figures are.
package labfootprint

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/lab"
)

func TestIntegrationLabFootprint(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the lab footprint's integration test runs through make test-lab (it needs the image make builds)")
	}
	home, _ := os.UserHomeDir()
	daemon, err := lab.ResolveDaemon(lab.Env{Getenv: os.Getenv, ReadFile: os.ReadFile, Home: home, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	var progress, labLog bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	rep, err := Measure(ctx, Config{
		Binary: "../../bin/omniglass", DockerHost: daemon.Host,
		Project:  fmt.Sprintf("ogfoot-it-%d", os.Getpid()),
		Scenario: lab.MeasureScenario, Systems: lab.SmokeSystems, Cadence: lab.CadenceDefault,
		Warmup: 20 * time.Second, Window: 40 * time.Second, Windows: 2,
		ReadHours: []int{24, 720}, ReadsEach: 1, ReadCPUs: 1,
		Out: &progress, LabLog: &labLog,
	})
	if err != nil {
		t.Fatalf("the lab footprint failed: %v\n%s\nthe lab:\n%s", err, progress.String(), labLog.String())
	}
	t.Logf("%s\n%s", progress.String(), rep.Table())
	if rep.Scenario != lab.MeasureScenario {
		t.Errorf("the footprint ran the %q scenario, want %q (#1719)", rep.Scenario, lab.MeasureScenario)
	}
	if rep.Systems == 0 || rep.Tasks == 0 || rep.Build == "" {
		t.Errorf("the run's conditions are unread: %+v", rep)
	}
	if rep.MetricRowsPerMinute.N != 2 || rep.MetricRowsPerMinute.Median <= 0 {
		t.Errorf("metric rows a minute %+v, want a positive median of 2 windows", rep.MetricRowsPerMinute)
	}
	if rep.DatabaseBytesPerDay.Max <= 0 || rep.WALBytesPerDay.Median <= 0 {
		t.Errorf("database %+v, WAL %+v: want growth", rep.DatabaseBytesPerDay, rep.WALBytesPerDay)
	}
	for _, c := range rep.Containers {
		if c.Millicores.Median <= 0 || c.WorkingSetMiB.Median <= 0 {
			t.Errorf("%s: cpu %+v memory %+v, want both read", c.Name, c.Millicores, c.WorkingSetMiB)
		}
	}
	if len(rep.ReadLatencyMS) != 2 || rep.ReadLatencyMS[0].Stat.N == 0 || rep.ReadLatencyMS[1].Stat.N == 0 {
		t.Errorf("reads %+v, want the 24 h and 30 d reads timed", rep.ReadLatencyMS)
	}
}

// assertScaleFiguresRead fails a scale run at the ladder's smoke size whose
// report leaves a figure unread, or names a fleet other than the one the lab
// was brought up as (#1719). It asserts that each figure is read, never what
// it is.
func assertScaleFiguresRead(t *testing.T, rep ScaleReport) {
	t.Helper()
	if rep.Scenario != lab.MeasureScenario || rep.Systems != lab.SmokeSystems {
		t.Errorf("the run names %s at %d systems, want %s at %d from the lab's labels",
			rep.Scenario, rep.Systems, lab.MeasureScenario, lab.SmokeSystems)
	}
	if rep.Build == "" || rep.Sites == 0 || rep.Components == 0 || rep.Tasks == 0 {
		t.Errorf("the run's conditions are unread: build %q, %d sites, %d components, %d tasks",
			rep.Build, rep.Sites, rep.Components, rep.Tasks)
	}
	if rep.MetricRowsPerMinute <= 0 || rep.BatchesLanded == 0 || rep.SamplesLanded == 0 {
		t.Errorf("metric rows a minute %v, %d batches, %d samples: want ingest read",
			rep.MetricRowsPerMinute, rep.BatchesLanded, rep.SamplesLanded)
	}
	if rep.DatabaseBytesPerDay <= 0 || rep.WALBytesPerDay <= 0 {
		t.Errorf("database %v, WAL %v a day: want growth", rep.DatabaseBytesPerDay, rep.WALBytesPerDay)
	}
	for _, c := range rep.Containers {
		if c.Millicores <= 0 || c.WorkingSetMiB <= 0 {
			t.Errorf("%s: cpu %v memory %v, want both read", c.Name, c.Millicores, c.WorkingSetMiB)
		}
	}
	if rep.IngestLagGap != nil {
		t.Errorf("ingest lag not measured: %+v", rep.IngestLagGap)
	}
	if !rep.ConsumerLane.Measured {
		t.Errorf("the consumer lane is unread: %s", rep.ConsumerLane.Reason)
	}
	if rep.FleetView.N == 0 {
		t.Errorf("fleet view %+v, want the reads timed", rep.FleetView)
	}
	if rep.Absolutes.DatabaseBytes <= 0 {
		t.Errorf("absolutes %+v, want the database's size read", rep.Absolutes)
	}
	if rep.DeliveryNotRead != "" {
		t.Errorf("delivery not read: %s", rep.DeliveryNotRead)
	}
}

// TestIntegrationScaleDelivery is the delivery read end to end (#1641): a
// healthy scale lab's nodes stamp each batch's ledger, the server records it
// on the receipt, and the read reconciles every kind across the ingest hop to
// exactly 0 loss, with the end-to-end line and the receipts' footprint read.
func TestIntegrationScaleDelivery(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the scale delivery read's integration test runs through make test-lab (it needs the image make builds)")
	}
	home, _ := os.UserHomeDir()
	daemon, err := lab.ResolveDaemon(lab.Env{Getenv: os.Getenv, ReadFile: os.ReadFile, Home: home, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	var progress, labLog bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	rep, err := MeasureScale(ctx, ScaleConfig{
		Binary: "../../bin/omniglass", DockerHost: daemon.Host,
		Scenario: lab.MeasureScenario, Systems: 3, ReadyTimeout: 5 * time.Minute,
		Warmup: 30 * time.Second, Window: time.Minute, FleetReads: 1,
		SettleTimeout: 3 * time.Minute,
		Out:           &progress, LabLog: &labLog,
	})
	if err != nil {
		t.Fatalf("the scale run failed: %v\n%s\nthe lab:\n%s", err, progress.String(), labLog.String())
	}
	t.Logf("%s\n%s", progress.String(), rep.Table())
	if rep.DeliveryNotRead != "" || len(rep.DeliveryUnsettled) > 0 {
		t.Fatalf("delivery not read (%q) or unsettled %+v", rep.DeliveryNotRead, rep.DeliveryUnsettled)
	}
	measured := 0
	for _, d := range rep.Delivery {
		if d.Loss == nil {
			continue
		}
		measured++
		if *d.Loss != 0 || d.Outcomes[delivery.Missing] != 0 {
			t.Errorf("%s across %s: loss %v, outcomes %v; a healthy lab loses nothing", d.Kind, d.Hop, *d.Loss, d.Outcomes)
		}
	}
	if measured == 0 {
		t.Errorf("no kind carried a ratio: %+v", rep.Delivery)
	}
	if !strings.Contains(rep.Table(), "end to end") {
		t.Errorf("the table has no end-to-end line:\n%s", rep.Table())
	}
	if rep.ReceiptBytesPerDay <= 0 {
		t.Errorf("receipts a day %v, want the receipts' growth read", rep.ReceiptBytesPerDay)
	}
}
