// Package platformmetric reads the figures Omniglass reports about itself
// (#1094): the platform metrics a process lands on its own platform component.
// The read is at the edge, one sampler of the Go runtime that the node and the
// server share, and what it reads turns into readings by a pure step, so the
// keys and units are unit-tested without a runtime to read.
package platformmetric

import (
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Reading is one platform metric observation: a declared platform key and its
// value in the key's unit.
type Reading struct {
	Key   string
	Value float64
}

// Process is what one Go process costs at a moment: its goroutines, the bytes
// its heap's live and not yet swept objects hold, how many collections it has
// run, and the most recent collection's stop-the-world pause.
type Process struct {
	Goroutines  int
	HeapBytes   uint64
	GCs         int64
	LastGCPause time.Duration
}

// heapObjects is the runtime/metrics name of the heap's object bytes.
const heapObjects = "/memory/classes/heap/objects:bytes"

// ReadProcess reads this process now. It does not stop the world: the heap
// figure comes from runtime/metrics and the pause from the collector's own
// record.
func ReadProcess() Process {
	s := []metrics.Sample{{Name: heapObjects}}
	metrics.Read(s)
	var heap uint64
	if s[0].Value.Kind() == metrics.KindUint64 {
		heap = s[0].Value.Uint64()
	}
	var gc debug.GCStats
	debug.ReadGCStats(&gc)
	var pause time.Duration
	if len(gc.Pause) > 0 {
		pause = gc.Pause[0]
	}
	return Process{Goroutines: runtime.NumGoroutine(), HeapBytes: heap, GCs: gc.NumGC, LastGCPause: pause}
}

// Readings are p's platform metrics: process-goroutine-count,
// process-heap-size in bytes, and process-gc-pause in milliseconds. A process
// that has not collected yet has no pause to report, so it reads none rather
// than a zero that never happened.
func (p Process) Readings() []Reading {
	out := []Reading{
		{Key: writerclass.ProcessGoroutineCount, Value: float64(p.Goroutines)},
		{Key: writerclass.ProcessHeapSize, Value: float64(p.HeapBytes)},
	}
	if p.GCs > 0 {
		out = append(out, Reading{Key: writerclass.ProcessGCPause, Value: Millis(p.LastGCPause)})
	}
	return out
}

// Millis is d in milliseconds, the unit every platform duration key reads in.
func Millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
