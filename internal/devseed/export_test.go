package devseed

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// ReplicaScenario is the large scenario at a chosen size, so a database test
// can prove the replica path without paying for the full fleet.
func ReplicaScenario(p ReplicaParams) Scenario {
	return Scenario{Name: "replica", expand: func(d Doc) Doc { return Replicate(d, p) }}
}

// SelfContainedRooms exposes the template choice to the shape tests.
func SelfContainedRooms(doc Doc) []string { return selfContainedRooms(doc) }

// The two fixtures nearly every database test here starts from, seeded once per
// test binary into a derived template and cloned per test (#1537). Before this
// each test paid for its own boot seed and devseed Run, which was the dominant
// cost of the package. A test that VERIFIES seeding (idempotence, an option's
// effect, the large scenario) still runs the seed itself, from the boot-seeded
// clone where it can.

// BootSeededDSN is a fresh database holding the boot seed only.
func BootSeededDSN(t testing.TB) string {
	t.Helper()
	return storagetest.NewDSNFrom(t, "boot_seed", func(ctx context.Context, dsn string) error {
		return seedInto(ctx, dsn, false)
	})
}

// FleetSeededDSN is a fresh database holding the boot seed and one devseed Run.
func FleetSeededDSN(t testing.TB) string {
	t.Helper()
	return storagetest.NewDSNFrom(t, "fleet_seed", func(ctx context.Context, dsn string) error {
		return seedInto(ctx, dsn, true)
	})
}

// SecretProvider is the static provider every gateway in these tests opens
// with, so a secret one clone wrote reads back through any other.
func SecretProvider() storage.Option {
	return storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32)))
}

func seedInto(ctx context.Context, dsn string, withFleet bool) error {
	gw, err := storage.NewPG(ctx, dsn, SecretProvider())
	if err != nil {
		return fmt.Errorf("open gateway: %w", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		return fmt.Errorf("boot seed: %w", err)
	}
	if withFleet {
		if err := Run(ctx, gw, ""); err != nil {
			return fmt.Errorf("devseed run: %w", err)
		}
	}
	return nil
}

// PlannedSender is one webhook sender a plan seeds (#1747).
type PlannedSender = plannedSender

// PlanSenders plans a scenario plan's webhook senders against the shipped
// simulator profiles and drivers, with no database.
func PlanSenders(p *scenario.Plan) ([]PlannedSender, error) { return shippedSenders(rowsOf(p)) }

// SlotLabels exposes the slot numbering to the label tests.
func SlotLabels(doc Doc, typeLabels map[string]string) map[string]string {
	return slotLabels(doc, typeLabels)
}

// ScopedLabel is one component of a plan as the console labels it, with the
// room, system and name it is found by (#2847). System is "" for a component
// that fills no slot.
type ScopedLabel struct{ Label, Room, System, Component string }

// PlannedComponentLabels is every component of a plan as the console labels it:
// the seed's own label where it gives one, else the component type's label bare,
// which is how the platform reads a slot of one.
func PlannedComponentLabels(p *scenario.Plan) []ScopedLabel {
	typeLabels := componentTypeLabelsByProduct()
	var out []ScopedLabel
	for _, c := range rowsOf(p).Components {
		l := c.label(typeLabels)
		if l == "" {
			l = typeLabels[c.Product]
		}
		if l != "" {
			out = append(out, ScopedLabel{Label: l, Room: c.Room, System: c.System, Component: c.Name})
		}
	}
	return out
}
