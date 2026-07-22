package storage_test

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// backfillSQL is the shipped migration's up section, read from the file rather
// than copied into the test. A backfill runs exactly once on a real database and
// never again, so a copy here would drift silently and the test would go on
// passing against SQL nobody ships.
func backfillSQL(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../db/migrations/20260722120100_system_member_backfill.sql")
	if err != nil {
		t.Fatalf("read backfill migration: %v", err)
	}
	body := string(raw)
	up := body[strings.Index(body, "-- migrate:up")+len("-- migrate:up"):]
	if i := strings.Index(up, "-- migrate:down"); i >= 0 {
		up = up[:i]
	}
	if strings.TrimSpace(up) == "" {
		t.Fatal("backfill migration has an empty up section")
	}
	return up
}

// The backfill has to reconstruct membership from the two places it was implied
// before the table existed, and the halves cover different components: the role
// table knows the shared device and every staffed component, the old pointer
// knows the ones that belonged to a system without filling any declared role.
// Dropping either half loses real estate data, so both are asserted here.
func TestSystemMemberBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	all := scope.Set{All: true}

	std := "backfill-standard"
	if err := gw.UpsertStandard(ctx, storage.Standard{ID: std, DisplayName: "Backfill"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	for _, s := range []string{"bf-a", "bf-b"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s, StandardID: &std}, all); err != nil {
			t.Fatalf("system %s: %v", s, err)
		}
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", std, storage.SystemRoleSpec{
		Name: "mic", DisplayName: "Mic", Quorum: 1,
		Capabilities: []string{"microphone"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("role: %v", err)
	}

	bar, sysA := "cisco-room-bar", "bf-a"
	// shared: staffs a role in both systems, no pointer. Only the role half finds it.
	// pointed: a pointer and no role at all. Only the pointer half finds it, and it
	// is the case that proves the backfill cannot be built from role_assignment alone.
	// both: a pointer to bf-a and a role in bf-b, so it ends up in two systems with
	// the pointer deciding which is the default.
	for _, c := range []struct {
		name   string
		system *string
	}{
		{"bf-shared", nil},
		{"bf-pointed", &sysA},
		{"bf-both", &sysA},
	} {
		product := bar
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
			Name: c.name, ProductName: &product, SystemName: c.system}, all); err != nil {
			t.Fatalf("component %s: %v", c.name, err)
		}
	}
	for _, a := range []struct{ system, component string }{
		{"bf-a", "bf-shared"}, {"bf-b", "bf-shared"}, {"bf-b", "bf-both"},
	} {
		if err := gw.AssignRole(ctx, "", a.system, "mic", a.component, all); err != nil {
			t.Fatalf("assign %s to %s: %v", a.component, a.system, err)
		}
	}

	// Reconstruct the pre-migration world. component.system_id has since been
	// dropped (20260722130000), so the column is put back and repopulated from the
	// memberships the fixture created through it: the backfill is a historical
	// migration and must still be tested against the schema it actually ran on,
	// not against today's.
	if _, err := conn.Exec(ctx, `alter table component add column system_id uuid references system (id)`); err != nil {
		t.Fatalf("restore the dropped column: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		update component c set system_id = s.id
		from system_member m join system s on s.name = m.system_id
		where m.component_id = c.name and m.is_primary and c.name in ('bf-pointed', 'bf-both')`); err != nil {
		t.Fatalf("repopulate the pointer: %v", err)
	}
	if _, err := conn.Exec(ctx, `delete from system_member`); err != nil {
		t.Fatalf("clear memberships: %v", err)
	}
	t.Cleanup(func() { _, _ = conn.Exec(ctx, `alter table component drop column if exists system_id`) })

	if _, err := conn.Exec(ctx, backfillSQL(t)); err != nil {
		t.Fatalf("run backfill: %v", err)
	}

	got := map[string][]string{}
	primary := map[string]string{}
	rows, err := conn.Query(ctx, `
		select component_id, system_id, is_primary from system_member
		where component_id like 'bf-%' order by component_id, system_id`)
	if err != nil {
		t.Fatalf("read memberships: %v", err)
	}
	for rows.Next() {
		var c, s string
		var isPrimary bool
		if err := rows.Scan(&c, &s, &isPrimary); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		got[c] = append(got[c], s)
		if isPrimary {
			if prev, dup := primary[c]; dup {
				t.Errorf("%s has two primaries (%s and %s)", c, prev, s)
			}
			primary[c] = s
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}

	want := map[string][]string{
		"bf-shared":  {"bf-a", "bf-b"}, // both from the role half
		"bf-pointed": {"bf-a"},         // only the pointer half knows this one
		"bf-both":    {"bf-a", "bf-b"}, // pointer gave one, role gave the other
	}
	for c, w := range want {
		sort.Strings(got[c])
		if strings.Join(got[c], ",") != strings.Join(w, ",") {
			t.Errorf("%s memberships = %v, want %v", c, got[c], w)
		}
	}

	// The old pointer answered "which system chain feeds this component's config",
	// so it is exactly the right seed for the default.
	if primary["bf-pointed"] != "bf-a" {
		t.Errorf("bf-pointed primary = %q, want bf-a (its old pointer)", primary["bf-pointed"])
	}
	if primary["bf-both"] != "bf-a" {
		t.Errorf("bf-both primary = %q, want bf-a (the pointer wins over the role)", primary["bf-both"])
	}
	// Two memberships and no pointer: there is no honest way to guess which one an
	// operator meant, so the backfill picks neither rather than inventing an answer.
	if p, ok := primary["bf-shared"]; ok {
		t.Errorf("bf-shared primary = %q, want none: a component in two systems with no "+
			"pointer has no defensible default", p)
	}

	// Running it twice must change nothing: a backfill that is not idempotent is one
	// bad retry away from duplicate membership.
	before := len(got["bf-shared"]) + len(got["bf-pointed"]) + len(got["bf-both"])
	if _, err := conn.Exec(ctx, backfillSQL(t)); err != nil {
		t.Fatalf("rerun backfill: %v", err)
	}
	var after int
	if err := conn.QueryRow(ctx, `select count(*) from system_member where component_id like 'bf-%'`).
		Scan(&after); err != nil {
		t.Fatalf("recount: %v", err)
	}
	if after != before {
		t.Errorf("membership count %d -> %d on a second run, want it unchanged", before, after)
	}
}
