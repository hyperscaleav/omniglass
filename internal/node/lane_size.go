package node

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// A node sizes its lane pool from what it may use (#1869). A lane is one
// request in flight to one endpoint, and a request mostly waits on its
// device: a run that costs a few milliseconds of CPU waits a couple of
// hundred on the answer. So a CPU carries many lanes, and how many is the
// ratio of a run's whole time to its CPU time, not the CPU count. Memory
// bounds it too, each lane holding its request and answer, and the pool is
// clamped either side.

// MinLaneWorkers and MaxLaneWorkers clamp an automatic lane count: a node on
// a sliver of a CPU still runs a few endpoints at once, and one on a large
// host does not open more connections than a fleet's devices welcome.
const (
	MinLaneWorkers = 4
	MaxLaneWorkers = 256
)

// laneCPUShare is the share of the node's CPU its lanes are sized to fill,
// leaving the rest to its reports, its worklist and its sends.
const laneCPUShare = 0.5

// laneMemoryShare is the share of the node's memory its lanes may hold.
const laneMemoryShare = 0.25

// LaneCost is what one lane's run costs: the CPU it burns, the time it waits
// on its device, and the memory a lane holds.
type LaneCost struct {
	CPUPerRun     time.Duration
	WaitPerRun    time.Duration
	MemoryPerLane int64
}

// DefaultLaneCost is a typical run: 4.4 ms of CPU, from the 3,000-endpoint
// lab's busiest node (81 polls a second on 0.36 of a CPU, its simulator
// included, so an overestimate), waiting 200 ms on a device's answer, and a
// lane holding at most 1 MiB. That is about 23 lanes a CPU.
var DefaultLaneCost = LaneCost{CPUPerRun: 4400 * time.Microsecond, WaitPerRun: 200 * time.Millisecond, MemoryPerLane: 1 << 20}

// Limits are the CPUs and memory a node may use, and where each was read.
// Zero is unknown.
type Limits struct {
	CPUs        float64
	MemoryBytes int64
	CPUFrom     string
	MemoryFrom  string
}

// String names each limit and its source, for the node's boot log.
func (l Limits) String() string {
	cpu := "cpus unknown"
	if l.CPUs > 0 {
		cpu = sourced(l.CPUFrom, "cpus") + "=" + strconv.FormatFloat(l.CPUs, 'g', 4, 64)
	}
	mem := "memory unknown"
	if l.MemoryBytes > 0 {
		mem = sourced(l.MemoryFrom, "memory") + "=" + strconv.FormatInt(l.MemoryBytes>>20, 10) + "MiB"
	}
	return cpu + ", " + mem
}

// sourced names a cgroup file as itself and the host's figure as "host what".
func sourced(from, what string) string {
	if from == "host" {
		return "host " + what
	}
	return from
}

// laneWorkersFor is how many lanes fit the limits at the cost: the CPU bound
// (the lanes' share of the CPUs, times the runs a CPU keeps in flight while
// they wait) or the memory bound, whichever is tighter, clamped. An unknown
// limit does not bind; with neither known the pool is the floor.
func laneWorkersFor(l Limits, c LaneCost) int {
	bound := -1.0
	if l.CPUs > 0 && c.CPUPerRun > 0 {
		bound = l.CPUs * laneCPUShare * float64(c.CPUPerRun+c.WaitPerRun) / float64(c.CPUPerRun)
	}
	if l.MemoryBytes > 0 && c.MemoryPerLane > 0 {
		m := float64(l.MemoryBytes) * laneMemoryShare / float64(c.MemoryPerLane)
		if bound < 0 || m < bound {
			bound = m
		}
	}
	return min(max(int(bound), MinLaneWorkers), MaxLaneWorkers)
}

// cgroupDir is the process's cgroup v2 path from /proc/self/cgroup, relative
// to the cgroup mount: "." for the root, which is what a container in its own
// cgroup namespace sees.
func cgroupDir(procSelfCgroup string) string {
	for line := range strings.Lines(procSelfCgroup) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "0::"); ok {
			if p = strings.Trim(p, "/"); p != "" {
				return path.Clean(p)
			}
			return "."
		}
	}
	return "."
}

// cgroupLimits reads cpu.max and memory.max in dir and every parent up to the
// mount's root, keeping the tightest of each: a parent's limit binds its
// children. "max", a missing file or one that does not parse is no limit.
func cgroupLimits(fsys fs.FS, dir string) Limits {
	var l Limits
	for d := dir; ; d = path.Dir(d) {
		if cpus, ok := cpuMax(fsys, path.Join(d, "cpu.max")); ok && (l.CPUs == 0 || cpus < l.CPUs) {
			l.CPUs, l.CPUFrom = cpus, "cpu.max"
		}
		if mem, ok := memoryMax(fsys, path.Join(d, "memory.max")); ok && (l.MemoryBytes == 0 || mem < l.MemoryBytes) {
			l.MemoryBytes, l.MemoryFrom = mem, "memory.max"
		}
		if d == "." || d == "/" {
			return l
		}
	}
}

// cpuMax reads a cpu.max, "<quota> <period>" in microseconds, as CPUs.
func cpuMax(fsys fs.FS, name string) (float64, bool) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(b))
	if len(f) != 2 {
		return 0, false
	}
	quota, err1 := strconv.ParseFloat(f[0], 64)
	period, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || quota <= 0 || period <= 0 {
		return 0, false
	}
	return quota / period, true
}

// memoryMax reads a memory.max, bytes or "max".
func memoryMax(fsys fs.FS, name string) (int64, bool) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// memTotal reads MemTotal from /proc/meminfo, in bytes; zero if absent.
func memTotal(meminfo string) int64 {
	for line := range strings.Lines(meminfo) {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			f := strings.Fields(rest)
			if len(f) == 0 {
				return 0
			}
			kb, err := strconv.ParseInt(f[0], 10, 64)
			if err != nil {
				return 0
			}
			return kb << 10
		}
	}
	return 0
}

// withHost takes each limit as the tighter of the cgroup's and the host's: a
// quota above the host's capacity buys nothing, and a limit the cgroup leaves
// open is the host's. Each keeps the source of the figure that binds.
func withHost(cg, host Limits) Limits {
	if cg.CPUs == 0 || (host.CPUs > 0 && host.CPUs < cg.CPUs) {
		cg.CPUs, cg.CPUFrom = host.CPUs, host.CPUFrom
	}
	if cg.MemoryBytes == 0 || (host.MemoryBytes > 0 && host.MemoryBytes < cg.MemoryBytes) {
		cg.MemoryBytes, cg.MemoryFrom = host.MemoryBytes, host.MemoryFrom
	}
	return cg
}

// hostCPUs is the CPUs the runtime schedules on: the machine's count, or
// GOMAXPROCS where lower, which carries a cgroup v1 CPU limit the v2 reader
// does not see and an operator's own GOMAXPROCS.
func hostCPUs(numCPU, maxProcs int) (float64, string) {
	if maxProcs > 0 && maxProcs < numCPU {
		return float64(maxProcs), "gomaxprocs"
	}
	return float64(numCPU), "host"
}

// hostLimits are the CPUs the process may schedule on and the host's memory.
func hostLimits() Limits {
	var l Limits
	l.CPUs, l.CPUFrom = hostCPUs(runtime.NumCPU(), runtime.GOMAXPROCS(0))
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		if m := memTotal(string(b)); m > 0 {
			l.MemoryBytes, l.MemoryFrom = m, "host"
		}
	}
	return l
}

// resolveLaneWorkers is the node's lane count: the operator's when set, else
// auto's, and where it came from.
func resolveLaneWorkers(set int, auto func() (int, string)) (int, string) {
	if set > 0 {
		return set, "set"
	}
	return auto()
}

// AutoLaneWorkers sizes the lane pool for the machine the node runs on: its
// cgroup's limits where it has them, else the host's, at DefaultLaneCost. The
// second value says what it read, for the boot log.
func AutoLaneWorkers() (int, string) {
	dir := "."
	if b, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		dir = cgroupDir(string(b))
	}
	l := withHost(cgroupLimits(os.DirFS("/sys/fs/cgroup"), dir), hostLimits())
	return laneWorkersFor(l, DefaultLaneCost), fmt.Sprintf("auto: %s", l)
}
