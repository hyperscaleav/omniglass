package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A segment is the one token an entity contributes to every address that passes
// through it, and the rule for it (`^[a-z0-9][a-z0-9-]*$`, uuid refused) has only
// ever been enforced on component, system, and location. Every other
// key-bearing table took whatever it was handed.
//
// That gap is load-bearing, not cosmetic. The address grammar (#549) uses `$` as
// its accessor sigil precisely BECAUSE `$` cannot appear in a segment, so
// `boi.17c.sys.$sys.av` parses without ambiguity and no word needs reserving.
// That guarantee is worth exactly as much as the tables that enforce it: one
// unvalidated registry accepting `$` is one place the grammar becomes ambiguous.
//
// This test is the enforcement. Each case creates through the real gateway with a
// segment the rule forbids and requires a refusal.
// provedByCreate is the set of key-bearing tables whose refusal this suite actually
// drives through the gateway, keyed by table name. It is package level on purpose:
// TestEveryKeyBearingTableIsProved reads the same map, so the classification and the
// proof cannot drift apart. Adding a table to keyBearing without adding it here (or
// excusing it) fails the build.
//
// Each entry creates one entity with the given key and returns the error. A nil error
// means the table accepted an illegal key.
func provedByCreate(ctx context.Context, gw *storage.PG) map[string]func(key string) error {
	return map[string]func(key string) error{
		"component": func(s string) error {
			_, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: s}, all)
			return err
		},
		"system": func(s string) error {
			_, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s}, all)
			return err
		},
		"location": func(s string) error {
			_, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: s, LocationType: "room"}, all)
			return err
		},
		"node": func(s string) error {
			_, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: s}, all)
			return err
		},
		"location_type": func(s string) error {
			_, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: s, DisplayName: "X"})
			return err
		},
		"standard": func(s string) error {
			_, err := gw.CreateStandard(ctx, "", storage.Standard{Name: s, DisplayName: "X"})
			return err
		},
		"vendor": func(s string) error {
			_, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: s, DisplayName: "X", Kind: "manufacturer"})
			return err
		},
		"driver": func(s string) error {
			_, err := gw.CreateDriver(ctx, "", storage.Driver{Name: s, DisplayName: "X"})
			return err
		},
		"capability": func(s string) error {
			_, err := gw.CreateCapability(ctx, "", storage.Capability{Name: s, DisplayName: "X"})
			return err
		},
		"product": func(s string) error {
			_, err := gw.CreateProduct(ctx, "", storage.Product{Name: s, DisplayName: "X"})
			return err
		},
		// A role's key arrives as a bare {role} path param on PUT
		// /systems/{name}/roles/{role}, so an operator types it directly.
		"system_role": func(s string) error {
			_, err := gw.SetSystemRole(ctx, "", "system", "sysrole-host", storage.SystemRoleSpec{Name: s, Quorum: 1})
			return err
		},
		// A group name was validated only by the Huma pattern, on a looser rule that
		// admitted . and _, and the gateway accepted anything at all. Both are now the
		// entity rule, so the refusal is proved here like every other table's.
		"principal_group": func(s string) error {
			_, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: s}, all)
			return err
		},
	}
}

// provedByCreateTables is the same key set, without needing a live gateway, so the
// cross-check can run as a unit test.
func provedByCreateTables() map[string]bool {
	out := map[string]bool{}
	for k := range provedByCreate(context.Background(), nil) {
		out[k] = true
	}
	return out
}

func TestEveryKeyBearingTableValidates(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	creates := provedByCreate(ctx, gw)
	// Every one of these violates the key rule, and each fails for a reason a
	// caller might otherwise talk themselves into allowing.
	bad := map[string]string{
		"the accessor sigil": "bad$seg",
		"a dot, which would split one key into two path tokens":    "bad.seg",
		"an underscore, legal in a keyspace key but not a segment": "bad_seg",
		"uppercase":                  "BadSeg",
		"a leading hyphen":           "-badseg",
		"whitespace":                 "bad seg",
		"a NATS wildcard":            "bad*seg",
		"a NATS tail wildcard":       "bad>seg",
		"shaped exactly like a uuid": "019f8754-461f-7b82-b5f2-fc4bbe1c3765",
	}

	for table, create := range creates {
		for why, seg := range bad {
			t.Run(table+"/"+why, func(t *testing.T) {
				err := create(seg)
				if err == nil {
					t.Fatalf("%s accepted the key %q (%s); every key-bearing table "+
						"must refuse it, or the address grammar's `$` sigil is not safe", table, seg, why)
				}
				if !errors.Is(err, storage.ErrInvalidEntityKey) && !errors.Is(err, storage.ErrEntityKeyIsUUID) {
					t.Fatalf("%s refused %q with %v, want ErrInvalidEntityKey or ErrEntityKeyIsUUID so the "+
						"API maps it to 422 rather than a 500", table, seg, err)
				}
			})
		}
	}
}
