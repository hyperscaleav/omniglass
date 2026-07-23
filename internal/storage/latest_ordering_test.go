package storage_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A calculated series is current at its HIGHEST id, never its newest ts.
//
// `recordHealth` writes `select clock_timestamp(), ...`, so the timestamp is
// evaluated in the SELECT list while the id comes from the identity sequence
// applied when the row is inserted: the clock is read BEFORE the id is assigned.
// Two concurrent inserts can therefore commit with ts inverted relative to id.
// Every production reader of a recorded verdict already orders by id, so the
// writer and the readers agree; a reader ordering by ts does not, and reports a
// verdict the engine never produced (#356).
//
// The inversion is written directly here rather than raced into existence. The
// race needed six concurrent copies of this package to reproduce once, which is
// no basis for a regression test; the ordering rule it violates is exact and can
// be stated in two rows.
func TestCalculatedSeriesIsCurrentAtHighestID(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn,
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "site", LocationType: "campus"}, all); err != nil {
		t.Fatalf("campus: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "room-sys", LocationName: strptr("site")}, all); err != nil {
		t.Fatalf("system: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// The inversion: the LATER row (higher id) carries the EARLIER ts, exactly as a
	// pair of concurrent inserts can land it.
	for _, r := range []struct {
		value  string
		offset string
	}{
		{"degraded", "0 seconds"},
		{"healthy", "-5 seconds"},
	} {
		if _, err := conn.Exec(ctx, `
			insert into state (ts, owner_kind, system_id, property_id, instance, value, provenance, source_rule)
			values (clock_timestamp() + $1::interval, 'system',
			        (select id from system where name = 'room-sys'), (select id from property where name = 'health'), '', $2, 'calculated', 'test')`,
			r.offset, r.value); err != nil {
			t.Fatalf("insert %s: %v", r.value, err)
		}
	}

	var byID, byTS string
	if err := conn.QueryRow(ctx, `select value from state
		where system_id = (select id from system where name = 'room-sys') and property_id = (select id from property where name = 'health')
		order by id desc limit 1`).Scan(&byID); err != nil {
		t.Fatalf("read by id: %v", err)
	}
	if err := conn.QueryRow(ctx, `select value from state
		where system_id = (select id from system where name = 'room-sys') and property_id = (select id from property where name = 'health')
		order by ts desc limit 1`).Scan(&byTS); err != nil {
		t.Fatalf("read by ts: %v", err)
	}

	// The premise: the two orderings genuinely disagree on these rows, so the
	// assertion below cannot pass by accident.
	if byID == byTS {
		t.Fatalf("the ts/id inversion did not take (both orderings read %q); this test proves nothing", byID)
	}
	if byID != "healthy" {
		t.Fatalf("latest by id = %q, want healthy (the row written second)", byID)
	}

	// LocationHealth reads the RECORDED verdict of each system beneath it
	// (subtreeSystemHealth), which is the production path this rule governs.
	// SystemHealth is deliberately not used here: it recomputes the verdict live
	// and would report `healthy` no matter how the rows are ordered, so it cannot
	// witness this defect.
	rep, err := gw.LocationHealth(ctx, "site", time.Time{}, all)
	if err != nil {
		t.Fatalf("location health: %v", err)
	}
	var got string
	for _, s := range rep.Systems {
		if s.Name == "room-sys" {
			got = s.Verdict
		}
	}
	if got != "healthy" {
		t.Errorf("the recorded verdict reads %q, want healthy: a reader ordering by ts returns a row "+
			"the writer did not consider current", got)
	}
}
