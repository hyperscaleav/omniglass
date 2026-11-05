package labcheck_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The lab-level check's judgment is a pure function of a snapshot of what a
// lab holds, so it is proven here without a lab: the live read that fills the
// snapshot is proven against a fake API in collect_test.go, and against a real
// lab by `make test-lab`.

// presentOf marks every key the transport and driver shape expect as present,
// the full built-in data an answered endpoint carries.
func presentOf(tr string, shape transport.DriverShape, answered bool) map[string]bool {
	out := map[string]bool{}
	for _, k := range transport.ExpectedBuiltIn(tr, shape, answered) {
		out[k] = true
	}
	return out
}

func healthy() labcheck.Snapshot {
	shape := transport.DriverShape{Polls: true}
	return labcheck.Snapshot{
		Components: []labcheck.Component{{
			Name: "Display 1", Product: "test-panel-1",
			Endpoints: []labcheck.Endpoint{{Name: "ssh", Transport: "ssh", Answered: true, Shape: shape, Present: presentOf("ssh", shape, true)}},
			Values: []labcheck.Value{
				{Key: "model-number", Class: writerclass.Collected},
				{Key: "uptime", Class: writerclass.Collected},
				{Key: "power-state", Class: writerclass.Collected},
				{Key: probekeys.TCPOpen, Class: writerclass.NodeProbe},
			},
		}},
		Systems: []labcheck.System{{Name: "Boardroom", Values: []labcheck.Value{{Key: "booking-count", Class: writerclass.Collected}}}},
	}
}

func render(fs []labcheck.Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return strings.Join(out, "\n")
}

func TestAHealthyLabHasNoFindings(t *testing.T) {
	if fs := labcheck.Evaluate(healthy(), nil); len(fs) != 0 {
		t.Fatalf("findings on a healthy lab:\n%s", render(fs))
	}
}

func TestAComponentWithNoEndpointFailsBuiltInNamingItAndTheTier(t *testing.T) {
	s := healthy()
	s.Components[0].Endpoints = nil
	fs := labcheck.Evaluate(s, nil)
	if len(fs) != 1 || fs[0].Tier != devseed.TierBuiltIn || !strings.Contains(fs[0].String(), "Display 1") {
		t.Fatalf("findings = %s, want one built-in finding naming Display 1", render(fs))
	}
}

func TestAMissingBuiltInKeyNamesTheEndpointAndTheKey(t *testing.T) {
	s := healthy()
	delete(s.Components[0].Endpoints[0].Present, probekeys.PollDuration)
	fs := labcheck.Evaluate(s, nil)
	if len(fs) != 1 || fs[0].Tier != devseed.TierBuiltIn {
		t.Fatalf("findings = %s, want one built-in finding", render(fs))
	}
	for _, want := range []string{"Display 1", "ssh", probekeys.PollDuration} {
		if !strings.Contains(fs[0].String(), want) {
			t.Errorf("%q does not name %q", fs[0], want)
		}
	}
}

func TestADownEndpointOwesNoTimingOrPollDuration(t *testing.T) {
	s := healthy()
	shape := transport.DriverShape{Polls: true}
	s.Components[0].Endpoints[0].Answered = false
	s.Components[0].Endpoints[0].Present = presentOf("ssh", shape, false)
	if fs := labcheck.Evaluate(s, nil); len(fs) != 0 {
		t.Fatalf("a down endpoint with its rungs landed owes nothing more:\n%s", render(fs))
	}
}

func TestAComponentWithOnlyBuiltInDataFailsCollected(t *testing.T) {
	s := healthy()
	s.Components[0].Values = []labcheck.Value{{Key: probekeys.TCPOpen, Class: writerclass.NodeProbe}, {Key: probekeys.PollDuration, Class: writerclass.NodeProbe}}
	fs := labcheck.Evaluate(s, nil)
	if len(fs) != 1 || fs[0].Tier != devseed.TierCollected || !strings.Contains(fs[0].String(), "Display 1") {
		t.Fatalf("findings = %s, want one collected finding naming Display 1", render(fs))
	}
}

func TestAComponentWithOnlyIdentityAndUptimeFailsCollected(t *testing.T) {
	s := healthy()
	s.Components[0].Values = []labcheck.Value{
		{Key: "model-number", Class: writerclass.Collected}, {Key: "serial-number", Class: writerclass.Collected},
		{Key: "firmware-version", Class: writerclass.Collected}, {Key: "uptime", Class: writerclass.Collected},
		{Key: probekeys.TCPOpen, Class: writerclass.NodeProbe},
	}
	fs := labcheck.Evaluate(s, nil)
	if len(fs) != 1 || fs[0].Tier != devseed.TierCollected {
		t.Fatalf("findings = %s, want one collected finding", render(fs))
	}
}

func TestACalcValueIsNotACollectedClassSignal(t *testing.T) {
	s := healthy()
	s.Components[0].Values = []labcheck.Value{{Key: "health", Class: writerclass.Platform}, {Key: "rollup", Class: writerclass.Calc}}
	if fs := labcheck.Evaluate(s, nil); len(fs) != 1 || fs[0].Tier != devseed.TierCollected {
		t.Fatalf("findings = %s, want the collected tier to fail", render(fs))
	}
}

func TestASystemWithNoValueOfItsOwnFailsTheSystemTier(t *testing.T) {
	s := healthy()
	s.Systems[0].Values = []labcheck.Value{{Key: "health", Class: writerclass.Platform}}
	fs := labcheck.Evaluate(s, nil)
	if len(fs) != 1 || fs[0].Tier != devseed.TierSystem || !strings.Contains(fs[0].String(), "Boardroom") {
		t.Fatalf("findings = %s, want one system finding naming Boardroom", render(fs))
	}
}

func TestAKnownGapIsSkippedAndNamedByItsIssue(t *testing.T) {
	s := healthy()
	s.Components[0].Values = nil
	gaps := map[devseed.Tier]map[string]devseed.TierGap{devseed.TierCollected: {"test-panel-1": {Issue: "#9"}}}
	if fs := labcheck.Evaluate(s, gaps); len(fs) != 0 {
		t.Fatalf("a listed gap still fails:\n%s", render(fs))
	}
}

func TestAGapEntryWhoseOwnerHasTelemetryIsStale(t *testing.T) {
	gaps := map[devseed.Tier]map[string]devseed.TierGap{devseed.TierCollected: {"test-panel-1": {Issue: "#9"}}}
	fs := labcheck.Evaluate(healthy(), gaps)
	if len(fs) != 1 || fs[0].Tier != devseed.TierCollected || !strings.Contains(fs[0].String(), "stale") || !strings.Contains(fs[0].String(), "#9") {
		t.Fatalf("findings = %s, want one stale collected finding citing #9", render(fs))
	}
}

func TestAGapStaysLiveWhileAnyComponentOfTheProductStillLacksTheTier(t *testing.T) {
	s := healthy()
	other := s.Components[0]
	other.Name, other.Values = "Display 2", nil
	s.Components = append(s.Components, other)
	gaps := map[devseed.Tier]map[string]devseed.TierGap{devseed.TierCollected: {"test-panel-1": {Issue: "#9"}}}
	if fs := labcheck.Evaluate(s, gaps); len(fs) != 0 {
		t.Fatalf("a gap half closed is not stale yet:\n%s", render(fs))
	}
}

func TestTheAllSystemsGapCoversEverySystemAndGoesStaleTogether(t *testing.T) {
	gaps := map[devseed.Tier]map[string]devseed.TierGap{devseed.TierSystem: {devseed.AllSystems: {Issue: "#1361"}}}
	s := healthy()
	s.Systems[0].Values = nil
	if fs := labcheck.Evaluate(s, gaps); len(fs) != 0 {
		t.Fatalf("the all-systems gap does not cover a bare system:\n%s", render(fs))
	}
	fs := labcheck.Evaluate(healthy(), gaps)
	if len(fs) != 1 || fs[0].Tier != devseed.TierSystem || !strings.Contains(fs[0].String(), "#1361") {
		t.Fatalf("findings = %s, want the all-systems gap stale", render(fs))
	}
}

func TestFindingsAreOrderedSoARunReadsTheSameTwice(t *testing.T) {
	s := healthy()
	for _, n := range []string{"Z", "A", "M"} {
		c := s.Components[0]
		c.Name, c.Values = n, nil
		s.Components = append(s.Components, c)
	}
	fs := labcheck.Evaluate(s, nil)
	if len(fs) != 3 || !strings.Contains(fs[0].String(), "A") || !strings.Contains(fs[2].String(), "Z") {
		t.Fatalf("findings out of order:\n%s", render(fs))
	}
}
