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

// A property and an event both survive their owner being renamed, and they do so
// for the strongest available reason: the arc references the owner's uuid, which
// a rename does not touch, so there is nothing to rewrite and no cascade to get
// wrong.
//
// Kept as a guard rather than a proof of new behaviour. If either arc were ever
// moved back onto the name, this is what would notice, either because the rename
// starts failing on a foreign key or because the rows quietly stop resolving.
func TestPropertiesAndEventsSurviveARename(t *testing.T) {
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

	std := "rename-standard"
	if err := gw.UpsertStandard(ctx, storage.Standard{ID: std, DisplayName: "Rename"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "old-sys", StandardID: &std}, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "old-comp", ProductName: &bar}, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	if _, err := gw.SetPropertyValue(ctx, "", "system", "old-sys", "model_number", "",
		[]byte(`"HR-2"`), all); err != nil {
		t.Fatalf("set property: %v", err)
	}
	if err := gw.InsertEvents(ctx, []storage.EventOccurrence{{
		OwnerKind: "component", OwnerID: "old-comp", Key: "syslog.line",
		Message: "link down", Source: "test",
	}}); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	newSys, newComp := "new-sys", "new-comp"
	if _, err := gw.UpdateSystem(ctx, "", "old-sys", storage.SystemPatch{Name: &newSys}, all, all); err != nil {
		t.Fatalf("rename system: %v", err)
	}
	if _, err := gw.UpdateComponent(ctx, "", "old-comp", storage.ComponentPatch{Name: &newComp}, all, all); err != nil {
		t.Fatalf("rename component: %v", err)
	}

	props, err := gw.EffectiveProperties(ctx, "system", newSys, all)
	if err != nil {
		t.Fatalf("effective properties: %v", err)
	}
	var found bool
	for _, p := range props {
		if p.PropertyName == "model_number" && p.IsSet {
			found = true
		}
	}
	if !found {
		t.Errorf("the property did not follow its system through a rename (got %+v)", props)
	}

	evs, err := gw.ListComponentEvents(ctx, newComp, time.Now().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(evs) != 1 {
		t.Errorf("events after the rename = %d, want 1: the arc points at the id, so it follows", len(evs))
	}
}
