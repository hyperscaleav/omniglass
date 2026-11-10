//go:build labintegration

package labfootprint

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/lab"
)

// TestIntegrationScaleEndpointsCloseTheLoop measures a 20-system corporate
// scale lab and holds its closed loop shut: every protocol the nodes poll
// drew requests, no poll target went unhit, every gap is zero, and no face
// saw two requests at once. It is what a fake cannot prove: real nodes, real
// sims and real receipts, bracketed through the server's API. The sim hop
// of the delivery table reads 0 on every protocol (#1694).
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
		Scenario: "corporate", Systems: 20, ReadyTimeout: 5 * time.Minute,
		Warmup: time.Minute, Window: 2 * time.Minute, FleetReads: 2,
		Out: &progress, LabLog: &labLog,
	})
	if err != nil {
		t.Fatalf("the scale measurement failed: %v\n%s\nthe lab:\n%s", err, progress.String(), labLog.String())
	}
	t.Logf("%s\n%s", progress.String(), rep.Table())
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
	// The sim hop (#1694): every request the sims settled came back.
	simRows := 0
	for _, d := range rep.Delivery {
		if d.Hop != delivery.Sim {
			continue
		}
		simRows++
		if d.Loss == nil || *d.Loss != 0 {
			t.Errorf("%s across the sim hop: in %d, out %d, outcomes %v; a healthy lab loses nothing", d.Protocol, d.In, d.Out, d.Outcomes)
		}
	}
	if simRows == 0 {
		t.Errorf("the delivery table has no sim row: %+v", rep.Delivery)
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
