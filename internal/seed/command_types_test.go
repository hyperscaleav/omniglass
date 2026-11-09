package seed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeedCommandTypesNameNoPowerOutlet pins #1648's seed side: the
// power-command marker is a column every shipped command type reads, and no
// shipped type sets it until an outlet command ships (#1653), so a seeded
// reboot still holds its own target and nothing an outlet feeds.
func TestSeedCommandTypesNameNoPowerOutlet(t *testing.T) {
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
	types, err := gw.ListCommandTypes(ctx)
	if err != nil {
		t.Fatalf("list command types: %v", err)
	}
	if len(types) == 0 {
		t.Fatal("the seed shipped no command types")
	}
	for _, ct := range types {
		if ct.PowerOutletParam != "" {
			t.Errorf("seeded command type %q names power_outlet_param %q, want none until #1653", ct.Name, ct.PowerOutletParam)
		}
	}
}
