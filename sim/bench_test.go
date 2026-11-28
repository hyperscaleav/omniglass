//go:build linux

package sim

import (
	"fmt"
	"net"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// thousandDevices spreads n devices across every family, each on its own
// loopback address.
func thousandDevices(b *testing.B, cat *spec.Catalog, n int) []DeviceSnapshot {
	b.Helper()
	var names []string
	for _, d := range cat.Definitions() {
		names = append(names, d.ID())
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
	snaps := thousandDevices(b, shippedCatalog(b), 1000)
	for i := 0; i < b.N; i++ {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		baseG := runtime.NumGoroutine()

		e := shippedEngine(b)
		e.SetPortMap(EphemeralPorts)
		start := time.Now()
		if err := e.Sync(snaps); err != nil {
			b.Fatalf("sync: %v", err)
		}
		startup := time.Since(start)

		// A device on the far end of the fleet answers.
		devs := e.Devices()
		for _, f := range devs[len(devs)-1].Faces() {
			if f.Protocol == "http" {
				if c, err := net.DialTimeout("tcp", f.Addr.String(), time.Second); err == nil {
					c.Close()
				}
				break
			}
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
