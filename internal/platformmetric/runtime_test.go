package platformmetric

import (
	"runtime"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

func TestProcessReadingsNameTheProcessKeys(t *testing.T) {
	t.Parallel()
	p := Process{Goroutines: 12, HeapBytes: 4 << 20, GCs: 3, LastGCPause: 1500 * time.Microsecond}
	got := p.Readings()
	want := []Reading{
		{Key: writerclass.ProcessGoroutineCount, Value: 12},
		{Key: writerclass.ProcessHeapSize, Value: 4 << 20},
		{Key: writerclass.ProcessGCPause, Value: 1.5},
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
	got := Process{Goroutines: 2, HeapBytes: 1024}.Readings()
	for _, r := range got {
		if r.Key == writerclass.ProcessGCPause {
			t.Fatalf("a process that never collected read a pause: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("readings = %v, want the goroutine count and heap size", got)
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
}

func TestEveryReadingKeyIsAPlatformKey(t *testing.T) {
	t.Parallel()
	for _, r := range (Process{Goroutines: 1, HeapBytes: 1, GCs: 1, LastGCPause: time.Millisecond}).Readings() {
		if !writerclass.NodeSelf(r.Key) {
			t.Errorf("%s is not a declared node-self platform key", r.Key)
		}
	}
}
