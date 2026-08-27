package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The endpoint rename (#811, ADR-0073 and the naming ruling on #603): the
// interface table becomes endpoint, its type FK becomes a transport name the
// Go registry validates, the interface_type table retires, and the rename
// carries everything that spells the old noun: the task arc's column, the
// role permission strings, and the canonical interface-reachable datapoint.
//
// Following product_type_backfill_test.go: the test stands the database just
// below this migration, plants pre-rename rows, then drives the migration's
// own embedded SQL, so what runs is what ships.

const endpointRenameVersion = "20260828000000"

func endpointRenameUpSQL(t *testing.T) string {
	t.Helper()
	raw, err := db.FS.ReadFile("migrations/" + endpointRenameVersion + "_endpoint_rename.sql")
	if err != nil {
		t.Fatalf("read endpoint rename migration: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatalf("endpoint rename migration missing '-- migrate:down' marker")
	}
	return strings.TrimPrefix(up, "-- migrate:up")
}

func TestEndpointRenameCarriesTheData(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, endpointRenameVersion); err != nil {
		t.Fatalf("roll back below the endpoint rename: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)

	// Pre-rename fixtures: a transport row the old FK points at, a
	// server-hosted interface (a nil component keeps the fixture chain flat;
	// ownership is not what this migration moves) with its derived task, a
	// custom role granting interface nouns, and the canonical reachability
	// datapoint whose samples reference it by id.
	var typeID, ifaceID string
	if err := conn.QueryRow(ctx,
		`insert into interface_type (name, official, description, built) values ('tcp', true, 'raw tcp', true) returning id`).Scan(&typeID); err != nil {
		t.Fatalf("plant interface_type: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into interface (name, type, params) values ('tcp', $1, '{"target":"10.0.0.9"}') returning id`,
		typeID).Scan(&ifaceID); err != nil {
		t.Fatalf("plant interface: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into task (id, mode, interface_id, spec, enabled) values ('ren-task', 'poll', $1, '{}', true)`, ifaceID); err != nil {
		t.Fatalf("plant task: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into role (name, permissions) values ('ren-role', array['interface:read','interface:create,update','component:read'])`); err != nil {
		t.Fatalf("plant role: %v", err)
	}
	var ptID string
	if err := conn.QueryRow(ctx,
		`insert into property_type (name, data_type, official) values ('interface-reachable', 'string', true)
		 on conflict (name) do update set official = excluded.official returning id`).Scan(&ptID); err != nil {
		t.Fatalf("plant property_type: %v", err)
	}

	up := endpointRenameUpSQL(t)
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("apply endpoint rename: %v", err)
	}

	// The table renamed and the transport backfilled from the retired type row.
	var transport, name string
	if err := conn.QueryRow(ctx,
		`select transport, name from endpoint where id = $1`, ifaceID).Scan(&transport, &name); err != nil {
		t.Fatalf("read renamed endpoint: %v", err)
	}
	if transport != "tcp" || name != "tcp" {
		t.Fatalf("endpoint carries transport %q name %q, want tcp/tcp", transport, name)
	}

	// The interface_type table and the type column are gone.
	var n int
	if err := conn.QueryRow(ctx,
		`select count(*) from information_schema.tables where table_schema='public' and table_name='interface_type'`).Scan(&n); err != nil {
		t.Fatalf("check interface_type gone: %v", err)
	}
	if n != 0 {
		t.Fatal("interface_type table survived the rename")
	}
	if err := conn.QueryRow(ctx,
		`select count(*) from information_schema.columns where table_name='endpoint' and column_name='type'`).Scan(&n); err != nil {
		t.Fatalf("check type column gone: %v", err)
	}
	if n != 0 {
		t.Fatal("the type column survived on endpoint")
	}

	// The task arc renamed with its endpoint.
	var taskEndpoint string
	if err := conn.QueryRow(ctx,
		`select endpoint_id from task where id = 'ren-task'`).Scan(&taskEndpoint); err != nil {
		t.Fatalf("read task.endpoint_id: %v", err)
	}
	if taskEndpoint != ifaceID {
		t.Fatalf("task.endpoint_id = %q, want %q", taskEndpoint, ifaceID)
	}

	// The role's permission nouns rewrote, entry order and the untouched noun preserved.
	var perms []string
	if err := conn.QueryRow(ctx,
		`select permissions from role where name = 'ren-role'`).Scan(&perms); err != nil {
		t.Fatalf("read role permissions: %v", err)
	}
	want := []string{"endpoint:read", "endpoint:create,update", "component:read"}
	if len(perms) != len(want) {
		t.Fatalf("permissions = %v, want %v", perms, want)
	}
	for i := range want {
		if perms[i] != want[i] {
			t.Fatalf("permissions = %v, want %v", perms, want)
		}
	}

	// The canonical datapoint renamed in place: same row id, new name, so
	// every uuid-keyed sample keeps its history.
	var ptName string
	if err := conn.QueryRow(ctx,
		`select name from property_type where id = $1`, ptID).Scan(&ptName); err != nil {
		t.Fatalf("read renamed property_type: %v", err)
	}
	if ptName != "endpoint-reachable" {
		t.Fatalf("property_type renamed to %q, want endpoint-reachable", ptName)
	}

	// Idempotence: the same SQL runs again and changes nothing.
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply endpoint rename: %v", err)
	}
	if err := conn.QueryRow(ctx, `select permissions from role where name = 'ren-role'`).Scan(&perms); err != nil {
		t.Fatalf("re-read role permissions: %v", err)
	}
	if perms[0] != "endpoint:read" {
		t.Fatalf("second pass disturbed permissions: %v", perms)
	}
}
