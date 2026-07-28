package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestInsertLogLines round-trips the raw-log lane (ADR-0066): log lines are
// untyped arrival owned through the exclusive arc, read back newest-first, with
// severity/facility (the retention/routing axes) and the freeform attributes and
// labels preserved. A line whose owner does not exist violates the component FK.
func TestInsertLogLines(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	now := time.Now().UTC()
	err = gw.InsertLogLines(ctx, []storage.LogLineWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Source: "syslog", Severity: "info", Facility: "kern", Message: "link state changed to up", TS: now.Add(-2 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Source: "syslog", Severity: "err", Facility: "daemon", Message: "codec unreachable", Attributes: []byte(`{"code":504}`), Labels: []byte(`{"class":"firmware"}`), CorrelationID: "sess-9", TS: now},
	})
	if err != nil {
		t.Fatalf("insert log lines: %v", err)
	}

	got, err := gw.ListComponentLogs(ctx, "disp-1", now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list logs: want 2, got %d", len(got))
	}
	// Newest first: the err/daemon line with its structured attributes and labels.
	n := got[0]
	if n.Message != "codec unreachable" || n.Severity != "err" || n.Facility != "daemon" ||
		n.Source != "syslog" || n.CorrelationID != "sess-9" ||
		string(n.Attributes) != `{"code": 504}` || string(n.Labels) != `{"class": "firmware"}` {
		t.Fatalf("newest log: unexpected row %+v", n)
	}
	if got[1].Message != "link state changed to up" || got[1].Severity != "info" {
		t.Fatalf("oldest log: want info 'link state changed to up', got %+v", got[1])
	}

	// The since window excludes the older line.
	windowed, err := gw.ListComponentLogs(ctx, "disp-1", now.Add(-time.Minute), 10)
	if err != nil {
		t.Fatalf("list windowed logs: %v", err)
	}
	if len(windowed) != 1 || windowed[0].Message != "codec unreachable" {
		t.Fatalf("windowed logs: want only 'codec unreachable', got %+v", windowed)
	}

	// An owner component that does not exist violates the component FK.
	if err := gw.InsertLogLines(ctx, []storage.LogLineWrite{
		{OwnerKind: "component", OwnerID: "ghost", Message: "x", TS: now},
	}); err == nil {
		t.Fatal("insert with unknown owner: want error, got nil")
	}
}
