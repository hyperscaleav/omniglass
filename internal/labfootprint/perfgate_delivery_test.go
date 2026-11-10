package labfootprint

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

// The delivery read in the perf gate (#1695): every telemetry kind's loss at
// every hop the table prints is an exact figure, read the way `lab perf check
// --samples` reads it, from the JSON a run wrote.

// viaJSON is the report as a check reads it back from lab-measure.json or a
// sample series: only what the JSON carries survives.
func viaJSON(t *testing.T, r ScaleReport) ScaleReport {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out ScaleReport
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// deliveryRow is one kind across the ingest hop with its outcomes, loss
// derived the way DeliveryOf derives it.
func deliveryRow(kind delivery.Kind, outcomes map[delivery.Outcome]uint64) DeliveryRow {
	var tot delivery.Total
	for o, n := range outcomes {
		if o.Out() {
			tot.Out += n
		}
		if !o.Deduplicated() {
			tot.In += n
		}
	}
	row := DeliveryRow{Hop: delivery.Ingest, Kind: kind, In: tot.In, Out: tot.Out, Outcomes: outcomes}
	if l, ok := delivery.Loss(tot.In, tot.Out); ok {
		row.Loss = &l
	}
	return row
}

// Every kind at 0 passes, and each kind is its own row, so a pass is a read
// of every kind and not a vacuous one.
func TestComparePerfDeliveryEveryLossZeroPasses(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(_ int, r *ScaleReport) {
		r.Delivery = nil
		for _, k := range delivery.Kinds {
			r.Delivery = append(r.Delivery, deliveryRow(k, map[delivery.Outcome]uint64{delivery.Stored: 40}))
		}
		*r = viaJSON(t, *r)
	})
	v := ComparePerf(base, runs, nil)
	if v.Status != PerfPass {
		var b bytes.Buffer
		v.WriteTable(&b)
		t.Fatalf("status %s, want pass:\n%s", v.Status, b.String())
	}
	for _, k := range delivery.Kinds {
		f := figureNamed(t, v, "delivery_loss/ingest/"+string(k))
		if f.Status != PerfPass || f.Class != ClassExact || f.Observed != 0 || f.Note != "" {
			t.Fatalf("%s = %+v, want an exact pass at 0 with no note", f.Name, f)
		}
	}
	if f := figureNamed(t, v, "delivery_unsettled_boots"); f.Status != PerfPass {
		t.Fatalf("delivery_unsettled_boots = %+v, want pass", f)
	}
}

// One row above 0 fails the check, and the failed row names the kind, the
// hop and the cause, even read back from the run's JSON (which carries the
// outcomes, not the cause). There is no tolerance: one item in a thousand
// fails.
func TestComparePerfDeliveryLossNamesKindHopAndCause(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	cases := []struct {
		name     string
		outcomes map[delivery.Outcome]uint64
		cause    delivery.Outcome
		lost     string
	}{
		{"items that never landed", map[delivery.Outcome]uint64{delivery.Stored: 7, delivery.Missing: 3}, delivery.Missing, "3 of 10"},
		{"one orphan in a thousand", map[delivery.Outcome]uint64{delivery.Stored: 999, delivery.Orphaned: 1}, delivery.Orphaned, "1 of 1000"},
		{"the largest cause of two", map[delivery.Outcome]uint64{delivery.Stored: 90, delivery.Missing: 2, delivery.RefusedWrite: 8}, delivery.RefusedWrite, "10 of 100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runs := runsWith(3, func(i int, r *ScaleReport) {
				r.Delivery = []DeliveryRow{
					deliveryRow(delivery.Metric, map[delivery.Outcome]uint64{delivery.Stored: 40}),
					deliveryRow(delivery.Property, map[delivery.Outcome]uint64{delivery.Stored: 40}),
				}
				if i == 1 {
					r.Delivery[1] = deliveryRow(delivery.Property, tc.outcomes)
				}
				*r = viaJSON(t, *r)
			})
			v := ComparePerf(base, runs, nil)
			if v.Status != PerfFail || !v.Failed() {
				t.Fatalf("status %s, want fail", v.Status)
			}
			if f := figureNamed(t, v, "delivery_loss/ingest/metric"); f.Status != PerfPass {
				t.Fatalf("the lossless kind reads %s, want pass", f.Status)
			}
			f := figureNamed(t, v, "delivery_loss/ingest/property")
			if f.Status != PerfFail || f.Class != ClassExact || f.Observed <= 0 {
				t.Fatalf("the lossy kind = %+v, want an exact fail above 0", f)
			}
			for _, want := range []string{"property", "ingest", "cause " + string(tc.cause), tc.lost} {
				if !strings.Contains(f.Note, want) {
					t.Errorf("the note %q does not name %q", f.Note, want)
				}
			}
			var b bytes.Buffer
			v.WriteTable(&b)
			named := false
			for _, line := range strings.Split(b.String(), "\n") {
				if strings.HasPrefix(line, string(PerfFail)) && strings.Contains(line, "delivery_loss/ingest/property") &&
					strings.Contains(line, "cause "+string(tc.cause)) {
					named = true
				}
			}
			if !named {
				t.Errorf("the table does not print the failed row with its cause:\n%s", b.String())
			}
		})
	}
}

// The worst run's cause is the one named: a run that lost more says why.
func TestComparePerfDeliveryLossNamesTheWorstRunsCause(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(i int, r *ScaleReport) {
		switch i {
		case 0:
			r.Delivery = []DeliveryRow{deliveryRow(delivery.Metric, map[delivery.Outcome]uint64{delivery.Stored: 99, delivery.Missing: 1})}
		case 2:
			r.Delivery = []DeliveryRow{deliveryRow(delivery.Metric, map[delivery.Outcome]uint64{delivery.Stored: 50, delivery.Orphaned: 50})}
		}
	})
	f := figureNamed(t, ComparePerf(base, runs, nil), "delivery_loss/ingest/metric")
	if f.Status != PerfFail || f.Observed != 0.5 || !strings.Contains(f.Note, "cause orphaned") {
		t.Fatalf("figure = %+v, want a fail at 0.5 naming orphaned", f)
	}
}

// A boot whose batches had not all settled fails, by name: the gate never
// passes on a ratio it could not read.
func TestComparePerfDeliveryUnsettledBootFailsByName(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(i int, r *ScaleReport) {
		if i == 2 {
			r.DeliveryUnsettled = []UnsettledBoot{{Node: "edge-hq", Boot: "b-7f3a"}}
		}
		*r = viaJSON(t, *r)
	})
	v := ComparePerf(base, runs, nil)
	if v.Status != PerfFail {
		t.Fatalf("status %s, want fail", v.Status)
	}
	f := figureNamed(t, v, "delivery_unsettled_boots")
	if f.Status != PerfFail || f.Observed != 1 {
		t.Fatalf("figure = %+v, want a fail at 1", f)
	}
	for _, want := range []string{"edge-hq", "b-7f3a"} {
		if !strings.Contains(f.Note, want) {
			t.Errorf("the note %q does not name %q", f.Note, want)
		}
	}
}

// Many unsettled boots name the first few and count the rest, so the table
// stays a table.
func TestComparePerfDeliveryUnsettledBootsNoteIsBounded(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(_ int, r *ScaleReport) {
		for _, n := range []string{"a", "b", "c", "d", "e"} {
			r.DeliveryUnsettled = append(r.DeliveryUnsettled, UnsettledBoot{Node: "edge-" + n, Boot: "boot-" + n})
		}
		r.DeliveryNote = "unsettled: the telemetry consumer still held batches when the settle ran out (4 pending, 0 unacked)"
	})
	f := figureNamed(t, ComparePerf(base, runs, nil), "delivery_unsettled_boots")
	if f.Status != PerfFail || f.Observed != 5 {
		t.Fatalf("figure = %+v, want a fail at 5", f)
	}
	for _, want := range []string{"edge-a", "edge-c", "and 2 more", "still held batches"} {
		if !strings.Contains(f.Note, want) {
			t.Errorf("the note %q does not hold %q", f.Note, want)
		}
	}
	if strings.Contains(f.Note, "edge-e") {
		t.Errorf("the note %q names every boot, want the first three", f.Note)
	}
}

// A run whose delivery read is not measured fails, saying why.
func TestComparePerfDeliveryNotMeasuredFails(t *testing.T) {
	base := baselineOf(t, runsWith(3, nil))
	runs := runsWith(3, func(i int, r *ScaleReport) {
		if i == 0 {
			r.Delivery, r.DeliveryNotRead = nil, "the database answered no receipts"
		}
		*r = viaJSON(t, *r)
	})
	v := ComparePerf(base, runs, nil)
	if v.Status != PerfFail {
		t.Fatalf("status %s, want fail", v.Status)
	}
	f := figureNamed(t, v, "delivery_loss")
	if f.Status != PerfFail || !strings.Contains(f.Note, "not measured: the database answered no receipts") {
		t.Fatalf("figure = %+v, want a fail saying the read was not measured and why", f)
	}
}

// The delivery table prints a row's cause from its outcomes, so a report
// read back from its JSON prints the same table it printed when measured.
func TestDeliveryTableCauseSurvivesJSON(t *testing.T) {
	r := ScaleReport{Delivery: []DeliveryRow{deliveryRow(delivery.Metric, map[delivery.Outcome]uint64{delivery.Stored: 7, delivery.Missing: 3})}}
	var measured, read strings.Builder
	r.deliveryTable(&measured)
	viaJSON(t, r).deliveryTable(&read)
	if measured.String() != read.String() {
		t.Fatalf("the table read back from JSON differs:\nmeasured:\n%s\nread:\n%s", measured.String(), read.String())
	}
	if !strings.Contains(read.String(), "missing") {
		t.Fatalf("the table does not name the cause:\n%s", read.String())
	}
}
