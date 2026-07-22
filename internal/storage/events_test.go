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

// TestInsertEvents proves an observed, component-owned log occurrence is written
// and read back newest-first with its message and structured attributes, that the
// owner-arc FK rejects an occurrence whose owner component does not exist, and that
// the since/limit window bounds the read.
func TestInsertEvents(t *testing.T) {
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
	err = gw.InsertEvents(ctx, []storage.EventOccurrence{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "syslog.line", Instance: "disp-1-ssh", Message: "link down", Source: "ssh", TS: now.Add(-2 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "syslog.line", Instance: "disp-1-ssh", Message: "link up", Attributes: []byte(`{"iface":"eth0"}`), Source: "ssh", TS: now},
	})
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	got, err := gw.ListComponentEvents(ctx, "disp-1", now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list events: want 2, got %d", len(got))
	}
	// Newest first: the "link up" occurrence with its json attributes.
	if got[0].Message != "link up" || got[0].Provenance != "observed" || string(got[0].Attributes) != `{"iface": "eth0"}` {
		t.Fatalf("newest event: want 'link up' observed with attributes, got %+v", got[0])
	}
	if got[1].Message != "link down" {
		t.Fatalf("oldest event: want 'link down', got %q", got[1].Message)
	}

	// The since window excludes the older occurrence.
	windowed, err := gw.ListComponentEvents(ctx, "disp-1", now.Add(-time.Minute), 10)
	if err != nil {
		t.Fatalf("list windowed events: %v", err)
	}
	if len(windowed) != 1 || windowed[0].Message != "link up" {
		t.Fatalf("windowed events: want only 'link up', got %+v", windowed)
	}

	// An owner component that does not exist violates the component FK.
	err = gw.InsertEvents(ctx, []storage.EventOccurrence{
		{OwnerKind: "component", OwnerID: "ghost", Key: "syslog.line", Message: "x", TS: now},
	})
	if err == nil {
		t.Fatal("insert with unknown owner: want error, got nil")
	}
}
