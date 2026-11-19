//go:build !windows

package lab

import (
	"os"
	"testing"
)

// The current process is alive with its own start time, and the same pid
// with any other start time reads as gone (a reused pid).
func TestProcessAliveJudgesByPIDAndStart(t *testing.T) {
	pid := os.Getpid()
	start := currentStart()
	if start == "" {
		t.Skip("this platform does not expose a process start time")
	}
	if !processAlive(pid, start) {
		t.Errorf("the current process reads as gone with its own start %q", start)
	}
	if processAlive(pid, start+"0") {
		t.Errorf("the current pid reads as alive with a wrong start time")
	}
	if !processAlive(pid, "") {
		t.Errorf("an empty start asks about the pid alone, and the current pid runs")
	}
	if processAlive(0, start) || processAlive(-1, "") {
		t.Errorf("a non-positive pid reads as alive")
	}
}
