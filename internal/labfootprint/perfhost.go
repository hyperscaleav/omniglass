package labfootprint

import (
	"bufio"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ReadFingerprint is the host this process runs on: the CPU model and memory
// from /proc (empty or 0 where /proc does not say), the logical cores and the
// Go version. A lab on a local Docker daemon runs on this host, so its runs
// carry this fingerprint; a lab on a remote daemon carries none.
func ReadFingerprint() Fingerprint {
	f := Fingerprint{Cores: runtime.NumCPU(), Go: runtime.Version()}
	if r, err := os.Open("/proc/cpuinfo"); err == nil {
		f.CPU = parseCPUModel(r)
		r.Close()
	}
	if r, err := os.Open("/proc/meminfo"); err == nil {
		f.MemoryGiB = parseMemoryGiB(r)
		r.Close()
	}
	return f
}

// parseCPUModel is the first "model name" in a /proc/cpuinfo.
func parseCPUModel(r io.Reader) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// parseMemoryGiB is a /proc/meminfo's MemTotal, rounded to whole GiB.
func parseMemoryGiB(r io.Reader) int {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok || key != "MemTotal" {
			continue
		}
		kb, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(val), " kB"), 64)
		if err != nil {
			return 0
		}
		return int(math.Round(kb / (1 << 20)))
	}
	return 0
}
