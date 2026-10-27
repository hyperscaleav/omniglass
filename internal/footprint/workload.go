// Package footprint builds the standard ingest workload the storage footprint
// is measured against (#1272): node-shaped telemetry batches, one per task per
// tick, deterministic for a given task and tick so two runs land the same rows
// and a count that moves is the code under test moving, never the workload.
//
// The workload is declared by shape, not by driver. Storage cost follows how
// many series a component carries, which lane each rides, how often it is
// checked and how often its value changes, not the protocol that produced it.
// The mix is calibrated to a measurement (StandardMix's comment) and is
// re-derived when the simulated fleet changes, rather than replaying a
// particular simulator.
package footprint

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"time"

	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Lane is the sample lane a signal rides.
type Lane int

const (
	MetricLane Lane = iota
	PropertyLane
)

// Shape is how a signal's value moves from tick to tick.
type Shape int

const (
	// Constant never moves: a reachability result that holds, a firmware
	// version. On the property lane only its first observation is stored.
	Constant Shape = iota
	// Noisy varies inside Base +/- Spread, to two decimals: a response time.
	Noisy
	// Counter starts at Base and advances by the cadence in seconds each tick:
	// an uptime.
	Counter
)

// Signal is one series every task carries.
type Signal struct {
	Key    string
	Lane   Lane
	Shape  Shape
	Base   float64
	Spread float64
	// Text is a property's value, carried as a JSON string.
	Text string
}

// StandardMix is the signals one task's run carries, calibrated to the
// 2026-10-27 measurement of the dev scenario at the product's 30 s cadence:
// about 1,000 metric rows a minute from 130 simulated endpoints (about four
// metric rows per endpoint per check), roughly half the metric series constant
// over a window, the probe results the largest writers, and the property lane
// sparse because it stores transitions only.
var StandardMix = []Signal{
	{Key: "icmp-reachable", Lane: MetricLane, Shape: Constant, Base: 1},
	{Key: "tcp-open", Lane: MetricLane, Shape: Constant, Base: 1},
	{Key: "tcp-connect-time", Lane: MetricLane, Shape: Noisy, Base: 1.5, Spread: 1},
	{Key: "uptime", Lane: MetricLane, Shape: Counter, Base: 86400},
	{Key: "endpoint-reachable", Lane: PropertyLane, Shape: Constant, Text: "up"},
}

// Task is the task a batch is published for, and the node that ran it.
type Task struct {
	ID   string
	Node string
}

// Workload is a mix checked at a cadence from a start time.
type Workload struct {
	Mix     []Signal
	Cadence time.Duration
	Start   time.Time
}

// Batch is what task publishes on its tick-th check: every signal of the mix,
// stamped at the tick's time.
func (w Workload) Batch(task Task, tick int) *ogv1.TelemetryBatch {
	ts := w.Start.Add(time.Duration(tick) * w.Cadence)
	b := &ogv1.TelemetryBatch{TaskId: task.ID, NodeId: task.Node, Ts: timestamppb.New(ts)}
	for _, s := range w.Mix {
		switch s.Lane {
		case MetricLane:
			b.Metrics = append(b.Metrics, &ogv1.MetricSample{Name: s.Key, Value: w.value(s, task, tick)})
		case PropertyLane:
			b.Properties = append(b.Properties, &ogv1.PropertySample{Name: s.Key, ValueJson: `"` + s.Text + `"`})
		}
	}
	return b
}

func (w Workload) value(s Signal, task Task, tick int) float64 {
	switch s.Shape {
	case Noisy:
		return math.Round((s.Base+s.Spread*(2*unit(task.ID, s.Key, tick)-1))*100) / 100
	case Counter:
		return s.Base + float64(tick)*w.Cadence.Seconds()
	default:
		return s.Base
	}
}

// unit is a deterministic value in [0, 1) for one task, key and tick.
func unit(task, key string, tick int) float64 {
	h := fnv.New64a()
	h.Write([]byte(task))
	h.Write([]byte{0})
	h.Write([]byte(key))
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(tick))
	h.Write(n[:])
	return float64(h.Sum64()>>11) / float64(1<<53)
}
