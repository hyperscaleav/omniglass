package bus

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// An outpost's tick carries its report of itself in the outpost-self block (#1625):
// the consumer lands it on the publishing outpost's platform component, never on
// an owner the batch names, and a refusal drops the report alone while the
// tick's task groups land.

// platformComponent is outpost-a's platform component id.
func (f *tickFixture) platformComponent(t *testing.T) string {
	t.Helper()
	n, err := f.gw.GetOutpost(f.ctx, "outpost-a", scope.Set{All: true})
	if err != nil {
		t.Fatalf("read outpost-a: %v", err)
	}
	if n.PlatformComponentID == nil {
		t.Fatal("outpost-a has no platform component")
	}
	return *n.PlatformComponentID
}

func TestOutpostSelfReportLandsOnThePlatformComponent(t *testing.T) {
	f := newTickFixture(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{OutpostId: "outpost-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1)},
		OutpostSelf: &ogv1.OutpostSelf{Metrics: []*ogv1.MetricSample{
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

// An outpost-self sample that names an identity claims an owner the report does
// not have: the block drops with a warning, and the tick's groups land.
func TestOutpostSelfReportNamingAnIdentityIsDropped(t *testing.T) {
	f := newTickFixture(t)
	logs := captureWarnings(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{OutpostId: "outpost-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1)},
		OutpostSelf: &ogv1.OutpostSelf{Metrics: []*ogv1.MetricSample{
			{Name: "tick-duration", Value: 12},
			{Name: "tick-duration", Value: 99, Identity: &ogv1.Identity{Key: "serial", Value: "x"}},
		}}}))

	if got, err := f.gw.LatestMetric(f.ctx, f.platformComponent(t), "tick-duration"); err != nil || got != nil {
		t.Errorf("platform component tick-duration = %+v (%v), want nothing: the block named an identity", got, err)
	}
	if got := f.tcpConnectTime(t, "disp-1"); got == nil {
		t.Error("disp-1 tcp-connect-time missing: the dropped report took the tick's group with it")
	}
	if warned := logs(outpostSelfOwnerLog); len(warned) != 1 || warned[0]["outpost"] != "outpost-a" {
		t.Errorf("identity warnings = %v, want one naming outpost-a", warned)
	}
}

// An outpost-lane batch naming an owner is Term'd whole, its outpost-self block with
// it: the report's owner is the publishing outpost's platform component, never a
// claim the batch makes.
func TestOutpostSelfReportWithABatchOwnerIsTermed(t *testing.T) {
	f := newTickFixture(t)
	m := f.deliver(t, &ogv1.TelemetryBatch{OutpostId: "outpost-a",
		Owner:       &ogv1.Owner{Kind: "component", Ref: "disp-1"},
		OutpostSelf: &ogv1.OutpostSelf{Metrics: []*ogv1.MetricSample{{Name: "tick-duration", Value: 12}}}})
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

// A report key the outpost does not own (health is the platform's own) is
// refused by the gateway: the report drops alone and is warned as the
// outpost-self report, the tick acks, and its groups land and stamp seen.
func TestOutpostSelfReportRefusedIsWarnedAndTheTickLands(t *testing.T) {
	f := newTickFixture(t)
	logs := captureWarnings(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{OutpostId: "outpost-a",
		Groups:      []*ogv1.TaskGroup{group("t-1", 1)},
		OutpostSelf: &ogv1.OutpostSelf{Metrics: []*ogv1.MetricSample{{Name: "health", Value: 1}}}}))

	if got, err := f.gw.LatestMetric(f.ctx, f.platformComponent(t), "health"); err != nil || got != nil {
		t.Errorf("platform component health = %+v (%v), want nothing: an outpost does not write it", got, err)
	}
	if got := f.tcpConnectTime(t, "disp-1"); got == nil {
		t.Error("disp-1 tcp-connect-time missing: the refused report took the tick's group with it")
	}
	warned := logs(outpostSelfRefusedLog)
	if len(warned) != 1 || !strings.Contains(fmt.Sprint(warned[0]["error"]), "outpost-self") {
		t.Errorf("outpost-self refusal warnings = %v, want one naming its cause", warned)
	}
	if g := logs(refusedGroupLog); len(g) != 0 {
		t.Errorf("task group refusal warnings = %v, want none: no task group was refused", g)
	}
	if got, want := f.seen(t), f.stamped("disp-1-tcp"); !slices.Equal(got, want) {
		t.Errorf("seen stamps = %v, want disp-1-tcp alone %v", got, want)
	}
}
