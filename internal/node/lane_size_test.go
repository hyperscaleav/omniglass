package node

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const mib = 1 << 20

// testLaneCost is a run of 5 ms of CPU waiting 195 ms on its device: a CPU
// keeps 40 such runs in flight, and the lanes take half of it, so 20 lanes a
// CPU before memory or the clamp bind.
var testLaneCost = LaneCost{CPUPerRun: 5 * time.Millisecond, WaitPerRun: 195 * time.Millisecond, MemoryPerLane: mib}

// #1869: a node sizes its lane pool from what it may use, weighing how long a
// run waits on its device against the CPU it costs, and the memory it holds,
// inside the clamp.
func TestLaneWorkersForSizesThePoolFromTheNodesLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		limits Limits
		want   int
	}{
		{"a 2-core 512 MiB container", Limits{CPUs: 2, MemoryBytes: 512 * mib}, 40},
		{"a 4-core 8 GiB host", Limits{CPUs: 4, MemoryBytes: 8 << 30}, 80},
		{"a 32-core host, past the ceiling", Limits{CPUs: 32, MemoryBytes: 64 << 30}, MaxLaneWorkers},
		{"a tenth of a core, at the floor", Limits{CPUs: 0.1, MemoryBytes: 8 << 30}, MinLaneWorkers},
		{"4 cores in 64 MiB, memory binds", Limits{CPUs: 4, MemoryBytes: 64 * mib}, 16},
		{"a fractional limit", Limits{CPUs: 1.5, MemoryBytes: 8 << 30}, 30},
		{"limits unknown", Limits{}, MinLaneWorkers},
	} {
		if got := laneWorkersFor(tc.limits, testLaneCost); got != tc.want {
			t.Errorf("%s: laneWorkersFor(%+v) = %d, want %d", tc.name, tc.limits, got, tc.want)
		}
	}
}

// The product's own cost gives a small node more lanes than one a request's
// wait would leave idle, and never leaves the clamp.
func TestLaneWorkersForTheDefaultCostStaysInTheClamp(t *testing.T) {
	t.Parallel()
	for _, l := range []Limits{
		{CPUs: 2, MemoryBytes: 512 * mib},
		{CPUs: 4, MemoryBytes: 8 << 30},
		{CPUs: 32, MemoryBytes: 64 << 30},
	} {
		n := laneWorkersFor(l, DefaultLaneCost)
		if n < MinLaneWorkers || n > MaxLaneWorkers {
			t.Errorf("laneWorkersFor(%+v) = %d, outside [%d, %d]", l, n, MinLaneWorkers, MaxLaneWorkers)
		}
		if l.CPUs >= 2 && n <= int(l.CPUs) {
			t.Errorf("laneWorkersFor(%+v) = %d: a lane mostly waits, so a CPU carries more than one", l, n)
		}
	}
}

// The cgroup reader takes the container's cpu.max and memory.max, the
// tightest of its cgroup and every parent's, and reads "max" as no limit.
func TestCgroupLimitsReadsCPUMaxAndMemoryMax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		fsys    fstest.MapFS
		dir     string
		cpus    float64
		memory  int64
		cpuFrom string
	}{
		{"no limit", fstest.MapFS{"cpu.max": {Data: []byte("max 100000\n")}, "memory.max": {Data: []byte("max\n")}}, ".", 0, 0, ""},
		{"2 CPUs and 512 MiB", fstest.MapFS{"cpu.max": {Data: []byte("200000 100000\n")}, "memory.max": {Data: []byte("536870912\n")}}, ".", 2, 512 * mib, "cpu.max"},
		{"half a CPU", fstest.MapFS{"cpu.max": {Data: []byte("50000 100000\n")}}, ".", 0.5, 0, "cpu.max"},
		{"no cgroup files, the host's root", fstest.MapFS{}, ".", 0, 0, ""},
		{"a parent's limit binds its child", fstest.MapFS{
			"system.slice/cpu.max":               {Data: []byte("100000 100000\n")},
			"system.slice/memory.max":            {Data: []byte("max\n")},
			"system.slice/og.service/cpu.max":    {Data: []byte("400000 100000\n")},
			"system.slice/og.service/memory.max": {Data: []byte("268435456\n")},
		}, "system.slice/og.service", 1, 256 * mib, "cpu.max"},
		{"an unreadable file is no limit", fstest.MapFS{"cpu.max": {Data: []byte("lots\n")}, "memory.max": {Data: []byte("plenty\n")}}, ".", 0, 0, ""},
	} {
		got := cgroupLimits(tc.fsys, tc.dir)
		if got.CPUs != tc.cpus || got.MemoryBytes != tc.memory || got.CPUFrom != tc.cpuFrom {
			t.Errorf("%s: cgroupLimits = %+v, want %v CPUs from %q and %d bytes", tc.name, got, tc.cpus, tc.cpuFrom, tc.memory)
		}
	}
}

// /proc/self/cgroup names the process's cgroup v2 path, relative to the
// cgroup mount; in a container's own cgroup namespace it is the root.
func TestCgroupDirReadsTheV2Path(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"0::/\n":                        ".",
		"0::/system.slice/og.service\n": "system.slice/og.service",
		"12:cpu:/x\n0::/a/b\n":          "a/b",
		"12:cpu:/docker/abc\n":          ".",
		"":                              ".",
	} {
		if got := cgroupDir(in); got != want {
			t.Errorf("cgroupDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMemTotalReadsMeminfo(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int64{
		"MemTotal:       16384000 kB\nMemFree:  1 kB\n": 16384000 * 1024,
		"MemFree: 1 kB\n":     0,
		"MemTotal: lots kB\n": 0,
	} {
		if got := memTotal(in); got != want {
			t.Errorf("memTotal(%q) = %d, want %d", in, got, want)
		}
	}
}

// Each limit is the tighter of the cgroup's and the host's, so a quota set
// above the host's CPUs or memory does not size the pool for capacity the
// node cannot have; one the cgroup leaves open is the host's. The source says
// which each came from.
func TestLimitsTakeTheTighterOfTheCgroupsAndTheHosts(t *testing.T) {
	t.Parallel()
	host := Limits{CPUs: 32, MemoryBytes: 64 << 30, CPUFrom: "host", MemoryFrom: "host"}
	open := withHost(Limits{}, host)
	if open != host {
		t.Errorf("no cgroup limit = %+v, want the host's %+v", open, host)
	}
	got := withHost(Limits{CPUs: 2, CPUFrom: "cpu.max"}, host)
	if got.CPUs != 2 || got.CPUFrom != "cpu.max" || got.MemoryBytes != 64<<30 || got.MemoryFrom != "host" {
		t.Errorf("a cpu.max limit alone = %+v, want 2 CPUs from cpu.max and the host's memory", got)
	}
	for _, want := range []string{"cpu.max=2", "host memory=65536MiB"} {
		if !strings.Contains(got.String(), want) {
			t.Errorf("source %q does not name %q", got.String(), want)
		}
	}
	over := withHost(Limits{CPUs: 64, MemoryBytes: 1 << 40, CPUFrom: "cpu.max", MemoryFrom: "memory.max"}, host)
	if over != host {
		t.Errorf("cgroup limits above the host = %+v, want the host's %+v", over, host)
	}
}

// The host's CPUs are the ones the runtime schedules on: GOMAXPROCS, which
// honors a cgroup's CPU limit the v2 reader does not see (cgroup v1) and an
// operator's GOMAXPROCS, where it is lower than the machine's count.
func TestHostCPUsAreTheRuntimesSchedulableCount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		numCPU, maxProcs int
		want             float64
		from             string
	}{
		{12, 12, 12, "host"},
		{12, 2, 2, "gomaxprocs"},
		{4, 8, 4, "host"},
	} {
		if got, from := hostCPUs(tc.numCPU, tc.maxProcs); got != tc.want || from != tc.from {
			t.Errorf("hostCPUs(%d, %d) = %v from %q, want %v from %q", tc.numCPU, tc.maxProcs, got, from, tc.want, tc.from)
		}
	}
	if s := (Limits{CPUs: 2, CPUFrom: "gomaxprocs"}).String(); !strings.Contains(s, "gomaxprocs=2") {
		t.Errorf("source %q does not name gomaxprocs=2", s)
	}
}

// An operator's count stands; zero is auto, sized from the node's limits, and
// the source says which the node took.
func TestResolveLaneWorkersTakesTheOperatorsCountOverAuto(t *testing.T) {
	t.Parallel()
	auto := func() (int, string) { return 46, "auto: cpu.max=2, memory.max=512MiB" }
	if n, from := resolveLaneWorkers(8, auto); n != 8 || from != "set" {
		t.Errorf("resolveLaneWorkers(8) = %d from %q, want 8 from set", n, from)
	}
	if n, from := resolveLaneWorkers(0, auto); n != 46 || from != "auto: cpu.max=2, memory.max=512MiB" {
		t.Errorf("resolveLaneWorkers(0) = %d from %q, want auto's 46 and its source", n, from)
	}
}
