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
	"cmp"
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
	if rep.Scenario != lab.MeasureScenario || rep.Systems != lab.SmokeSystems {
		t.Errorf("the footprint names %s at %d systems, want %s at %d (#1719)",
			rep.Scenario, rep.Systems, lab.MeasureScenario, lab.SmokeSystems)
	}
	if rep.Tasks == 0 || rep.Build == "" {
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
	for _, unread := range unreadScaleFigures(rep, lab.MeasureScenario, lab.SmokeSystems) {
		t.Error(unread)
	}
}

// TestIntegrationScaleDelivery is the delivery read end to end (#1641): a
// healthy scale lab's nodes stamp each batch's ledger, the server records it
// on the receipt, and the read reconciles every kind across the node hop and
// the ingest hop (#1692) to exactly 0 loss, with the end-to-end line and the receipts' footprint read.
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
	if rep.DeliveryNotRead != "" || len(rep.DeliveryUnsettled) > 0 || len(rep.DeliveryEnded) > 0 {
		t.Fatalf("delivery not read (%q), unsettled %+v or unshipped %+v", rep.DeliveryNotRead, rep.DeliveryUnsettled, rep.DeliveryEnded)
	}
	measured := 0
	for _, d := range rep.Delivery {
		if d.Loss == nil {
			continue
		}
		measured++
		if *d.Loss != 0 || d.Outcomes[delivery.Missing] != 0 {
			t.Errorf("%s across %s: loss %v, outcomes %v; a healthy lab loses nothing", cmp.Or(string(d.Kind), d.Protocol), d.Hop, *d.Loss, d.Outcomes)
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

// TestIntegrationScaleDeliveryInducedUnregistered is the refusal read end to
// end (#1698): a scale lab brought up with one device answering a key no
// driver registers loses property samples at the ingest hop, the table names
// refused-unregistered as the cause, and metrics, which the induced poll does
// not touch, still lose nothing.
func TestIntegrationScaleDeliveryInducedUnregistered(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the induced refusal's integration test runs through make test-lab (it needs the image make builds)")
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
		Scenario: "corporate", Systems: 3, ReadyTimeout: 5 * time.Minute,
		InduceUnregistered: true,
		Warmup:             30 * time.Second, Window: time.Minute, FleetReads: 1,
		SettleTimeout: 3 * time.Minute,
		Out:           &progress, LabLog: &labLog,
	})
	if err != nil {
		t.Fatalf("the scale run failed: %v\n%s\nthe lab:\n%s", err, progress.String(), labLog.String())
	}
	table := rep.Table()
	t.Logf("%s\n%s", progress.String(), table)
	if rep.DeliveryNotRead != "" || len(rep.DeliveryUnsettled) > 0 {
		t.Fatalf("delivery not read (%q) or unsettled %+v", rep.DeliveryNotRead, rep.DeliveryUnsettled)
	}
	var property, metric bool
	for _, d := range rep.Delivery {
		if d.Loss == nil {
			continue
		}
		switch d.Kind {
		case delivery.Property:
			property = true
			if *d.Loss <= 0 || d.Cause != delivery.RefusedUnregistered || d.Outcomes[delivery.RefusedUnregistered] == 0 {
				t.Errorf("property across %s: loss %v, cause %q, outcomes %v; want a loss caused by refused-unregistered", d.Hop, *d.Loss, d.Cause, d.Outcomes)
			}
		case delivery.Metric:
			metric = true
			if *d.Loss != 0 {
				t.Errorf("metric across %s: loss %v, outcomes %v; the induced poll emits no metric", d.Hop, *d.Loss, d.Outcomes)
			}
		}
	}
	if !property || !metric {
		t.Errorf("property read %v, metric read %v: want both kinds carrying a ratio: %+v", property, metric, rep.Delivery)
	}
	if !strings.Contains(table, string(delivery.RefusedUnregistered)) {
		t.Errorf("the table does not name the cause:\n%s", table)
	}
}
