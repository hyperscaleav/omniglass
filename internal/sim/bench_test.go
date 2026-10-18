//go:build linux

package sim

import (
	"fmt"
	"net"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

const benchPortShift = 25000

// thousandDevices spreads n devices across every catalog profile, each on
// its own loopback address.
func thousandDevices(b *testing.B, store *profile.Store, n int) []DeviceSnapshot {
	b.Helper()
	names := make([]string, 0, len(store.Devices))
	for name := range store.Devices {
		names = append(names, name)
	}
	sort.Strings(names)
	snaps := make([]DeviceSnapshot, n)
	for i := range snaps {
		snaps[i] = DeviceSnapshot{
			DeviceID:    fmt.Sprintf("bench-%04d", i),
			Hostname:    fmt.Sprintf("bench-%04d", i),
			LoopbackIP:  fmt.Sprintf("127.40.%d.%d", i/250, i%250+1),
			ProfileName: names[i%len(names)],
			Version:     1,
		}
	}
	return snaps
}

// BenchmarkServeThousandDevices measures one simulator process serving a
// 1,000-device fleet: how long it takes to start, the goroutines and heap
// it holds while serving, and the goroutines left behind once it stops.
func BenchmarkServeThousandDevices(b *testing.B) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		b.Fatal(err)
	}
	snaps := thousandDevices(b, store, 1000)
	for i := 0; i < b.N; i++ {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		baseG := runtime.NumGoroutine()

		e := NewEngine(store, profiles.Templates())
		e.SetPortMap(PrivilegedPortShift(benchPortShift))
		start := time.Now()
		if err := e.Sync(snaps); err != nil {
			b.Fatalf("sync: %v", err)
		}
		startup := time.Since(start)

		// A device on the far end of the fleet answers.
		last := snaps[len(snaps)-1]
		c, err := net.DialTimeout("tcp", net.JoinHostPort(last.LoopbackIP, fmt.Sprint(80+benchPortShift)), time.Second)
		if err == nil {
			c.Close()
		}

		runtime.GC()
		var during runtime.MemStats
		runtime.ReadMemStats(&during)
		serving := runtime.NumGoroutine() - baseG

		e.StopAll()
		time.Sleep(200 * time.Millisecond) // let accept loops and tickers unwind
		leaked := runtime.NumGoroutine() - baseG

		b.ReportMetric(float64(startup.Milliseconds()), "startup-ms")
		b.ReportMetric(float64(serving), "goroutines")
		b.ReportMetric(float64(during.HeapAlloc-before.HeapAlloc)/(1<<20), "heap-MB")
		b.ReportMetric(float64(leaked), "leaked-goroutines")
	}
}
