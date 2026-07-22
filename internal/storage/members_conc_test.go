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

// Whether a new membership becomes the component's default is decided by reading
// its other memberships, which is compare-then-act: under READ COMMITTED a second
// transaction takes its snapshot before the first commits, so both see no
// membership and both claim the default. The partial unique index then refuses the
// loser, turning an ordinary write (two rooms wired at once) into an error.
//
// Racing two goroutines does NOT reliably reproduce this. The transactions are
// short and rarely overlap in the window that matters, so such a test passes just
// as happily with the serialization removed, which was measured rather than
// assumed: six runs, zero failures. This drives the two transactions by hand
// instead, holding both open across the decision point, which is the only way to
// land in the window every run.
func TestFirstMembershipRaceIsSerialized(t *testing.T) {
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
	all := scope.Set{All: true}

	std := "race-standard"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, DisplayName: "Race"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	for _, s := range []string{"race-a", "race-b"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s, StandardID: &std}, all); err != nil {
			t.Fatalf("system %s: %v", s, err)
		}
	}
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "race-dsp", ProductName: &bar}, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	open := func() *pgx.Conn {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { _ = c.Close(ctx) })
		return c
	}
	a, b := open(), open()

	// The statement under test, exactly as addMemberTx issues it, preceded by the
	// serialization that makes it safe.
	const lock = `select pg_advisory_xact_lock(hashtextextended($1, 0))`
	const ins = `insert into system_member (system_id, component_id, is_primary)
		select s.id, c.id, not exists (select 1 from system_member where component_id = c.id)
		from system s, component c
		where s.name = $1 and c.name = $2
		on conflict (system_id, component_id) do nothing`

	txA, err := a.Begin(ctx)
	if err != nil {
		t.Fatalf("begin a: %v", err)
	}
	defer func() { _ = txA.Rollback(ctx) }()
	if _, err := txA.Exec(ctx, lock, "system_member/race-dsp"); err != nil {
		t.Fatalf("lock a: %v", err)
	}
	if _, err := txA.Exec(ctx, ins, "race-a", "race-dsp"); err != nil {
		t.Fatalf("insert a: %v", err)
	}

	// B starts while A is still open and uncommitted. It must WAIT on the lock
	// rather than deciding from a snapshot that predates A's row; without the lock
	// it would compute "no membership yet" here and claim the default too.
	done := make(chan error, 1)
	go func() {
		txB, err := b.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = txB.Rollback(ctx) }()
		if _, err := txB.Exec(ctx, lock, "system_member/race-dsp"); err != nil {
			done <- err
			return
		}
		if _, err := txB.Exec(ctx, ins, "race-b", "race-dsp"); err != nil {
			done <- err
			return
		}
		done <- txB.Commit(ctx)
	}()

	// Do not commit A until B is actually blocked. Without this the goroutine
	// usually runs after the commit, misses the window entirely, and the test
	// passes no matter what the code does, which is the trap the goroutine-racing
	// version fell into.
	watcher := open()
	blocked := false
	for range 400 {
		var n int
		if err := watcher.QueryRow(ctx, `
			select count(*) from pg_stat_activity
			where wait_event_type = 'Lock' and state = 'active' and pid <> pg_backend_pid()`).Scan(&n); err != nil {
			t.Fatalf("watch: %v", err)
		}
		if n > 0 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("the second transaction never blocked, so the race window was never entered and this test proves nothing")
	}

	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit a: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the second room's write failed instead of becoming an ordinary member: %v", err)
	}

	// Both memberships exist and exactly one is the default.
	rows, err := a.Query(ctx, `select s.name, m.is_primary from system_member m
		join system s on s.id = m.system_id
		where m.component_id = (select id from component where name = 'race-dsp')`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var systems []string
	primaries := 0
	for rows.Next() {
		var s string
		var isPrimary bool
		if err := rows.Scan(&s, &isPrimary); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		systems = append(systems, s)
		if isPrimary {
			primaries++
		}
	}
	rows.Close()
	if len(systems) != 2 {
		t.Errorf("memberships = %v, want both rooms", systems)
	}
	if primaries != 1 {
		t.Errorf("primaries = %d, want exactly 1 (%s)", primaries, strings.Join(systems, ", "))
	}
}
