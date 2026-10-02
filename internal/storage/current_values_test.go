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

// TestCurrentValueReads proves the #591 model on the read side: the current
// value of a series is its latest row by the value's own time (a late-arriving
// older sample lands in the series but never displaces the newer current
// value), a declared value coexists as its own provenance series, and a
// provenance with no rows is a clean miss.
func TestCurrentValueReads(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Postgres timestamptz keeps microsecond precision; truncate at the source so
	// the round-tripped ts compares equal to what we wrote.
	t0 := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)

	obs := func(val string, ts time.Time) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
			OwnerKind: "component", OwnerID: "disp-1", Key: "endpoint-reachable",
			Instance: "", Value: val, TS: ts,
		}}); err != nil {
			t.Fatalf("insert observed %s: %v", val, err)
		}
	}
	obs("up", t0)
	obs("down", t1)

	cv, err := gw.LatestValue(ctx, "component", "disp-1", "endpoint-reachable", "", "observed", all)
	if err != nil {
		t.Fatalf("latest observed: %v", err)
	}
	if cv == nil || string(cv.Value) != `"down"` || !cv.TS.Equal(t1) {
		t.Fatalf("latest observed: want down@t1, got %+v", cv)
	}

	// A late-arriving older sample joins the series but the current value stays
	// the newest by the value's own time.
	obs("stale", t0)
	cv, err = gw.LatestValue(ctx, "component", "disp-1", "endpoint-reachable", "", "observed", all)
	if err != nil {
		t.Fatalf("latest after stale: %v", err)
	}
	if cv == nil || string(cv.Value) != `"down"` || !cv.TS.Equal(t1) {
		t.Fatalf("late old sample displaced the current value: want down@t1, got %+v", cv)
	}

	// A declared value coexists as its own provenance series: observed stays,
	// declared reads back its own value.
	if _, err := gw.SetProperty(ctx, "", "component", "disp-1", "endpoint-reachable", "", json.RawMessage(`"up"`), all, all); err != nil {
		t.Fatalf("set declared: %v", err)
	}
	obsAfter, err := gw.LatestValue(ctx, "component", "disp-1", "endpoint-reachable", "", "observed", all)
	if err != nil || obsAfter == nil || string(obsAfter.Value) != `"down"` {
		t.Fatalf("observed after declared set: want down, got %+v (err %v)", obsAfter, err)
	}
	dec, err := gw.LatestValue(ctx, "component", "disp-1", "endpoint-reachable", "", "declared", all)
	if err != nil || dec == nil || string(dec.Value) != `"up"` {
		t.Fatalf("declared read: want up, got %+v (err %v)", dec, err)
	}

	// A provenance with no row is a clean miss.
	told, err := gw.LatestValue(ctx, "component", "disp-1", "endpoint-reachable", "", "intended", all)
	if err != nil {
		t.Fatalf("latest intended: %v", err)
	}
	if told != nil {
		t.Fatalf("intended should have no row, got %+v", told)
	}

	// An out-of-scope owner is the non-disclosing not-found, not a disclosure.
	if _, err := gw.LatestValue(ctx, "component", "ghost", "endpoint-reachable", "", "observed", all); err == nil {
		t.Fatal("latest value for unknown component: want not-found error, got nil")
	}
}

// TestReconciliation proves the want/told/is pivot: the declared value is
// resolved live from the cascade (want), the observed value is the series'
// latest row (is), and drift is computed on read (want present, is present,
// differ). Declared equal to observed is no drift.
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-r"}, all, all, all, all); err != nil {
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
	if _, err := gw.SetProperty(ctx, "", "component", "disp-r", "firmware-version", "", json.RawMessage(`"1.0.0"`), all, all); err != nil {
		t.Fatalf("set declared firmware-version: %v", err)
	}
	// is: an observed series row that differs from the declared value.
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "disp-r", Key: "firmware-version",
		Instance: "", Value: "2.0.0", TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("insert observed firmware-version: %v", err)
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
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "disp-r", Key: "firmware-version",
		Instance: "", Value: "1.0.0", TS: time.Now().UTC().Add(time.Second),
	}}); err != nil {
		t.Fatalf("insert observed match: %v", err)
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
