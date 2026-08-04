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

// TestPropertyCacheUpsert proves the latest-value cache: an upsert writes the
// newest value per series in one lookup, the out-of-order guard keeps a late older
// sample from displacing a newer latest, and a declared value coexists with the
// observed one as a separate provenance row (so making property a producer cache
// does not disturb the declared path).
func TestPropertyCacheUpsert(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Postgres timestamptz keeps microsecond precision; truncate at the source so
	// the round-tripped ts compares equal to what we wrote.
	t0 := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)

	// Insert, then a newer value updates in place: one row, the newer value.
	up := func(val string, ts time.Time) {
		t.Helper()
		if err := gw.UpsertProperties(ctx, []storage.PropertyUpsert{{
			OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable",
			Instance: "", Provenance: "observed", Value: json.RawMessage(`"` + val + `"`), TS: ts,
		}}); err != nil {
			t.Fatalf("upsert %s: %v", val, err)
		}
	}
	up("up", t0)
	up("down", t1)

	cv, err := gw.LatestValue(ctx, "component", "disp-1", "interface.reachable", "", "observed", all)
	if err != nil {
		t.Fatalf("latest observed: %v", err)
	}
	if cv == nil || string(cv.Value) != `"down"` || !cv.TS.Equal(t1) {
		t.Fatalf("latest observed: want down@t1, got %+v", cv)
	}

	// Out-of-order guard: an older sample must not displace the newer latest.
	up("stale", t0)
	cv, err = gw.LatestValue(ctx, "component", "disp-1", "interface.reachable", "", "observed", all)
	if err != nil {
		t.Fatalf("latest after stale: %v", err)
	}
	if cv == nil || string(cv.Value) != `"down"` || !cv.TS.Equal(t1) {
		t.Fatalf("out-of-order guard failed: want down@t1, got %+v", cv)
	}

	// A declared value coexists as a separate provenance row (the declared path is
	// untouched): observed stays, declared reads back its own value.
	if _, err := gw.SetProperty(ctx, "", "component", "disp-1", "interface.reachable", "", json.RawMessage(`"up"`), all); err != nil {
		t.Fatalf("set declared: %v", err)
	}
	obs, err := gw.LatestValue(ctx, "component", "disp-1", "interface.reachable", "", "observed", all)
	if err != nil || obs == nil || string(obs.Value) != `"down"` {
		t.Fatalf("observed after declared set: want down, got %+v (err %v)", obs, err)
	}
	dec, err := gw.LatestValue(ctx, "component", "disp-1", "interface.reachable", "", "declared", all)
	if err != nil || dec == nil || string(dec.Value) != `"up"` {
		t.Fatalf("declared read: want up, got %+v (err %v)", dec, err)
	}

	// A provenance with no row is a clean miss.
	told, err := gw.LatestValue(ctx, "component", "disp-1", "interface.reachable", "", "intended", all)
	if err != nil {
		t.Fatalf("latest intended: %v", err)
	}
	if told != nil {
		t.Fatalf("intended should have no row, got %+v", told)
	}

	// An out-of-scope owner is the non-disclosing not-found, not a disclosure.
	if _, err := gw.LatestValue(ctx, "component", "ghost", "interface.reachable", "", "observed", all); err == nil {
		t.Fatal("latest value for unknown component: want not-found error, got nil")
	}
}

// TestReconciliation proves the want/told/is pivot: the declared value is resolved
// live from the cascade (want), the observed value is the cache (is), and drift is
// computed on read (want present, is present, differ). Declared equal to observed
// is no drift. Declared is never a cache row.
func TestReconciliation(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-r"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	byName := func(recs []storage.PropertyReconciliation) map[string]storage.PropertyReconciliation {
		m := make(map[string]storage.PropertyReconciliation, len(recs))
		for _, r := range recs {
			m[r.PropertyTypeName] = r
		}
		return m
	}

	// want: an ad-hoc declared value on the component (a productless component's
	// declared values are all ad-hoc).
	if _, err := gw.SetProperty(ctx, "", "component", "disp-r", "firmware-version", "", json.RawMessage(`"1.0.0"`), all); err != nil {
		t.Fatalf("set declared firmware-version: %v", err)
	}
	// is: an observed value that differs from the declared one.
	if err := gw.UpsertProperties(ctx, []storage.PropertyUpsert{{
		OwnerKind: "component", OwnerID: "disp-r", Key: "firmware-version",
		Instance: "", Provenance: "observed", Value: json.RawMessage(`"2.0.0"`), TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("upsert observed firmware-version: %v", err)
	}

	recs, err := gw.Reconciliation(ctx, "component", "disp-r", all)
	if err != nil {
		t.Fatalf("reconciliation: %v", err)
	}
	fw := byName(recs)["firmware-version"]
	if string(fw.Want) != `"1.0.0"` || string(fw.Is) != `"2.0.0"` || fw.Told != nil || !fw.Drift {
		t.Fatalf("drift pivot: want want=1.0.0 is=2.0.0 told=nil drift=true, got %+v", fw)
	}

	// Reality matching intent: no drift.
	if err := gw.UpsertProperties(ctx, []storage.PropertyUpsert{{
		OwnerKind: "component", OwnerID: "disp-r", Key: "firmware-version",
		Instance: "", Provenance: "observed", Value: json.RawMessage(`"1.0.0"`), TS: time.Now().UTC().Add(time.Second),
	}}); err != nil {
		t.Fatalf("upsert observed match: %v", err)
	}
	recs, err = gw.Reconciliation(ctx, "component", "disp-r", all)
	if err != nil {
		t.Fatalf("reconciliation after match: %v", err)
	}
	if fw := byName(recs)["firmware-version"]; fw.Drift || string(fw.Is) != `"1.0.0"` {
		t.Fatalf("no-drift pivot: want is=1.0.0 drift=false, got %+v", fw)
	}

	// An out-of-scope owner is the non-disclosing not-found.
	if _, err := gw.Reconciliation(ctx, "component", "ghost", all); err == nil {
		t.Fatal("reconciliation for unknown component: want not-found error, got nil")
	}
}
