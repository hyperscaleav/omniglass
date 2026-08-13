package accesspath_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
	"github.com/jackc/pgx/v5"
)

// TestMain routes this package's tests through the storage harness so the
// shared Postgres container is terminated on normal exit. See storagetest.Main.
func TestMain(m *testing.M) {
	os.Exit(storagetest.Main(m))
}

// Parse can be tested on a fixture document; Explain cannot. It wraps a real
// capability (the planner, and a session setting on a real connection), and a
// fixture-only proof would leave the two things that actually decide whether
// this instrument works untested: that `enable_seqscan = off` produces a plan
// naming the index, and that Postgres really does mark the sequential fallback
// disabled instead of failing or silently choosing it anyway.
//
// The fixture is the instrument's own table, not omniglass's schema: what is
// under test here is the reading of a plan, and a scratch table makes the three
// states (index present, index gone, index present but unprovable) exact.
func explainFixture(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	for _, stmt := range []string{
		`create table probe (id bigint generated always as identity primary key, kind text not null, owner uuid, note text)`,
		`insert into probe (kind, owner, note)
		 select 'k' || (g % 3), gen_random_uuid(), 'n' || g from generate_series(1, 200) g`,
		`analyze probe`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("fixture %q: %v", stmt, err)
		}
	}
	return conn
}

const (
	probeRead = `select id, note from probe where kind = $1 and owner is not null order by id desc limit 5`
	// The same filter with nothing to order by, so no other index can offer the
	// planner a path and the sequential fallback is the only one left.
	probeUnordered = `select note from probe where kind = $1 and owner is not null`
)

func TestExplainReportsTheIndexTheQueryCanReach(t *testing.T) {
	ctx := context.Background()
	conn := explainFixture(t)
	if _, err := conn.Exec(ctx, `create index probe_owner_idx on probe (kind, id desc) where owner is not null`); err != nil {
		t.Fatalf("create index: %v", err)
	}

	// The planner PREFERS a sequential scan on a fixture this small, which is
	// the whole reason the assertion is about usability rather than choice.
	// Asserted here so the test proves the distinction it depends on rather
	// than assuming it.
	var preferred string
	if err := conn.QueryRow(ctx, `explain (format json, costs off) `+probeRead, "k1").Scan(&preferred); err != nil {
		t.Fatalf("explain without the setting: %v", err)
	}
	pref, err := accesspath.Parse([]byte(preferred))
	if err != nil {
		t.Fatalf("parse preferred plan: %v", err)
	}
	if scans := pref.Scans("probe"); len(scans) != 1 || scans[0].Node != "Seq Scan" {
		t.Logf("the planner already prefers %v on this fixture; the usability assertion below still holds, but the distinction it exists for is not visible here", scans)
	}

	plan, err := accesspath.Explain(ctx, conn, probeRead, "k1")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if ok, why := plan.MustReach("probe", "probe_owner_idx"); !ok {
		t.Fatalf("the read cannot reach the index it was built for: %s", why)
	}

	// The setting is restored: a connection left with enable_seqscan off would
	// silently re-plan everything the test ran next.
	var setting string
	if err := conn.QueryRow(ctx, "show enable_seqscan").Scan(&setting); err != nil {
		t.Fatalf("show enable_seqscan: %v", err)
	}
	if setting != "on" {
		t.Errorf("enable_seqscan is %q after Explain, want it restored to on", setting)
	}
}

// The two failures that a `pg_indexes` catalog test cannot tell apart from
// success, driven against real Postgres.
func TestExplainReportsAnUnreachableIndex(t *testing.T) {
	ctx := context.Background()

	t.Run("no index at all: the sequential scan comes back disabled", func(t *testing.T) {
		conn := explainFixture(t)
		plan, err := accesspath.Explain(ctx, conn, probeUnordered, "k1")
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		scans := plan.Scans("probe")
		if len(scans) != 1 {
			t.Fatalf("plan scanned probe %d times, want once:\n\t%s", len(scans), plan)
		}
		if scans[0].Node != "Seq Scan" || !scans[0].Disabled {
			t.Errorf("with no index the scan is %s, want a disabled sequential scan", scans[0])
		}
		if ok, _ := plan.MustReach("probe", "probe_owner_idx"); ok {
			t.Error("MustReach passed with no index in the database at all")
		}
	})

	t.Run("another index serves the ordering and answers nothing", func(t *testing.T) {
		// With the index missing but an ORDER BY the primary key can satisfy,
		// the fallback is not a sequential scan at all: the planner walks the
		// pkey and filters every row it reads, which is the shape that reads
		// like an index scan in a plan and costs like a scan of the table.
		conn := explainFixture(t)
		plan, err := accesspath.Explain(ctx, conn, probeRead, "k1")
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		scans := plan.Scans("probe")
		if len(scans) != 1 || scans[0].Index != "probe_pkey" || scans[0].Cond != "" {
			t.Fatalf("fallback path is %v, want the primary key walked with no condition", scans)
		}
		if ok, why := plan.MustReach("probe", "probe_owner_idx"); ok {
			t.Error("MustReach passed on a plan that reaches a different index entirely")
		} else if !strings.Contains(why, "probe_pkey") {
			t.Errorf("refusal said %q, want it to name the path that was taken instead", why)
		}
	})

	t.Run("a partial index whose predicate the query cannot prove", func(t *testing.T) {
		conn := explainFixture(t)
		// The index exists, covers the right columns, and is unusable: the
		// query never states `note is not null`, so the planner cannot prove
		// the partial predicate holds. This is the silent break, and the
		// catalog says the index is present throughout.
		if _, err := conn.Exec(ctx,
			`create index probe_owner_idx on probe (kind, id desc) where note is not null`); err != nil {
			t.Fatalf("create index: %v", err)
		}
		plan, err := accesspath.Explain(ctx, conn, probeRead, "k1")
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		ok, why := plan.MustReach("probe", "probe_owner_idx")
		if ok {
			t.Fatalf("MustReach passed on an index the query cannot use:\n\t%s", plan)
		}
		t.Logf("refused as it should: %s", why)
	})
}
