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

// A probe runs on an interval of its own and a node runs its tasks on a pool
// of workers (#1634): an interval or a pool of none would run nothing, so
// node run refuses either before it claims anything, as it does a worker
// count from the environment that is not a number.
func TestNodeRunRefusesAProbeIntervalOrAWorkerPoolOfNone(t *testing.T) {
	for _, tc := range []struct {
		args []string
		env  string
		want string
	}{
		{[]string{"--probe-every", "0s"}, "", "--probe-every"},
		{[]string{"--poll-workers", "0"}, "", "--poll-workers"},
		{nil, "many", "OMNIGLASS_POLL_WORKERS"},
		{nil, "0", "--poll-workers"},
	} {
		t.Setenv("OMNIGLASS_POLL_WORKERS", tc.env)
		root := Root("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"node", "run", "--server", "http://127.0.0.1:1", "--name", "edge", "--token", "t"}, tc.args...))
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("node run %v (OMNIGLASS_POLL_WORKERS=%q): %v, want a refusal naming %s", tc.args, tc.env, err, tc.want)
		}
	}
}
