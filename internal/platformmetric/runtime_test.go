package platformmetric

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

func TestProcessReadingsNameTheProcessKeys(t *testing.T) {
	t.Parallel()
	p := Process{Goroutines: 12, HeapBytes: 4 << 20, GCs: 3, LastGCPause: 1500 * time.Microsecond, FDs: 9}
	got := p.Readings()
	want := []Reading{
		{Key: writerclass.ProcessGoroutineCount, Value: 12},
		{Key: writerclass.ProcessHeapSize, Value: 4 << 20},
		{Key: writerclass.ProcessGCPause, Value: 1.5},
		{Key: writerclass.ProcessFDCount, Value: 9},
	}
	if len(got) != len(want) {
		t.Fatalf("readings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reading %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestProcessWithNoCollectionYetReadsNoPause(t *testing.T) {
	t.Parallel()
	got := Process{Goroutines: 2, HeapBytes: 1024, FDs: -1}.Readings()
	for _, r := range got {
		if r.Key == writerclass.ProcessGCPause {
			t.Fatalf("a process that never collected read a pause: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("readings = %v, want the goroutine count and heap size (no fd count where none is exposed)", got)
	}
}

func TestReadProcessReadsThisProcess(t *testing.T) {
	t.Parallel()
	runtime.GC()
	p := ReadProcess()
	if p.Goroutines < 1 {
		t.Errorf("goroutines = %d, want at least this one", p.Goroutines)
	}
	if p.HeapBytes == 0 {
		t.Error("heap size read zero")
	}
	if p.GCs < 1 {
		t.Errorf("collections = %d after a forced collection", p.GCs)
	}
	if p.LastGCPause < 0 {
		t.Errorf("last pause = %v", p.LastGCPause)
	}
	if p.CPU <= 0 {
		t.Errorf("cpu = %v, want the time this process has run", p.CPU)
	}
	if runtime.GOOS == "linux" && p.FDs < 3 {
		t.Errorf("fds = %d, want at least stdin, stdout and stderr on linux", p.FDs)
	}
}

func TestEveryReadingKeyIsAPlatformKey(t *testing.T) {
	t.Parallel()
	var rt Runtime
	rt.Readings(Process{CPU: time.Second})
	for _, r := range rt.Readings(Process{Goroutines: 1, HeapBytes: 1, GCs: 1, LastGCPause: time.Millisecond, CPU: 2 * time.Second, FDs: 3}) {
		if !writerclass.NodeSelf(r.Key) {
			t.Errorf("%s is not a declared node-self platform key", r.Key)
		}
	}
}

// TestRuntimeReadsCPUAsTheDeltaOncePrimed: the first read has no previous to
// subtract, so it reads no cpu; each read after reads what the process spent
// since the one before, and a total that went backwards reads zero.
func TestRuntimeReadsCPUAsTheDeltaOncePrimed(t *testing.T) {
	t.Parallel()
	var rt Runtime
	cpu := func(rs []Reading) (float64, bool) {
		for _, r := range rs {
			if r.Key == writerclass.ProcessCPUSeconds {
				return r.Value, true
			}
		}
		return 0, false
	}
	if v, ok := cpu(rt.Readings(Process{CPU: 10 * time.Second, FDs: -1})); ok {
		t.Fatalf("the first read reported cpu %v with nothing to subtract", v)
	}
	if v, ok := cpu(rt.Readings(Process{CPU: 12500 * time.Millisecond, FDs: -1})); !ok || v != 2.5 {
		t.Fatalf("cpu = %v (present %v), want the 2.5 s spent since the last read", v, ok)
	}
	if v, ok := cpu(rt.Readings(Process{CPU: time.Second, FDs: -1})); !ok || v != 0 {
		t.Fatalf("cpu = %v (present %v) after the total went backwards, want 0", v, ok)
	}
}

// TestRuntimeSamplerReadsEveryKeyNonnegative: the sampler the server and the
// node share reads every generic process key off this process, none negative.
func TestRuntimeSamplerReadsEveryKeyNonnegative(t *testing.T) {
	t.Parallel()
	runtime.GC()
	r := NewRecorder(time.Now)
	s := RuntimeSampler(ReadProcess)
	s(context.Background(), r)
	s(context.Background(), r)
	got := map[string]float64{}
	for _, row := range r.Flush("c") {
		got[row.Key] = row.Value
	}
	want := []string{writerclass.ProcessGoroutineCount, writerclass.ProcessHeapSize, writerclass.ProcessGCPause,
		writerclass.ProcessCPUSeconds}
	if runtime.GOOS == "linux" {
		want = append(want, writerclass.ProcessFDCount)
	}
	for _, k := range want {
		v, ok := got[k]
		if !ok {
			t.Errorf("%s not read (read %v)", k, got)
			continue
		}
		if v < 0 {
			t.Errorf("%s = %v, want nonnegative", k, v)
		}
	}
}
