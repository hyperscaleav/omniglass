package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestAWorklistCarriesTheCollectionReportWindow: a node's worklist carries the
// platform's collection report window (#1369), the default a minute, so an
// admin's change reaches every node on its next pull with nothing cached.
func TestAWorklistCarriesTheCollectionReportWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	wl, err := gw.NodeWorklist(ctx, "edge-unknown")
	if err != nil {
		t.Fatal(err)
	}
	if wl.ReportSeconds != 60 {
		t.Fatalf("the worklist's report window = %d seconds, want the default 60", wl.ReportSeconds)
	}
	if _, err := gw.UpsertSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": 30}, nil); err != nil {
		t.Fatalf("override: %v", err)
	}
	if wl, err = gw.NodeWorklist(ctx, "edge-unknown"); err != nil || wl.ReportSeconds != 30 {
		t.Fatalf("after an override, the worklist's report window = %d seconds (err %v), want 30", wl.ReportSeconds, err)
	}
}

// TestTheWebhookReceiversCountsLandOnTheEndpointsComponent: a window's webhook
// counts (#1369) land as the component's metric series under the endpoint's
// name, observed, written by the process; a refusal count only when the
// window carries one. The push lane may not write them: the server counts
// its own posts.
func TestTheWebhookReceiversCountsLandOnTheEndpointsComponent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "ucc-1"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	two, one, zero := 2, 1, 0
	at := time.Now().UTC().Truncate(time.Second)
	if err := gw.RecordWebhookCounts(ctx, []storage.WebhookCount{
		{ComponentID: c.ID, Endpoint: "webhook", Posts: &two, Refusals: &one, At: at},
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	for key, want := range map[string]float64{writerclass.WebhookPostCount: 2, writerclass.WebhookRefusalCount: 1} {
		m, err := gw.LatestMetricInstance(ctx, "ucc-1", key, "webhook")
		if err != nil || m == nil || m.Value != want || m.Provenance != "observed" || !m.TS.Equal(at) {
			t.Fatalf("%s = %+v (err %v), want %v observed at %v", key, m, err, want, at)
		}
	}
	if err := gw.RecordWebhookCounts(ctx, []storage.WebhookCount{{ComponentID: c.ID, Endpoint: "webhook", Posts: &zero, At: at.Add(time.Minute)}}); err != nil {
		t.Fatalf("record the zero: %v", err)
	}
	if m, _ := gw.LatestMetricInstance(ctx, "ucc-1", writerclass.WebhookPostCount, "webhook"); m == nil || m.Value != 0 {
		t.Fatalf("the quiet window's post count = %+v, want 0", m)
	}
	if m, _ := gw.LatestMetricInstance(ctx, "ucc-1", writerclass.WebhookRefusalCount, "webhook"); m == nil || m.Value != 1 || !m.TS.Equal(at) {
		t.Fatalf("a window with no refusal wrote one: latest = %+v, want the first window's 1", m)
	}

	err = gw.InsertMetricSamples(ctx, storage.PushWriter(), []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: c.ID, Key: writerclass.WebhookPostCount, Instance: "webhook", Value: 99}})
	if !errors.Is(err, storage.ErrKeyWriterRefused) {
		t.Fatalf("a push writing the webhook post count = %v, want refused", err)
	}
}

// TestADeletedComponentCostsOnlyItsOwnWebhookCounts: a window carrying a
// component deleted since its last post (a decommissioned device, a lab torn
// down) lands every other component's counts and drops only the deleted
// one's, without an error: the receiver's window is not lost to it.
func TestADeletedComponentCostsOnlyItsOwnWebhookCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	live, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "ucc-live"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "ucc-gone"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.DeleteComponent(ctx, "", gone.Name, all, all); err != nil {
		t.Fatalf("delete: %v", err)
	}
	five := 5
	at := time.Now().UTC().Truncate(time.Second)
	if err := gw.RecordWebhookCounts(ctx, []storage.WebhookCount{
		{ComponentID: gone.ID, Endpoint: "webhook", Posts: &five, At: at},
		{ComponentID: live.ID, Endpoint: "webhook", Posts: &five, At: at},
	}); err != nil {
		t.Fatalf("a deleted component failed the window: %v", err)
	}
	if m, _ := gw.LatestMetricInstance(ctx, "ucc-live", writerclass.WebhookPostCount, "webhook"); m == nil || m.Value != 5 {
		t.Fatalf("the live component's posts = %+v, want 5 beside a deleted one", m)
	}
}
