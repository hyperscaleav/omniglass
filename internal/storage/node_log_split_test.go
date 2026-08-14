package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The #589 split. A log line comes off a component, so log_line loses the
// exclusive arc (owner_kind and the system/location/node arms) and component_id
// becomes NOT NULL. A node's self-logs (ADR-0066) are not orphaned by the
// narrowing: they move to node_log, an origin-true table where every row is
// node-owned and no arc is needed. These tests pin the migrated shape; the
// behavior over it lives in logs_test.go.

// TestLogLineIsComponentOnly asserts the narrowed log_line: the three foreign
// arms and the arc machinery are gone, and the surviving owner column is
// mandatory. A single-owner table needs neither an owner_kind discriminator nor
// an arc CHECK.
func TestLogLineIsComponentOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	for _, col := range []string{"owner_kind", "system_id", "location_id", "node_id"} {
		if logColumnExists(t, ctx, conn, "log_line", col) {
			t.Errorf("log_line.%s still present; #589 narrowed log_line to component-only", col)
		}
	}
	var nullable string
	err = conn.QueryRow(ctx, `select is_nullable from information_schema.columns
		where table_name = 'log_line' and column_name = 'component_id'`).Scan(&nullable)
	if err != nil {
		t.Fatalf("probe log_line.component_id: %v", err)
	}
	if nullable != "NO" {
		t.Error("log_line.component_id is nullable; the single owner of a component-only table is mandatory")
	}
	for _, con := range []string{"log_line_owner_arc_check", "log_line_owner_kind_check"} {
		if constraintExists(t, conn, con) {
			t.Errorf("%s still present; a single-owner table needs no arc CHECK", con)
		}
	}
}

// TestNodeLogShape asserts the self-log home the split creates: node_log keyed
// to node(principal_id) with the same cascade the old node arm had, and the
// (node_id, ts desc) index the newest-first list read leans on.
func TestNodeLogShape(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var exists bool
	if err := conn.QueryRow(ctx,
		`select to_regclass('node_log') is not null`).Scan(&exists); err != nil {
		t.Fatalf("probe node_log: %v", err)
	}
	if !exists {
		t.Fatal("node_log missing; #589 moves node self-logs into their own table")
	}

	for col, wantNullable := range map[string]string{
		"node_id": "NO", "message": "NO", "ts": "NO",
		"severity": "YES", "facility": "YES", "correlation_id": "YES",
	} {
		var nullable string
		err := conn.QueryRow(ctx, `select is_nullable from information_schema.columns
			where table_name = 'node_log' and column_name = $1`, col).Scan(&nullable)
		if err != nil {
			t.Fatalf("probe node_log.%s: %v", col, err)
		}
		if nullable != wantNullable {
			t.Errorf("node_log.%s nullable = %s, want %s", col, nullable, wantNullable)
		}
	}

	// The FK carries the node arm's target and cascade over unchanged: a
	// deleted node takes its self-logs with it, exactly as the arc did.
	var confdel string
	err = conn.QueryRow(ctx, `select confdeltype::text from pg_constraint
		where conname = 'node_log_node_id_fkey' and confrelid = 'node'::regclass`).Scan(&confdel)
	if err != nil {
		t.Fatalf("probe node_log_node_id_fkey (want an FK to node): %v", err)
	}
	if confdel != "c" {
		t.Errorf("node_log_node_id_fkey delete rule = %q, want cascade", confdel)
	}

	if !indexExists(t, conn, "node_log_node_ts_idx") {
		t.Error("node_log_node_ts_idx missing; the list read is newest-first per node")
	}
}

// TestInsertLogLinesRefusesANonComponentBinding pins the write-side fence of the
// narrowing: the storage layer refuses a non-component binding loudly
// (reject-not-project, the house ingest posture) instead of projecting it onto
// the component arm or letting a NULL trip a constraint opaquely. The node here
// exists, so before the split this insert succeeded; only the narrowing turns
// it into a named refusal.
func TestInsertLogLinesRefusesANonComponentBinding(t *testing.T) {
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-9"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}

	err = gw.InsertLogLines(ctx, []storage.LogLineWrite{
		{OwnerKind: "node", OwnerID: "edge-9", Message: "self-log on the wrong lane", TS: time.Now().UTC()},
	})
	if err == nil {
		t.Fatal("insert with a node binding: want a refusal, got nil (node self-logs belong in node_log)")
	}
	if !strings.Contains(err.Error(), "component") {
		t.Fatalf("refusal should name the component-only contract, got: %v", err)
	}
}

// logColumnExists probes information_schema for a column, the shape check the
// narrowing assertions above are built on.
func logColumnExists(t *testing.T, ctx context.Context, conn *pgx.Conn, table, col string) bool {
	t.Helper()
	var exists bool
	err := conn.QueryRow(ctx, `select exists (select 1 from information_schema.columns
		where table_name = $1 and column_name = $2)`, table, col).Scan(&exists)
	if err != nil {
		t.Fatalf("probe %s.%s: %v", table, col, err)
	}
	return exists
}
