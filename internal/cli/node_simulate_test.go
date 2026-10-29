package cli

import (
	"bytes"
	"strings"
	"testing"
)

// The simulator's own flags ride on node run only beside --simulate: alone
// they would be silently ignored, and an operator who asked for impairments
// would get a node that never applies any.
func TestNodeRunRefusesSimulatorFlagsWithoutSimulate(t *testing.T) {
	for _, flag := range [][]string{
		{"--impairments"},
		{"--port-offset", "10000"},
		{"--poll", "2s"},
	} {
		root := Root("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"node", "run", "--server", "http://127.0.0.1:1", "--name", "edge", "--token", "t"}, flag...))
		err := root.Execute()
		if err == nil {
			t.Errorf("node run %s without --simulate: no error, want a refusal", flag[0])
			continue
		}
		if !strings.Contains(err.Error(), flag[0]) || !strings.Contains(err.Error(), "--simulate") {
			t.Errorf("node run %s without --simulate: %v, want a refusal naming %s and --simulate", flag[0], err, flag[0])
		}
	}
}

// --once is one collection cycle; a simulator that starts and stops with it
// would serve nothing the cycle could reach, so the pair is refused.
func TestNodeRunRefusesSimulateWithOnce(t *testing.T) {
	root := Root("test")
	root.SetArgs([]string{"node", "run", "--server", "http://127.0.0.1:1", "--name", "edge", "--token", "t", "--simulate", "--once"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--once") {
		t.Fatalf("node run --simulate --once: %v, want a refusal naming --once", err)
	}
}

// A probe interval below the heartbeat is refused when it is set (#1373): the
// heartbeat is the granularity the node runs at, so the node could not keep
// it. One at the heartbeat, or above, is the node's to keep.
func TestNodeRunRefusesAProbeIntervalBelowTheHeartbeat(t *testing.T) {
	root := Root("test")
	root.SetArgs([]string{"node", "run", "--server", "http://127.0.0.1:1", "--name", "edge", "--token", "t",
		"--heartbeat", "10s", "--probe-every", "5s"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--probe-every") || !strings.Contains(err.Error(), "--heartbeat") {
		t.Fatalf("node run --heartbeat 10s --probe-every 5s: %v, want a refusal naming both", err)
	}
}
