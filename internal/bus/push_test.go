package bus

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// testRegistry is the minimal registry these cases resolve against.
func testRegistry(t *testing.T) collection.Registry {
	t.Helper()
	return collection.NewRegistry([]storage.MetricType{{Name: "icmp-rtt-avg"}}, nil, nil)
}

// pushOwner is what apiBinding produces: a component, and no interface labels.
func pushOwner() storage.TaskOwner {
	return storage.TaskOwner{ComponentID: "boardroom-a-bar"}
}

// nodeOwner is what ResolveTaskOwner produces: the interface supplies the instance
// discriminator and the source.
func nodeOwner() storage.TaskOwner {
	return storage.TaskOwner{ComponentID: "disp-1", EndpointName: "api", Transport: "icmp"}
}

// The trust model in one place: a batch's owner is believed because of the SUBJECT
// it arrived on, never because the field is populated. apiBinding is the API lane's
// half of that, so these pin what it will and will not accept.

func TestAPIBindingRequiresAnAddressableOwner(t *testing.T) {
	cases := []struct {
		name  string
		batch *ogv1.TelemetryBatch
	}{
		{"no owner at all", &ogv1.TelemetryBatch{}},
		{"owner with no kind", &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Ref: "disp-1"}}},
		{"owner with no ref", &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Kind: "component"}}},
		// Guarded until #422 widens the lane derivations past owner_kind=component.
		// Accepting these would write a sample to the wrong arc, which is worse than
		// refusing.
		{"system owner, not yet supported", &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Kind: "system", Ref: "boardroom-a"}}},
		{"location owner, not yet supported", &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Kind: "location", Ref: "hq-west"}}},
		{"node owner, not yet supported", &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Kind: "node", Ref: "edge-hq"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := apiBinding(c.batch); ok {
				t.Fatalf("apiBinding accepted %s; it must refuse rather than guess an arc", c.name)
			}
		})
	}
}

// A push has ONE owner, unlike the node lane where log lines are node-owned
// self-logs while samples belong to the task's component.
func TestAPIBindingOwnsBothLanesOfTheBatch(t *testing.T) {
	bind, ok := apiBinding(&ogv1.TelemetryBatch{
		Owner: &ogv1.Owner{Kind: "component", Ref: "boardroom-a-bar"},
	})
	if !ok {
		t.Fatal("apiBinding refused a well-formed component owner")
	}
	if bind.LogComponent != "boardroom-a-bar" || bind.LogNode != "" {
		t.Fatalf("log destination = component %q / node %q, want component boardroom-a-bar only", bind.LogComponent, bind.LogNode)
	}
	if bind.SampleOwner == nil || bind.SampleOwner.ComponentID != "boardroom-a-bar" {
		t.Fatalf("sample owner = %+v, want component boardroom-a-bar", bind.SampleOwner)
	}
	// A push has no interface, so the interface-derived fallbacks must be empty and
	// the batch's own source/instance are what land.
	if bind.SampleOwner.EndpointName != "" || bind.SampleOwner.Transport != "" {
		t.Fatalf("push binding invented interface labels: %+v", bind.SampleOwner)
	}
}

// The wire value wins where supplied, else the interface-derived value. This is what
// keeps the node lane byte-for-byte unchanged while letting a push express both.
func TestDeriveMetricsInstanceAndSourcePrecedence(t *testing.T) {
	reg := testRegistry(t)

	t.Run("push supplies instance and source", func(t *testing.T) {
		metrics := deriveMetrics(&ogv1.TelemetryBatch{
			Source:  "webex-cloud",
			Metrics: []*ogv1.MetricSample{{Name: "icmp-rtt-avg", Instance: "mic-1", Value: 12.4}},
		}, pushOwner(), reg)
		if len(metrics) != 1 {
			t.Fatalf("got %d metrics, want 1", len(metrics))
		}
		if metrics[0].Instance != "mic-1" || metrics[0].Source != "webex-cloud" {
			t.Fatalf("instance/source = %q/%q, want mic-1/webex-cloud", metrics[0].Instance, metrics[0].Source)
		}
	})

	t.Run("node lane falls back to the interface", func(t *testing.T) {
		metrics := deriveMetrics(&ogv1.TelemetryBatch{
			Metrics: []*ogv1.MetricSample{{Name: "icmp-rtt-avg", Value: 12.4}},
		}, nodeOwner(), reg)
		if len(metrics) != 1 {
			t.Fatalf("got %d metrics, want 1", len(metrics))
		}
		if metrics[0].Instance != "api" || metrics[0].Source != "icmp" {
			t.Fatalf("instance/source = %q/%q, want the interface's api/icmp", metrics[0].Instance, metrics[0].Source)
		}
	})
}

// A log line's own source wins, else the batch's.
func TestLogLineSourceFallsBackToTheBatch(t *testing.T) {
	logs := logLineWrites(&ogv1.TelemetryBatch{
		Source: "webex-cloud",
		Logs: []*ogv1.LogLine{
			{Message: "explicit", Source: "media"},
			{Message: "inherited"},
		},
	}, "boardroom-a-bar")
	if len(logs) != 2 {
		t.Fatalf("got %d log writes, want 2", len(logs))
	}
	if logs[0].Source != "media" {
		t.Fatalf("explicit line source = %q, want media", logs[0].Source)
	}
	if logs[1].Source != "webex-cloud" {
		t.Fatalf("inherited line source = %q, want the batch's webex-cloud", logs[1].Source)
	}
	for _, l := range logs {
		if l.OwnerKind != "component" || l.OwnerID != "boardroom-a-bar" {
			t.Fatalf("log owner = %s/%s, want the authorized component", l.OwnerKind, l.OwnerID)
		}
	}
}

// A node's self-logs resolve ts and source exactly as a push's lines do (the
// shared per-line helpers), and every write is stamped with the publishing
// node: the origin-true landing #589 split out of log_line.
func TestNodeLogWritesStampTheNode(t *testing.T) {
	logs := nodeLogWrites(&ogv1.TelemetryBatch{
		Source: "node",
		Logs: []*ogv1.LogLine{
			{Message: "explicit", Source: "collection", Severity: "warning"},
			{Message: "inherited"},
		},
	}, "site-a")
	if len(logs) != 2 {
		t.Fatalf("got %d node log writes, want 2", len(logs))
	}
	if logs[0].Source != "collection" || logs[0].Severity != "warning" {
		t.Fatalf("explicit line = %+v, want source collection severity warning", logs[0])
	}
	if logs[1].Source != "node" {
		t.Fatalf("inherited line source = %q, want the batch's node", logs[1].Source)
	}
	for _, l := range logs {
		if l.Node != "site-a" {
			t.Fatalf("node log stamped %q, want the publishing node site-a", l.Node)
		}
	}
}
