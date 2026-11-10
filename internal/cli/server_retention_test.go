package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// bootServer runs runServer against dsn under the server identity name, on
// kernel-picked ports, until the test ends, and returns once its ready file
// lands, with the path of the file the server logs to. Cleanup cancels the
// boot's context and waits for runServer to return, so its pool is closed
// before the harness drops the database.
func bootServer(t *testing.T, dsn, name string) string {
	t.Helper()
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready.json")
	// The server logs to os.Stdout and makes that its default logger, which
	// the bus's retention loop logs through; the boot is pointed at a file so
	// a test can read what the server said.
	logPath := filepath.Join(dir, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("server log: %v", err)
	}
	stdout, defaultLog := os.Stdout, slog.Default()
	t.Cleanup(func() { slog.SetDefault(defaultLog); _ = logFile.Close() })
	os.Stdout = logFile
	defer func() { os.Stdout = stdout }()
	t.Setenv("OMNIGLASS_DSN", dsn)
	t.Setenv("OMNIGLASS_ADDR", "127.0.0.1:0")
	t.Setenv("OMNIGLASS_NATS_ADDR", "127.0.0.1:0")
	t.Setenv("OMNIGLASS_NATS_STORE_DIR", filepath.Join(dir, "nats"))
	t.Setenv("OMNIGLASS_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("OMNIGLASS_SETTINGS_FILE", filepath.Join(dir, "settings.yaml"))
	t.Setenv("OMNIGLASS_READY_FILE", ready)
	t.Setenv("OMNIGLASS_SERVER_NAME", name)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	var bootErr error
	go func() { bootErr = runServer(ctx, "test", false); close(stopped) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
			if bootErr != nil {
				t.Errorf("server stopped with %v", bootErr)
			}
		case <-time.After(30 * time.Second):
			t.Error("server did not stop when its context ended")
		}
	})
	waitUntil(t, "the server's ready file", 60*time.Second, func() bool {
		select {
		case <-stopped:
			t.Fatalf("server stopped during boot: %v", bootErr)
		default:
		}
		_, err := os.Stat(ready)
		return err == nil
	})
	return logPath
}

// logged reports whether the server's log at path carries msg.
func logged(t *testing.T, path, msg string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the server log: %v", err)
	}
	return strings.Contains(string(b), msg)
}

// waitUntil polls cond every 50 ms until it holds or within passes.
func waitUntil(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// retentionRows counts the observed samples of a retention figure on
// componentID, or on any component when componentID is empty.
func retentionRows(t *testing.T, ctx context.Context, conn *pgx.Conn, componentID, key string) int {
	t.Helper()
	var n int
	err := conn.QueryRow(ctx, `
		select count(*) from metric m
		join metric_series s on s.id = m.series_id
		join metric_type mt on mt.id = s.metric_type_id
		where mt.name = $1 and ($2 = '' or s.owner_id = nullif($2, '')::uuid)`, key, componentID).Scan(&n)
	if err != nil {
		t.Fatalf("count %s rows: %v", key, err)
	}
	return n
}

// plantOldLogLine lands a debug line two years old on a component of its
// own, past any debug window, and returns its id: a line only the retention
// pass's log prune removes, so its going is the sign a pass ran.
func plantOldLogLine(t *testing.T, ctx context.Context, dsn string, conn *pgx.Conn) int64 {
	t.Helper()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	defer gw.Close()
	all := scope.Set{All: true}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "retention-c1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	var id int64
	if err := conn.QueryRow(ctx, `insert into log_line (ts, component_id, instance, source, severity, message)
		values (now() - interval '2 years', $1::uuid, '', 'push', 'debug', 'past every window') returning id`,
		comp.ID).Scan(&id); err != nil {
		t.Fatalf("insert the old line: %v", err)
	}
	return id
}

func logLineExists(t *testing.T, ctx context.Context, conn *pgx.Conn, id int64) bool {
	t.Helper()
	var ok bool
	if err := conn.QueryRow(ctx, `select exists (select 1 from log_line where id = $1)`, id).Scan(&ok); err != nil {
		t.Fatalf("look up line %d: %v", id, err)
	}
	return ok
}

// TestABootedServerReportsItsRetentionPassOnItsOwnComponent is #1671's
// acceptance: a server booted through runServer lands its first retention
// pass's figures on the platform component its boot ensured, not only in a
// test that hands the bus a component id. Serial: it sets the process
// environment the server reads its configuration from.
func TestABootedServerReportsItsRetentionPassOnItsOwnComponent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	bootServer(t, dsn, "retention-reporter")

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var componentID string
	if err := conn.QueryRow(ctx, `select platform_component_id::text from platform_server where name = $1`,
		"retention-reporter").Scan(&componentID); err != nil {
		t.Fatalf("the booted server's platform component: %v", err)
	}
	waitUntil(t, "retention-pass-duration on the server's component", 30*time.Second, func() bool {
		return retentionRows(t, ctx, conn, componentID, writerclass.RetentionPassDuration) > 0
	})
}

// TestAServerWhoseComponentEnsureFailsStillPrunes is #1671's failure half: a
// boot whose platform component cannot be ensured (an identity of only
// spaces) still starts and still runs its retention pass, which prunes a log
// line planted past its window before the boot, and lands no retention figure
// anywhere. The figures would land after the pass's last lane, so the count
// waits for the line the loop logs once the pass and its record are done.
// Serial: it sets the process environment and the stdout the server reads.
func TestAServerWhoseComponentEnsureFailsStillPrunes(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	old := plantOldLogLine(t, ctx, dsn, conn)

	logPath := bootServer(t, dsn, "   ")

	waitUntil(t, "the retention pass to prune the old line", 30*time.Second, func() bool {
		return !logLineExists(t, ctx, conn, old)
	})
	waitUntil(t, "the retention pass to finish", 30*time.Second, func() bool {
		return logged(t, logPath, "rows pruned past their retention window")
	})
	var servers int
	if err := conn.QueryRow(ctx, `select count(*) from platform_server`).Scan(&servers); err != nil {
		t.Fatalf("count servers: %v", err)
	}
	if servers != 0 {
		t.Fatalf("a failed ensure recorded %d platform servers, want none", servers)
	}
	if n := retentionRows(t, ctx, conn, "", writerclass.RetentionPassDuration); n != 0 {
		t.Fatalf("a server with no platform component wrote %d retention-pass-duration rows, want none", n)
	}
}
