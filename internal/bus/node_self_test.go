package bus

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// A node's tick carries its report of itself in the node-self block (#1625):
// the consumer lands it on the publishing node's platform component, never on
// an owner the batch names, and a refusal drops the report alone while the
// tick's task groups land.

// platformComponent is node-a's platform component id.
func (f *tickFixture) platformComponent(t *testing.T) string {
	t.Helper()
	n, err := f.gw.GetNode(f.ctx, "node-a", scope.Set{All: true})
	if err != nil {
		t.Fatalf("read node-a: %v", err)
	}
	if n.PlatformComponentID == nil {
		t.Fatal("node-a has no platform component")
	}
	return *n.PlatformComponentID
}

func TestNodeSelfReportLandsOnThePlatformComponent(t *testing.T) {
	f := newTickFixture(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1)},
		NodeSelf: &ogv1.NodeSelf{Metrics: []*ogv1.MetricSample{
			{Name: "tick-duration", Value: 12},
			{Name: "session-held-count", Value: 2, Instance: "open"},
		}}}))

	comp := f.platformComponent(t)
	if got, err := f.gw.LatestMetric(f.ctx, comp, "tick-duration"); err != nil || got == nil || got.Value != 12 {
		t.Errorf("platform component tick-duration = %+v (%v), want 12", got, err)
	}
	if got, err := f.gw.LatestMetricInstance(f.ctx, comp, "session-held-count", "open"); err != nil || got == nil || got.Value != 2 {
		t.Errorf("platform component session-held-count under open = %+v (%v), want 2", got, err)
	}
	if got := f.tcpConnectTime(t, "disp-1"); got == nil || got.Value != 1 {
		t.Errorf("disp-1 tcp-connect-time = %+v, want the tick's group landed beside the report", got)
	}
}

// A node-self sample that names an identity claims an owner the report does
// not have: the block drops with a warning, and the tick's groups land.
func TestNodeSelfReportNamingAnIdentityIsDropped(t *testing.T) {
	f := newTickFixture(t)
	logs := captureWarnings(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1)},
		NodeSelf: &ogv1.NodeSelf{Metrics: []*ogv1.MetricSample{
			{Name: "tick-duration", Value: 12},
			{Name: "tick-duration", Value: 99, Identity: &ogv1.Identity{Key: "serial", Value: "x"}},
		}}}))

	if got, err := f.gw.LatestMetric(f.ctx, f.platformComponent(t), "tick-duration"); err != nil || got != nil {
		t.Errorf("platform component tick-duration = %+v (%v), want nothing: the block named an identity", got, err)
	}
	if got := f.tcpConnectTime(t, "disp-1"); got == nil {
		t.Error("disp-1 tcp-connect-time missing: the dropped report took the tick's group with it")
	}
	if warned := logs(nodeSelfOwnerLog); len(warned) != 1 || warned[0]["node"] != "node-a" {
		t.Errorf("identity warnings = %v, want one naming node-a", warned)
	}
}

// A node-lane batch naming an owner is Term'd whole, its node-self block with
// it: the report's owner is the publishing node's platform component, never a
// claim the batch makes.
func TestNodeSelfReportWithABatchOwnerIsTermed(t *testing.T) {
	f := newTickFixture(t)
	m := f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Owner:    &ogv1.Owner{Kind: "component", Ref: "disp-1"},
		NodeSelf: &ogv1.NodeSelf{Metrics: []*ogv1.MetricSample{{Name: "tick-duration", Value: 12}}}})
	if !m.termed || m.acked || m.naked {
		t.Fatalf("acked=%v termed=%v naked=%v, want termed", m.acked, m.termed, m.naked)
	}
	if len(f.store.lands) != 0 {
		t.Fatalf("a termed batch reached LandBatch: %+v", f.store.lands)
	}
	for _, comp := range []string{"disp-1", f.platformComponent(t)} {
		if got, err := f.gw.LatestMetric(f.ctx, comp, "tick-duration"); err != nil || got != nil {
			t.Errorf("%s tick-duration = %+v (%v), want nothing from a termed batch", comp, got, err)
		}
	}
}

// A report key the node does not own (health is the platform's own) is
// refused by the gateway: the report drops alone and is warned as the
// node-self report, the tick acks, and its groups land and stamp seen.
func TestNodeSelfReportRefusedIsWarnedAndTheTickLands(t *testing.T) {
	f := newTickFixture(t)
	logs := captureWarnings(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Groups:   []*ogv1.TaskGroup{group("t-1", 1)},
		NodeSelf: &ogv1.NodeSelf{Metrics: []*ogv1.MetricSample{{Name: "health", Value: 1}}}}))

	if got, err := f.gw.LatestMetric(f.ctx, f.platformComponent(t), "health"); err != nil || got != nil {
		t.Errorf("platform component health = %+v (%v), want nothing: a node does not write it", got, err)
	}
	if got := f.tcpConnectTime(t, "disp-1"); got == nil {
		t.Error("disp-1 tcp-connect-time missing: the refused report took the tick's group with it")
	}
	warned := logs(nodeSelfRefusedLog)
	if len(warned) != 1 || !strings.Contains(fmt.Sprint(warned[0]["error"]), "node-self") {
		t.Errorf("node-self refusal warnings = %v, want one naming its cause", warned)
	}
	if g := logs(refusedGroupLog); len(g) != 0 {
		t.Errorf("task group refusal warnings = %v, want none: no task group was refused", g)
	}
	if len(f.store.stamps) != 1 || len(f.store.stamps[0]) != 1 || f.store.stamps[0][0] != f.endpoints["disp-1-tcp"] {
		t.Errorf("seen stamps = %v, want disp-1-tcp alone", f.store.stamps)
	}
}
