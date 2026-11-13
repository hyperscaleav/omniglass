package node

import (
	"testing"
	"time"
)

// spinFor keeps the calling goroutine on CPU for d of wall time.
func spinFor(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); {
	}
}

// A run that spins and then blocks is charged its spin, not its block (#1870):
// the thread clock counts the time the kernel ran the run's thread, and a
// thread parked on a sleep (as on a device that has not answered) runs
// nothing. The spin's floor is only that some of it counted, because a
// loaded or throttled host can take the thread off CPU for most of the spin;
// the ceiling is the point, far under the block.
func TestARunsCPUTimeIsItsSpinNotItsBlock(t *testing.T) {
	if !threadClockReads {
		t.Skip("this platform has no per-thread CPU clock")
	}
	const spin, block = 50 * time.Millisecond, 300 * time.Millisecond
	cost := measureRun(func() {
		spinFor(spin)
		time.Sleep(block)
	})
	if !cost.cpuRead {
		t.Fatal("the thread clock did not read on a platform that has one")
	}
	if cost.took < spin+block {
		t.Errorf("took %v, want at least the spin and the block, %v", cost.took, spin+block)
	}
	if cost.cpu <= 0 || cost.cpu > spin+25*time.Millisecond {
		t.Errorf("cpu %v, want near the %v spin and far under the %v block", cost.cpu, spin, block)
	}
}

// Work a run hands to another goroutine runs on another thread, so the run's
// thread clock does not count it: the stated limit of the measure, held by a
// test so a change that starts counting it (or claims to) is seen.
func TestARunsCPUTimeLeavesOutWorkOnOtherGoroutines(t *testing.T) {
	if !threadClockReads {
		t.Skip("this platform has no per-thread CPU clock")
	}
	const spin = 50 * time.Millisecond
	cost := measureRun(func() {
		done := make(chan struct{})
		go func() { spinFor(spin); close(done) }()
		<-done
	})
	if cost.cpu > spin/2 {
		t.Errorf("cpu %v counts the helper goroutine's %v spin, which ran on another thread", cost.cpu, spin)
	}
}
