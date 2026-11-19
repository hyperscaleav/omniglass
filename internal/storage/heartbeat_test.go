package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestHeartbeatReadsWhatItSends covers the gateway side of the outbound
// heartbeat (#1986): the settings default to off, the named webhook target
// reads back for the platform's own send, an absent one is not found, the
// open platform alarms count what a stalled sweep raised, and a recorded
// outcome lands in the audit log.
func TestHeartbeatReadsWhatItSends(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)

	hb, err := gw.HeartbeatSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hb.Target != "" || hb.IntervalSeconds != 60 {
		t.Errorf("heartbeat settings = %+v, want off (no target) every 60s", hb)
	}

	if _, err := gw.HeartbeatTarget(ctx, "no-such-target"); !errors.Is(err, storage.ErrNotificationTargetNotFound) {
		t.Errorf("a missing heartbeat target = %v, want not found", err)
	}
	tgt, err := gw.CreateNotificationTarget(ctx, "", storage.NotificationTargetSpec{Name: "dead-mans-switch", Kind: storage.ChannelWebhook,
		URL: "https://monitor.example.com/ping"}, all, storage.SecretUse{}, storage.PeopleUse{})
	if err != nil {
		t.Fatal(err)
	}
	send, err := gw.HeartbeatTarget(ctx, "dead-mans-switch")
	if err != nil || send.TargetID != tgt.ID || send.URL != "https://monitor.example.com/ping" {
		t.Fatalf("heartbeat target = %+v, %v; want the created target's send", send, err)
	}

	if n, err := gw.PlatformAlarmCount(ctx); err != nil || n != 0 {
		t.Fatalf("open platform alarms on a quiet install = %d, %v; want 0", n, err)
	}
	a := serverComponent(t, ctx, gw, "hb-a")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: a.ID,
		Key: writerclass.RuleSweepAge, Value: 120, TS: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if err := gw.SweepEventRules(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := gw.PlatformAlarmCount(ctx); err != nil || n != 1 {
		t.Errorf("open platform alarms with a stalled sweep = %d, %v; want 1", n, err)
	}

	if err := gw.AuditHeartbeat(ctx, tgt.ID, false, 502, "the receiver answered 502"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where verb = 'heartbeat' and resource = 'notification_target' and resource_id = $1`, tgt.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("heartbeat audit rows = %d, %v; want 1", n, err)
	}
}
