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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	now := time.Now().UTC()
	err = gw.InsertEvents(ctx, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "call-started", Message: "call started", Source: "xapi", TS: now.Add(-2 * time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "call-started", Message: "call joined by 4 participants", Attributes: []byte(`{"peer":"room-204","protocol":"sip"}`), Source: "xapi", TS: now},
	})
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	got, err := gw.ListComponentEvents(ctx, "disp-1", time.Hour, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list events: want 2, got %d", len(got))
	}
	// Newest first: the "call joined by 4 participants" occurrence with its json attributes, defaulted to
	// the caught origin (a component-observed occurrence).
	if got[0].Message != "call joined by 4 participants" || got[0].Provenance != "observed" || got[0].Origin != "caught" || string(got[0].Attributes) != `{"peer": "room-204", "protocol": "sip"}` {
		t.Fatalf("newest event: want 'call joined by 4 participants' observed/caught with attributes, got %+v", got[0])
	}
	if got[1].Message != "call started" {
		t.Fatalf("oldest event: want 'call started', got %q", got[1].Message)
	}

	// The since window excludes the older occurrence.
	windowed, err := gw.ListComponentEvents(ctx, "disp-1", time.Minute, 10)
	if err != nil {
		t.Fatalf("list windowed events: %v", err)
	}
	if len(windowed) != 1 || windowed[0].Message != "call joined by 4 participants" {
		t.Fatalf("windowed events: want only 'call joined by 4 participants', got %+v", windowed)
	}

	// An owner component that does not exist violates the component FK.
	err = gw.InsertEvents(ctx, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "ghost", Key: "call-started", Message: "x", TS: now},
	})
	if err == nil {
		t.Fatal("insert with unknown owner: want error, got nil")
	}
}
