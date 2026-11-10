// Package platformmetric reads the figures Omniglass reports about itself
// (#1094): the platform metrics a process lands on its own platform component.
// The read is at the edge, one sampler of the Go runtime that the node and the
// server share, and what it reads turns into readings by a pure step, so the
// keys and units are unit-tested without a runtime to read.
package platformmetric

import (
	"context"
	"os"
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
// run, the most recent collection's stop-the-world pause, the CPU time it has
// spent since it started, and the file descriptors it holds open (-1 where
// the platform does not expose them).
type Process struct {
	Goroutines  int
	HeapBytes   uint64
	GCs         int64
	LastGCPause time.Duration
	CPU         time.Duration
	FDs         int
}

// The runtime/metrics names ReadProcess reads: the heap's object bytes, and
// the CPU time the runtime has accounted, total and idle, so the time spent
// is their difference.
const (
	heapObjects = "/memory/classes/heap/objects:bytes"
	cpuTotal    = "/cpu/classes/total:cpu-seconds"
	cpuIdle     = "/cpu/classes/idle:cpu-seconds"
)

// fdDir lists a Linux process's open file descriptors, one entry each.
const fdDir = "/proc/self/fd"

// ReadProcess reads this process now. It does not stop the world: the heap
// figure comes from runtime/metrics and the pause from the collector's own
// record.
func ReadProcess() Process {
	s := []metrics.Sample{{Name: heapObjects}, {Name: cpuTotal}, {Name: cpuIdle}}
	metrics.Read(s)
	var heap uint64
	if s[0].Value.Kind() == metrics.KindUint64 {
		heap = s[0].Value.Uint64()
	}
	var cpu float64
	if s[1].Value.Kind() == metrics.KindFloat64 && s[2].Value.Kind() == metrics.KindFloat64 {
		cpu = max(s[1].Value.Float64()-s[2].Value.Float64(), 0)
	}
	var gc debug.GCStats
	debug.ReadGCStats(&gc)
	var pause time.Duration
	if len(gc.Pause) > 0 {
		pause = gc.Pause[0]
	}
	return Process{Goroutines: runtime.NumGoroutine(), HeapBytes: heap, GCs: gc.NumGC, LastGCPause: pause,
		CPU: time.Duration(cpu * float64(time.Second)), FDs: openFDs()}
}

// openFDs counts this process's open file descriptors, or -1 where the
// platform has no /proc to count them in.
func openFDs() int {
	es, err := os.ReadDir(fdDir)
	if err != nil {
		return -1
	}
	return len(es)
}

// Readings are p's platform metrics as it stands: process-goroutine-count,
// process-heap-size in bytes, process-gc-pause in milliseconds and
// process-fd-count. A process that has not collected yet has no pause to
// report, and one whose platform exposes no descriptors no fd count, so each
// reads none rather than a zero that never happened. CPU time is a delta
// between two reads, so Runtime reads it.
func (p Process) Readings() []Reading {
	out := []Reading{
		{Key: writerclass.ProcessGoroutineCount, Value: float64(p.Goroutines)},
		{Key: writerclass.ProcessHeapSize, Value: float64(p.HeapBytes)},
	}
	if p.GCs > 0 {
		out = append(out, Reading{Key: writerclass.ProcessGCPause, Value: Millis(p.LastGCPause)})
	}
	if p.FDs >= 0 {
		out = append(out, Reading{Key: writerclass.ProcessFDCount, Value: float64(p.FDs)})
	}
	return out
}

// Runtime is the one runtime sampler the server and the node share (#1628):
// it reads a Process into its readings and adds process-cpu-seconds, the CPU
// time spent since the read before. The zero value is ready; its first read
// has nothing to subtract, so it reads no CPU time.
type Runtime struct {
	prev   time.Duration
	primed bool
}

// Readings are p's readings and, once primed, the CPU seconds p spent since
// the previous read. A total that went backwards reads zero, never less.
func (rt *Runtime) Readings(p Process) []Reading {
	out := p.Readings()
	if rt.primed {
		out = append(out, Reading{Key: writerclass.ProcessCPUSeconds, Value: max(p.CPU-rt.prev, 0).Seconds()})
	}
	rt.prev, rt.primed = p.CPU, true
	return out
}

// RuntimeSampler gauges the process read by read each window, CPU time as
// the window's own: the server's runtime figures, on the sampler the node
// reports its own through.
func RuntimeSampler(read func() Process) Sampler {
	var rt Runtime
	rt.Readings(read())
	return func(_ context.Context, r *Recorder) {
		for _, rd := range rt.Readings(read()) {
			r.Gauge(rd.Key, "", rd.Value)
		}
	}
}

// Millis is d in milliseconds, the unit every platform duration key reads in.
func Millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
