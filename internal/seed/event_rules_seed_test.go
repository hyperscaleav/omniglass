package seed_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeedShipsARuleDisabled boots the seed and reads each official event
// rule's state back (#1985): a rule the seed marks disabled lands off, the
// rest on, and db-size-growing, which a fleet onboarded in a database's first
// week alarms as bloat would, is one of the off ones. A second boot keeps it.
func TestSeedShipsARuleDisabled(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts struct {
		EventRules []struct {
			Name     string `json:"name"`
			Disabled bool   `json:"disabled"`
		} `json:"event_rules"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for boot := 1; boot <= 2; boot++ {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("boot %d: %v", boot, err)
		}
		off := map[string]bool{}
		for _, f := range facts.EventRules {
			r, err := gw.GetEventRule(ctx, f.Name, all)
			if err != nil {
				t.Fatalf("boot %d: %s: %v", boot, f.Name, err)
			}
			if r.Enabled == f.Disabled {
				t.Errorf("boot %d: %s enabled %v, want %v", boot, f.Name, r.Enabled, !f.Disabled)
			}
			off[f.Name] = !r.Enabled
		}
		if !off["db-size-growing"] {
			t.Errorf("boot %d: db-size-growing ships enabled, want it off", boot)
		}
	}
}
