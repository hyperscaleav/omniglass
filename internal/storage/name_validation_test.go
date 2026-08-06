package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
		// tag, variable, and secret were declared keyspace on the strength of the
		// word "key" in their prose. None of them carries a dot, which is the only
		// thing the keyspace rule adds, so all three are entity names. tag had its
		// own fourth validator; variable and secret had none at all.
		"tag": func(s string) error {
			_, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: s, AppliesTo: []string{"component"}}, all)
			return err
		},
		"variable": func(s string) error {
			_, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
				Name: s, ValueType: "string", OwnerKind: "platform", Value: json.RawMessage(`"x"`),
			}, all)
			return err
		},
		"secret": func(s string) error {
			_, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
				Name: s, SecretType: "password", OwnerKind: "platform",
			}, all, true)
			return err
		},
	}
}

// provedByCreateKeyspace is the same idea for the tables on the other rule. They
// were classified from the beginning and never proved, so for six tables the
// declaration was a claim: nothing drove a keyspace create through the gateway with
// an illegal name, and key.ValidateKey did not refuse a uuid at all.
//
// Only three tables are truly keyspace: the ones whose names are dot-joined paths.
// A keyspace name legally carries a dot, so the illegal set for them excludes the
// dot cases and adds the empty-segment ones only they can fail.
func provedByCreateKeyspace(ctx context.Context, gw *storage.PG) map[string]func(name string) error {
	return map[string]func(name string) error{
		"property_type": func(s string) error {
			_, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: s, DisplayName: "X", DataType: "string"})
			return err
		},
		"metric_type": func(s string) error {
			_, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: s, DisplayName: "X", DataType: "int"})
			return err
		},
		"event_type": func(s string) error {
			_, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{Name: s, DisplayName: "X"})
			return err
		},
		"command_type": func(s string) error {
			_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: s, DisplayName: "X"})
			return err
		},
	}
}

// provedByCreateTables is the same key set, without needing a live gateway, so the
// cross-check can run as a unit test. It covers both rules, because the guard's
// question is "is this table's refusal proved", not "on which rule".
func provedByCreateTables() map[string]bool {
	out := map[string]bool{}
	for k := range provedByCreate(context.Background(), nil) {
		out[k] = true
	}
	for k := range provedByCreateKeyspace(context.Background(), nil) {
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
				if !errors.Is(err, storage.ErrInvalidEntityName) && !errors.Is(err, storage.ErrEntityNameIsUUID) {
					t.Fatalf("%s refused %q with %v, want ErrInvalidEntityName or ErrEntityNameIsUUID so the "+
						"API maps it to 422 rather than a 500", table, seg, err)
				}
			})
		}
	}
}

// TestEveryKeyspaceTableValidates is the same proof for the other rule. A keyspace
// name is a dot-joined path, so the dot itself is legal here and the illegal set is
// about the segments and the whole.
func TestEveryKeyspaceTableValidates(t *testing.T) {
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

	bad := map[string]string{
		"the accessor sigil":                     "bad$name",
		"an underscore":                          "bad_name",
		"uppercase":                              "BadName",
		"a leading hyphen":                       "-badname",
		"whitespace":                             "bad name",
		"a NATS wildcard":                        "bad*name",
		"a NATS tail wildcard":                   "bad>name",
		"a leading dot, an empty first segment":  ".badname",
		"a trailing dot, an empty last segment":  "badname.",
		"a doubled dot, an empty middle segment": "bad..name",
		"shaped exactly like a uuid":             "019f8754-461f-7b82-b5f2-fc4bbe1c3765",
	}

	for table, create := range provedByCreateKeyspace(ctx, gw) {
		for why, name := range bad {
			t.Run(table+"/"+why, func(t *testing.T) {
				err := create(name)
				if err == nil {
					t.Fatalf("%s accepted the name %q (%s)", table, name, why)
				}
				// The keyspace tables each wrap the rule error in their own sentinel
				// (ErrPropertyTypeInvalid and siblings) so their handlers keep mapping
				// to 422, so unwrapping to the rule sentinel is what matters here.
				if !errors.Is(err, storage.ErrInvalidEntityName) && !errors.Is(err, storage.ErrEntityNameIsUUID) {
					t.Fatalf("%s refused %q with %v, want the error to wrap ErrInvalidEntityName or "+
						"ErrEntityNameIsUUID so a handler cannot map it to a 500", table, name, err)
				}
			})
		}
		// A dot is illegal on every table now (#586), so the keyspace tables refuse
		// it through the gateway exactly as the entity tables always did.
		t.Run(table+"/refuses a dotted name", func(t *testing.T) {
			err := create(strings.ReplaceAll(table, "_", "-") + ".legal-name")
			if err == nil {
				t.Fatalf("%s accepted a dotted name; there is one name rule and it has no dots", table)
			}
			if !errors.Is(err, storage.ErrInvalidEntityName) {
				t.Fatalf("%s refused a dotted name with %v, want it to wrap ErrInvalidEntityName", table, err)
			}
		})
		// The positive case, so the refusals are not passing because the create is
		// simply broken.
		t.Run(table+"/accepts a kebab name", func(t *testing.T) {
			if err := create(strings.ReplaceAll(table, "_", "-") + "-legal-name"); err != nil {
				t.Fatalf("%s refused a legal kebab name: %v", table, err)
			}
		})
	}
}
