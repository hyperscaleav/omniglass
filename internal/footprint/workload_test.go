package footprint_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/footprint"
	"google.golang.org/protobuf/proto"
)

var start = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func standard() footprint.Workload {
	return footprint.Workload{Mix: footprint.StandardMix, Cadence: 30 * time.Second, Start: start}
}

// TestBatchIsDeterministic: the same task and tick always build the same
// batch, so two runs of the harness land byte-identical workloads and a count
// that moves is the code under test moving, never the workload.
func TestBatchIsDeterministic(t *testing.T) {
	w := standard()
	task := footprint.Task{ID: "t-1", Node: "edge-hq"}
	for tick := 0; tick < 5; tick++ {
		a, b := w.Batch(task, tick), w.Batch(task, tick)
		if !proto.Equal(a, b) {
			t.Fatalf("tick %d: two builds differ:\n%v\n%v", tick, a, b)
		}
	}
	if proto.Equal(w.Batch(footprint.Task{ID: "t-1", Node: "edge-hq"}, 1), w.Batch(footprint.Task{ID: "t-2", Node: "edge-hq"}, 1)) {
		t.Fatal("two tasks built the same batch: a noisy signal must vary by task")
	}
}

// TestBatchCarriesTheMix: every tick carries every signal of the mix, on the
// lane the mix declares, stamped at the tick's time, bound to the task.
func TestBatchCarriesTheMix(t *testing.T) {
	w := standard()
	b := w.Batch(footprint.Task{ID: "t-9", Node: "edge-hq"}, 3)
	if b.GetTaskId() != "t-9" || b.GetNodeId() != "edge-hq" {
		t.Fatalf("batch not bound to its task: task %q node %q", b.GetTaskId(), b.GetNodeId())
	}
	var metrics, properties int
	for _, s := range footprint.StandardMix {
		switch s.Lane {
		case footprint.MetricLane:
			metrics++
		case footprint.PropertyLane:
			properties++
		}
	}
	if len(b.GetMetrics()) != metrics || len(b.GetProperties()) != properties {
		t.Fatalf("batch carries %d metrics and %d properties, the mix declares %d and %d",
			len(b.GetMetrics()), len(b.GetProperties()), metrics, properties)
	}
	want := start.Add(3 * 30 * time.Second)
	if got := b.GetTs().AsTime(); !got.Equal(want) {
		t.Fatalf("tick 3 stamped %v, want %v", got, want)
	}
}

// TestSignalShapes: a constant never moves, a counter advances by the cadence
// each tick (an uptime in seconds), and a noisy signal stays inside its band
// and is not constant.
func TestSignalShapes(t *testing.T) {
	w := standard()
	task := footprint.Task{ID: "t-1", Node: "edge-hq"}
	values := func(key string) []float64 {
		var out []float64
		for tick := 0; tick < 20; tick++ {
			for _, m := range w.Batch(task, tick).GetMetrics() {
				if m.GetName() == key {
					out = append(out, m.GetValue())
				}
			}
		}
		return out
	}
	for _, s := range footprint.StandardMix {
		if s.Lane != footprint.MetricLane {
			continue
		}
		v := values(s.Key)
		if len(v) != 20 {
			t.Fatalf("%s: %d values over 20 ticks", s.Key, len(v))
		}
		switch s.Shape {
		case footprint.Constant:
			for i := range v {
				if v[i] != s.Base {
					t.Errorf("%s: constant %v moved to %v at tick %d", s.Key, s.Base, v[i], i)
				}
			}
		case footprint.Counter:
			for i := 1; i < len(v); i++ {
				if v[i]-v[i-1] != w.Cadence.Seconds() {
					t.Errorf("%s: counter stepped %v at tick %d, want the cadence %v", s.Key, v[i]-v[i-1], i, w.Cadence.Seconds())
				}
			}
		case footprint.Noisy:
			distinct := map[float64]bool{}
			for i := range v {
				if v[i] < s.Base-s.Spread || v[i] > s.Base+s.Spread {
					t.Errorf("%s: %v at tick %d is outside %v +/- %v", s.Key, v[i], i, s.Base, s.Spread)
				}
				distinct[v[i]] = true
			}
			if len(distinct) < 2 {
				t.Errorf("%s: noisy signal never varied", s.Key)
			}
		}
	}
}

// TestPropertyValuesAreCanonicalJSON: a property's value rides as canonical
// JSON text, which ingest parses against the property type.
func TestPropertyValuesAreCanonicalJSON(t *testing.T) {
	for _, p := range standard().Batch(footprint.Task{ID: "t-1", Node: "edge-hq"}, 0).GetProperties() {
		if v := p.GetValueJson(); len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
			t.Errorf("%s: value %q is not a JSON string", p.GetName(), v)
		}
	}
}
