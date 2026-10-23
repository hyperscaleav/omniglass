package storage_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func (r *systemRig) occupancy(comp string, v float64) {
	r.t.Helper()
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.ids[comp],
		Key: "occupancy-count", Value: v, TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

// A preview orders its owners on the server (#999's review): the owners
// holding a current output first, before the cap, so the cap never hides an
// owner whose value the rule would change; the rows it returns lead with
// the owners that hold a value or would write one, then by name.
func TestCalcPreviewOrdersValuedFirst(t *testing.T) {
	r := newSystemRig(t)
	for _, c := range []string{"m1", "m2", "m3", "m4"} {
		r.occupancy(c, 2)
	}
	// m4, last by name and by creation, already holds the output.
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.ids["m4"],
		Key: "temp-sum", Value: 9, TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	defer storage.SetCalcPreviewOwnerCap(2)()
	pv, err := r.gw.PreviewCalcRule(r.ctx, storage.CalcRuleSpec{OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "occupancy-count"}}, Reduce: rules.CalcReduce{Expr: "max(t) * 1.0"}, OutputKey: "temp-sum"},
		r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Truncated || pv.OwnersJudged != 2 || pv.OwnersTotal != 4 || len(pv.Owners) != 2 {
		t.Fatalf("preview %+v, want 2 of 4 owners, truncated", pv)
	}
	if pv.Owners[0].OwnerName != "m1" || pv.Owners[1].OwnerName != "m4" || string(pv.Owners[1].Current) != "9" {
		t.Fatalf("preview owners %s, %s; want m1 and m4 (holding 9), by name", pv.Owners[0].OwnerName, pv.Owners[1].OwnerName)
	}
}

// A rule's state is ordered on the server (#999's review): failing owners,
// then pending, then those missing an input (or with no members), then the
// valued, each by name; a limit bounds the owners listed and the total
// says how many there are, and the pending depth counts every readable
// owner, listed or not.
func TestCalcRuleStateOrderAndLimit(t *testing.T) {
	r := newSystemRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "ratio", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "occupancy-count"}, {Alias: "u", Key: "room-temperature"}},
		Reduce: rules.CalcReduce{Expr: "10.0 / (max(t) - 2) + max(u)"}, OutputKey: "temp-sum"}); err != nil {
		t.Fatal(err)
	}
	r.occupancy("m1", 2) // 10 / 0: not a finite number, an error
	r.temp("m1", "", 1)
	r.occupancy("m2", 4) // valued: 5 + 1
	r.temp("m2", "", 1)
	r.occupancy("m3", 4) // no temperature: missing
	r.occupancy("m4", 7)
	r.temp("m4", "", 1)
	r.drain()
	r.occupancy("m4", 12) // pending again
	all, err := r.gw.CalcRuleState(r.ctx, "ratio", r.all, r.all, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, o := range all.Owners {
		names = append(names, o.OwnerName)
	}
	if got := len(names); got != 4 || names[0] != "m1" || names[1] != "m4" || names[2] != "m3" || names[3] != "m2" || all.Total != 4 {
		t.Fatalf("state order %v (total %d), want m1 (error), m4 (pending), m3 (missing), m2 (valued)", names, all.Total)
	}
	two, err := r.gw.CalcRuleState(r.ctx, "ratio", r.all, r.all, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Owners) != 2 || two.Owners[0].OwnerName != "m1" || two.Owners[1].OwnerName != "m4" || two.Total != 4 || two.PendingDepth != 1 {
		t.Fatalf("a limit of 2: %+v, want m1 and m4 of 4, one pending", two)
	}
}
