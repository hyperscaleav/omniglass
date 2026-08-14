package storage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// What breaks a same-instant tie in a property series, and why it is already a
// total order (#712).
//
// The issue this test answers reported that the series "breaks a ts tie on a
// random uuid", so two values sharing a timestamp resolve to whichever uuid
// sorts higher, and asked for a tiebreak reflecting insertion order instead.
// The premise does not hold, and the tiebreak it asks for is the one already
// there. `order by ts desc, id desc` breaks the tie on `property.id`, which is
// a **bigint identity column**: it is not a uuid of any kind, random or
// time-ordered, and it is allocated at insert, so `id desc` IS insertion order
// descending. There is nothing arbitrary for a tie to fall through to.
//
// That is asserted here in two ways rather than argued, because "the issue was
// wrong about the mechanism" is a claim with a short shelf life if nothing
// holds it. The first assertion reads the column's shape out of the live
// catalog, so converting `id` to a `gen_random_uuid()` default (the shape the
// issue described) fails here loudly, naming this reasoning. The second drives
// a genuine tie, which the public API cannot produce at all, and shows it
// resolving to the later insert every time.
//
// # What the ordering does depend on, which is worth stating
//
// `ts` LEADS, and it is a wall-clock value. That is deliberate and cannot be
// swapped for the sequence: the observed lane accepts a caller-supplied `ts`
// (a node backfilling, a device reporting a reading it took a minute ago), and
// a late-arriving older sample must not become the current value merely because
// it was written last. So the series is ordered by the value's own time, and
// insertion order is only the tiebreak. The consequence, stated so it is not
// rediscovered as a surprise: if the database's clock steps BACKWARDS between
// two writes to one series, the earlier-written row wins, because it carries the
// later `ts`. The tiebreak cannot help there, since there is no tie.
func TestTheSeriesTiebreakIsInsertionOrderNotARandomIdentifier(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn := connectDSN(t, dsn)
	all := scope.Set{All: true}

	// The mechanism check, read from the catalog rather than from a migration
	// file: what the running database says the tiebreak column is.
	var dataType, isIdentity string
	var columnDefault *string
	if err := conn.QueryRow(ctx, `select data_type, column_default, is_identity
		from information_schema.columns where table_name = 'property' and column_name = 'id'`).
		Scan(&dataType, &columnDefault, &isIdentity); err != nil {
		t.Fatalf("read property.id from the catalog: %v", err)
	}
	if dataType != "bigint" || isIdentity != "YES" {
		def := "<none>"
		if columnDefault != nil {
			def = *columnDefault
		}
		t.Fatalf("property.id is %s (identity=%s, default=%s), want a bigint identity column.\n"+
			"The series tiebreak `order by ts desc, id desc` is insertion order ONLY while this column is monotonic at insert. "+
			"If it is now a uuid, a same-instant tie resolves arbitrarily, which is the defect #712 described and this test exists to catch arriving.",
			dataType, isIdentity, def)
	}

	// A genuine tie, which no public write path can produce: SetProperty opens
	// its own transaction per call and `ts` defaults to transaction_timestamp,
	// so two declared writes never share a ts to begin with. The rows go in by
	// hand so the tiebreak is actually exercised rather than assumed to be.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "tie-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	tie := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, v := range []string{`"first"`, `"second"`} {
		if _, err := conn.Exec(ctx, `insert into property (owner_kind, component_id, property_type_id, instance, provenance, value, ts)
			values ('component', (select id from component where name = 'tie-1'),
			        (select id from property_type where name = 'firmware-version'), '', 'declared', $1::jsonb, $2)`, v, tie); err != nil {
			t.Fatalf("insert %s: %v", v, err)
		}
	}
	var lower, higher int64
	if err := conn.QueryRow(ctx, `select min(id), max(id) from property where provenance = 'declared'`).Scan(&lower, &higher); err != nil {
		t.Fatalf("read the tied ids: %v", err)
	}
	if higher <= lower {
		t.Fatalf("the two tied rows carry ids %d and %d: the second insert did not take a higher id, so this test cannot tell insertion order from anything else", lower, higher)
	}

	// Both read paths, repeatedly. Repetition is the point: a coin flip passes a
	// single look half the time, which is exactly how the behaviour the issue
	// described would have survived review.
	for i := 0; i < 20; i++ {
		cv, err := gw.LatestValue(ctx, "component", "tie-1", "firmware-version", "", "declared", all)
		if err != nil || cv == nil {
			t.Fatalf("latest value: %+v (err %v)", cv, err)
		}
		if string(cv.Value) != `"second"` {
			t.Fatalf("look %d: the tied series resolved to %s, want the later insert \"second\": the tiebreak is not insertion order", i, cv.Value)
		}
		eff := byName(mustResolve(t, gw, "tie-1", all))["firmware-version"]
		if string(eff.Value) != `"second"` {
			t.Fatalf("look %d: the effective read resolved the tie to %s, want \"second\": the two read paths order the same series differently", i, eff.Value)
		}
	}

	// And the ordinary path, which never ties: two writes through the gateway
	// carry strictly increasing ts AND strictly increasing id, so the two
	// orderings agree and the tiebreak is never consulted. This is why the test
	// #712 was filed from could not have been decided by a tiebreak at all.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "tie-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	for _, v := range []string{`"1.0.0"`, `"2.0.0"`} {
		if _, err := gw.SetProperty(ctx, "", "component", "tie-2", "firmware-version", "", json.RawMessage(v), all, all); err != nil {
			t.Fatalf("set %s: %v", v, err)
		}
	}
	var byTS, byID string
	if err := conn.QueryRow(ctx, `select
			(select value #>> '{}' from property where component_id = c.id and provenance = 'declared' order by ts desc limit 1),
			(select value #>> '{}' from property where component_id = c.id and provenance = 'declared' order by id desc limit 1)
		from component c where c.name = 'tie-2'`).Scan(&byTS, &byID); err != nil {
		t.Fatalf("compare the two orderings: %v", err)
	}
	if byTS != "2.0.0" || byID != "2.0.0" {
		t.Errorf("latest by ts = %q, latest by id = %q, want both 2.0.0: the value's own time and its insertion order must agree on a series the gateway wrote", byTS, byID)
	}
}
