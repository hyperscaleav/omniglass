package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// audit_log.resource_id must be the entity's primary key, never the reference the
// caller happened to use.
//
// A named entity is addressable two ways (ADR-0062: the uuid or the renameable
// handle), and several gateway functions passed the caller's ref straight through to
// the audit row. So the same entity acquired two different audit keys depending on
// how each request addressed it, and a rename orphaned every row keyed on the old
// name. That defeats the one thing an audit log exists to do, which is survive the
// entity changing.
//
// The name at the time of the action is not lost: it is in the old and new row
// images, which is the right place for it. A point-in-time snapshot, not a lookup
// key. This mirrors actor_username, which is snapshotted beside actor_principal_id
// for exactly this reason.

// auditedRename is one renameable, dual-addressable resource: how to create it, how
// to update it by an arbitrary ref, and how to rename it.
type auditedRename struct {
	resource string
	create   func(ctx context.Context, gw *storage.PG, name string) (id string, err error)
	update   func(ctx context.Context, gw *storage.PG, ref string) error
	rename   func(ctx context.Context, gw *storage.PG, ref, to string) error
}

func auditedRenames() []auditedRename {
	return []auditedRename{
		{
			resource: "driver",
			create: func(ctx context.Context, gw *storage.PG, name string) (string, error) {
				d, err := gw.CreateDriver(ctx, "", storage.Driver{Name: name, DisplayName: "X"})
				if err != nil {
					return "", err
				}
				return d.ID, nil
			},
			update: func(ctx context.Context, gw *storage.PG, ref string) error {
				n := "Y"
				_, err := gw.UpdateDriver(ctx, "", ref, storage.DriverPatch{DisplayName: &n})
				return err
			},
		},
		{
			resource: "vendor",
			create: func(ctx context.Context, gw *storage.PG, name string) (string, error) {
				v, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: name, DisplayName: "X", Kind: "manufacturer"})
				if err != nil {
					return "", err
				}
				return v.ID, nil
			},
			update: func(ctx context.Context, gw *storage.PG, ref string) error {
				d := "Y"
				_, err := gw.UpdateVendor(ctx, "", ref, storage.VendorPatch{DisplayName: &d})
				return err
			},
		},
		{
			resource: "product",
			create: func(ctx context.Context, gw *storage.PG, name string) (string, error) {
				m, err := gw.CreateProduct(ctx, "", storage.Product{Name: name, DisplayName: "X"})
				if err != nil {
					return "", err
				}
				return m.ID, nil
			},
			update: func(ctx context.Context, gw *storage.PG, ref string) error {
				d := "Y"
				_, err := gw.UpdateProduct(ctx, "", ref, storage.ProductPatch{DisplayName: &d})
				return err
			},
		},
		{
			resource: "node",
			create: func(ctx context.Context, gw *storage.PG, name string) (string, error) {
				n, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all)
				if err != nil {
					return "", err
				}
				// A node IS a principal: principal_id is the node table's primary key.
				return n.PrincipalID, nil
			},
		},
	}
}

// A property type is addressed only by name today, so it never acquires two keys
// for two addressings. The rename hazard is the whole risk here: the name IS the
// address, which is exactly why the trail cannot also be keyed on it.
func TestAuditResourceIDOnANameAddressedRegistry(t *testing.T) {
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

	const name = "audit-key-property"
	created, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
		Name: name, DisplayName: "X", DataType: "string"})
	if err != nil {
		t.Fatalf("create property type: %v", err)
	}
	display := "Y"
	if _, err := gw.UpdatePropertyType(ctx, "", name, storage.PropertyTypePatch{DisplayName: &display}); err != nil {
		t.Fatalf("update property type: %v", err)
	}
	if err := gw.DeletePropertyType(ctx, "", name); err != nil {
		t.Fatalf("delete property type: %v", err)
	}

	rows, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "property_type", Limit: 50})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var seen int
	for _, r := range rows {
		if r.ResourceID == name {
			t.Errorf("a property_type audit row keys on the NAME %q; a rename orphans it", name)
		}
		if r.ResourceID == created.ID {
			seen++
		}
	}
	if seen != 3 {
		t.Errorf("%d property_type audit rows key on the uuid %q, want 3 (create, update, delete)",
			seen, created.ID)
	}
}

// A membership is a join row, and the entity the trail is about is the system it
// binds into, so that system's uuid is the key. The system name is a snapshot in
// the row image, where a rename cannot break it.
func TestAuditResourceIDOnAMembershipJoinRow(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	if err := f.gw.AddMember(ctx, "", "room-a", "dsp", f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := f.gw.SetPrimaryMember(ctx, "", "room-a", "dsp", f.all); err != nil {
		t.Fatalf("set primary member: %v", err)
	}
	if err := f.gw.RemoveMember(ctx, "", "room-a", "dsp", f.all); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	sys, err := f.gw.GetSystem(ctx, "room-a", f.all)
	if err != nil {
		t.Fatalf("get system: %v", err)
	}

	rows, err := f.gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "system_member", Limit: 50})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var seen int
	for _, r := range rows {
		if r.ResourceID == "room-a" {
			t.Error("a system_member audit row keys on the system NAME; a rename orphans it")
		}
		if r.ResourceID == sys.ID {
			seen++
		}
	}
	if seen != 3 {
		t.Errorf("%d system_member audit rows key on the system uuid %q, want 3 (add, set primary, remove)",
			seen, sys.ID)
	}
}

func TestAuditResourceIDIsAlwaysThePrimaryKey(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, c := range auditedRenames() {
		t.Run(c.resource, func(t *testing.T) {
			name := "audit-key-" + c.resource
			id, err := c.create(ctx, gw, name)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if id == "" {
				t.Fatal("create returned no id")
			}

			// Address the same entity both legal ways. Whichever the caller picks,
			// the audit row must key on the entity, not on the request.
			if c.update != nil {
				if err := c.update(ctx, gw, name); err != nil {
					t.Fatalf("update by name: %v", err)
				}
				if err := c.update(ctx, gw, id); err != nil {
					t.Fatalf("update by uuid: %v", err)
				}
			}

			rows, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: c.resource, Limit: 50})
			if err != nil {
				t.Fatalf("list audit: %v", err)
			}
			var seen int
			for _, r := range rows {
				if r.ResourceID == name {
					t.Errorf("an audit row for %s keys on the NAME %q; a rename orphans it",
						c.resource, name)
				}
				if r.ResourceID == id {
					seen++
				}
			}
			want := 1
			if c.update != nil {
				want = 3 // create, update-by-name, update-by-uuid
			}
			if seen != want {
				t.Errorf("%d audit rows key on the entity uuid, want %d: addressing the same "+
					"entity two ways produced two different audit keys", seen, want)
			}
		})
	}
}
