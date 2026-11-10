//go:build labintegration

package labfootprint

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/lab"
)

// TestIntegrationScaleEndpointsCloseTheLoop measures a scale lab at the
// ladder's smoke size (corporate at 20 systems, #1719, no drain probe), checks
// every figure is read and the fleet is named from the lab's labels, and holds
// its closed loop shut: every protocol the nodes poll
// drew requests, no poll target went unhit, every gap is zero, and no face
// saw two requests at once. It is what a fake cannot prove: real nodes, real
// sims and real receipts, bracketed through the server's API.
func TestIntegrationScaleEndpointsCloseTheLoop(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the scale lab's closed loop runs through make test-lab (it needs the image make builds)")
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
		Scenario: lab.MeasureScenario, Systems: lab.SmokeSystems, ReadyTimeout: 5 * time.Minute,
		Warmup: time.Minute, Window: 2 * time.Minute, FleetReads: 2, Backlog: 0,
		SettleTimeout: 3 * time.Minute,
		Out:           &progress, LabLog: &labLog,
	})
	if err != nil {
		t.Fatalf("the scale measurement failed: %v\n%s\nthe lab:\n%s", err, progress.String(), labLog.String())
	}
	t.Logf("%s\n%s", progress.String(), rep.Table())
	assertScaleFiguresRead(t, rep)
	e := rep.Endpoints
	if e == nil {
		t.Fatalf("the endpoints were not measured: %s", rep.EndpointsGap)
	}
	polled := 0
	for _, p := range e.Protocols {
		if p.PollTargets > 0 {
			polled++
			if p.Received == 0 && p.Accepted == 0 {
				t.Errorf("%s: %d poll targets drew no request", p.Protocol, p.PollTargets)
			}
		}
		if p.OverlapRatio != 0 {
			t.Errorf("%s overlap-ratio %.3f, want 0", p.Protocol, p.OverlapRatio)
		}
	}
	if polled == 0 {
		t.Errorf("no protocol carries a poll target: %+v", e.Protocols)
	}
	for _, g := range e.GapTotals {
		if g.Loss != 0 || g.Impaired != 0 {
			t.Errorf("%s: loss %d, impaired %d, want 0", g.Gap, g.Loss, g.Impaired)
		}
	}
	for _, g := range e.Gaps {
		if g.Gap == closedloop.GapUnhitEndpoint && g.Reading != closedloop.ReadingNotObserved && g.Count > 0 {
			t.Errorf("unhit endpoint: %+v", g)
		}
		if g.Reading == closedloop.ReadingRestarted {
			t.Errorf("a node restarted inside the window: %+v", g)
		}
	}
}
