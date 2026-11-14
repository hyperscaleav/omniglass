//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// #1869: a node sizes its lane pool from the container's real cgroup limits.
// The unit tests read fixture files; this runs the node in a container Docker
// limits, so the kernel writes the cpu.max and memory.max the node reads, and
// asserts the count and source its boot log states. The node needs no server:
// it sizes its lanes before it claims.
func TestIntegrationNodeSizesItsLanesFromItsContainer(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the lab's integration tests run through make test-lab (they need the image make builds)")
	}
	image := labTestImage()
	for _, tc := range []struct {
		name, cpus, memory string
		want               []string
	}{
		// Half a CPU: 0.5 x 0.5 x (204.4 / 4.4) = 11 lanes, under memory's 16.
		{"cpu binds", "0.5", "64m", []string{"workers=11", "cpu.max=0.5", "memory.max=64MiB"}},
		// 32 MiB: 32 x 0.25 = 8 lanes, under four CPUs' 92.
		{"memory binds", "4", "32m", []string{"workers=8", "cpu.max=4", "memory.max=32MiB"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := fmt.Sprintf("omniglass-lanes-it-%d-%s", os.Getpid(), tc.cpus)
			t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
			// The node logs its lanes, then exits on the refused claim; the
			// timeout bounds a node that retried instead.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			out, _ := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", container,
				"--cpus", tc.cpus, "--memory", tc.memory, image,
				"node", "run", "--server", "http://127.0.0.1:9", "--name", "lanes-it", "--token", "unused").CombinedOutput()
			var line string
			for l := range strings.Lines(string(out)) {
				if strings.Contains(l, `msg="lane workers"`) {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("no lane workers line in the node's boot log:\n%s", out)
			}
			for _, want := range tc.want {
				if !strings.Contains(line, want) {
					t.Errorf("boot log %q does not state %q", strings.TrimSpace(line), want)
				}
			}
		})
	}
}
