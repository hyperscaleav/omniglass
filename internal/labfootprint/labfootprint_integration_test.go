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
	"testing"
	"time"

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
		Scenario: lab.DefaultScenario, Cadence: lab.CadenceDefault,
		Warmup: 20 * time.Second, Window: 40 * time.Second, Windows: 2,
		ReadHours: []int{24, 720}, ReadsEach: 1, ReadCPUs: 1,
		Out: &progress, LabLog: &labLog,
	})
	if err != nil {
		t.Fatalf("the lab footprint failed: %v\n%s\nthe lab:\n%s", err, progress.String(), labLog.String())
	}
	t.Logf("%s\n%s", progress.String(), rep.Table())
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
