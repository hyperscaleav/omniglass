package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// #2735: the default window carries in the health in force at its start, as an
// explicit since does, so a reader (the console's Uptime) never takes the first
// edge inside the window for the health that held before it.
func TestTheDefaultWindowCarriesInTheHealthBeforeIt(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	// The system's own record, moved into the past in order: healthy 40 days
	// ago (before a 30-day window), incomplete 10 days ago, healthy an hour later.
	rows, err := f.conn.Query(ctx, `select id from property where system_id = (select id from system where name = 'hq-huddle')
		and property_type_id = (select id from property_type where name = 'health') order by id`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 3 {
		t.Fatalf("hq-huddle has %d health rows, want its three (healthy, incomplete, healthy)", len(ids))
	}
	now := time.Now()
	for i, at := range []time.Time{now.Add(-40 * 24 * time.Hour), now.Add(-10 * 24 * time.Hour), now.Add(-10*24*time.Hour + time.Hour)} {
		if _, err := f.conn.Exec(ctx, `update property set ts = $1 where id = $2`, at, ids[i]); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", storage.HealthRange{Window: 30 * 24 * time.Hour}, f.all, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CarriedIn != "healthy" {
		t.Errorf("carried in = %q, want healthy, the health in force when the 30-day window opened", rep.CarriedIn)
	}
	if len(rep.Transitions) != 2 || rep.Transitions[0].Value != "incomplete" {
		t.Errorf("transitions in the window = %+v, want incomplete then healthy", rep.Transitions)
	}
	// The whole history has no start to carry in to.
	if rep, err := f.gw.SystemHealth(ctx, "hq-huddle", storage.HealthRange{}, f.all, f.all); err != nil || rep.CarriedIn != "" {
		t.Errorf("whole-history carried in = %q (%v), want none", rep.CarriedIn, err)
	}
}
