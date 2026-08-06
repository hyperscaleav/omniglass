package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestDeclaredValuesAppendToSeries proves the #591 model: a declared value is a
// property-series row (provenance declared), an edit appends rather than updates
// (history for free), the current value is the latest row of the series, and an
// unset appends a tombstone rather than deleting history. A repeat set of the
// value already current appends nothing, mirroring the transition-only guard on
// the observed lane, so an idempotent save does not pollute the edit history.
func TestDeclaredValuesAppendToSeries(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "series-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	declaredRows := func() int {
		var n int
		if err := conn.QueryRow(ctx, `
			select count(*) from property
			where owner_kind = 'component'
			  and component_id = (select id from component where name = 'series-1')
			  and property_type_id = (select id from property_type where name = 'firmware-version')
			  and provenance = 'declared'`).Scan(&n); err != nil {
			t.Fatalf("count declared series rows: %v", err)
		}
		return n
	}

	// Two sets are two series rows and one current value: the append IS the edit
	// history.
	if _, err := gw.SetProperty(ctx, "", "component", "series-1", "firmware-version", "", json.RawMessage(`"1.0.0"`), all); err != nil {
		t.Fatalf("set 1.0.0: %v", err)
	}
	if _, err := gw.SetProperty(ctx, "", "component", "series-1", "firmware-version", "", json.RawMessage(`"2.0.0"`), all); err != nil {
		t.Fatalf("set 2.0.0: %v", err)
	}
	if n := declaredRows(); n != 2 {
		t.Fatalf("declared series rows after two sets = %d, want 2 (append, not update-in-place)", n)
	}
	eff := byName(mustResolve(t, gw, "series-1", all))
	if got := eff["firmware-version"]; string(got.Value) != `"2.0.0"` || !got.IsSet {
		t.Fatalf("current value: want the latest series row 2.0.0, got %+v", got)
	}

	// Re-declaring the current value appends nothing: the history records edits,
	// not saves.
	if _, err := gw.SetProperty(ctx, "", "component", "series-1", "firmware-version", "", json.RawMessage(`"2.0.0"`), all); err != nil {
		t.Fatalf("re-set 2.0.0: %v", err)
	}
	if n := declaredRows(); n != 2 {
		t.Fatalf("declared series rows after an identical re-set = %d, want 2 (no duplicate append)", n)
	}

	// Unset appends a tombstone (a third row) rather than deleting: the property
	// leaves the effective read, the current declared value is gone, the history
	// is intact.
	if err := gw.ClearProperty(ctx, "", "component", "series-1", "firmware-version", "", all); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := declaredRows(); n != 3 {
		t.Fatalf("declared series rows after clear = %d, want 3 (a tombstone appends, nothing deletes)", n)
	}
	if got := byName(mustResolve(t, gw, "series-1", all)); len(got["firmware-version"].Value) != 0 || got["firmware-version"].IsSet {
		t.Fatalf("after clear: want firmware-version resolved to nothing, got %+v", got["firmware-version"])
	}
	if cv, err := gw.LatestValue(ctx, "component", "series-1", "firmware-version", "", "declared", all); err != nil || cv != nil {
		t.Fatalf("current declared value after clear: want nil, got %+v (err %v)", cv, err)
	}

	// Clearing what is already unset stays the explicit miss it always was.
	if err := gw.ClearProperty(ctx, "", "component", "series-1", "firmware-version", "", all); !errors.Is(err, storage.ErrPropertyNotFound) {
		t.Fatalf("clear an unset property: want ErrPropertyNotFound, got %v", err)
	}

	// Setting again after a clear is a fresh edit: a fourth row, current again.
	if _, err := gw.SetProperty(ctx, "", "component", "series-1", "firmware-version", "", json.RawMessage(`"3.0.0"`), all); err != nil {
		t.Fatalf("set after clear: %v", err)
	}
	if n := declaredRows(); n != 4 {
		t.Fatalf("declared series rows after re-set = %d, want 4", n)
	}
	if got := byName(mustResolve(t, gw, "series-1", all)); string(got["firmware-version"].Value) != `"3.0.0"` {
		t.Fatalf("after re-set: want 3.0.0 current, got %+v", got["firmware-version"])
	}
}

// TestPropertyValueStoreRetired asserts the #591 outcome as it stands after
// #588 took the freed name: the value STORE is gone (the `property` table is
// now the series, not a latest-value cache, so it carries no store shape), and
// the series admits declared rows (the #460 narrowing is reversed) with the
// no-lineage arm.
func TestPropertyValueStoreRetired(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn := connectMigrated(t)

	// The store's cache shape (created_at/updated_at, the series-unique key) must
	// not survive on the table that took its name: `property` is append-only
	// series rows, keyed by nothing but its identity id.
	for _, col := range []string{"created_at", "updated_at"} {
		var present bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.columns
			 where table_name = 'property' and column_name = $1)`, col).Scan(&present); err != nil {
			t.Fatalf("check property.%s: %v", col, err)
		}
		if present {
			t.Errorf("property carries %q, a value-store cache column; the store did not retire", col)
		}
	}
	var seriesKey bool
	if err := conn.QueryRow(ctx, `
		select exists (select 1 from pg_constraint
		where conrelid = 'property'::regclass and contype = 'u')`).Scan(&seriesKey); err != nil {
		t.Fatalf("check unique constraints: %v", err)
	}
	if seriesKey {
		t.Error("property has a unique series key; the series is append-only, only the store deduped per series")
	}

	// The series takes a declared row, with no lineage.
	mustExec(t, conn, `insert into property_type (name, data_type) values ('retire-prop', 'string')`)
	mustExec(t, conn, `insert into component (name) values ('retire-c1')`)
	mustExec(t, conn, `
		insert into property (owner_kind, component_id, property_type_id, instance, provenance, value)
		values ('component', (select id from component where name = 'retire-c1'),
		        (select id from property_type where name = 'retire-prop'), '', 'declared', to_jsonb('v1'::text))`)

	// The lineage check holds its shape: a declared row must not carry an event.
	if _, err := conn.Exec(ctx, `
		insert into property (owner_kind, component_id, property_type_id, instance, provenance, value, event_id)
		values ('component', (select id from component where name = 'retire-c1'),
		        (select id from property_type where name = 'retire-prop'), '', 'declared', to_jsonb('v2'::text), 1)`); err == nil {
		t.Fatal("property accepted a declared row with event lineage; the declared arm of property_lineage_check is missing or wrong")
	}
}
