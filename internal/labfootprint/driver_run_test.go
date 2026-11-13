package labfootprint

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/node"
)

func TestALabsLabelsReadBackFromItsReadyFile(t *testing.T) {
	got, err := parseLabLabels("com.docker.compose.project=p omniglass.lab=p omniglass.lab.pid=42")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"com.docker.compose.project": "p", "omniglass.lab": "p", "omniglass.lab.pid": "42"}
	if !maps.Equal(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	if _, err := parseLabLabels("a=b nokey"); err == nil {
		t.Fatal("a label with no value is not one the lab wrote, want an error")
	}
	if _, err := parseLabLabels(""); err == nil {
		t.Fatal("a lab with no labels could not be torn down, want an error")
	}
}

// sweepEnv is what a ready file gives a sweep.
func sweepEnv() map[string]string {
	return map[string]string{
		"LAB_PROJECT": "og-sweep", "LAB_NETWORK": "og-sweep_default",
		"LAB_SERVER_URL": "http://server:8080", "LAB_IMAGE": "omniglass:test",
		"LAB_LABELS": "com.docker.compose.project=og-sweep omniglass.lab=og-sweep",
	}
}

// The node a driver is measured on runs with no simulator in its process,
// so its cgroup holds only the node's own work; it otherwise runs as the
// lab's nodes do: root in its own container, for the pinger and NET_ADMIN.
func TestASweepNodeRunsWithoutTheSimulator(t *testing.T) {
	s, err := sweepNodeService(sweepEnv(), "sweep-http-json", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if s.Image != "omniglass:test" || s.User != "0:0" || !slices.Equal(s.CapAdd, []string{"NET_ADMIN"}) {
		t.Fatalf("node spec = %+v", s)
	}
	if s.Sysctls["net.ipv4.ping_group_range"] != "0 65535" {
		t.Fatalf("sysctls = %v, want the pinger's group range", s.Sysctls)
	}
	if s.Cmd[0] != "node" || s.Cmd[1] != "run" || slices.Contains(s.Cmd, "--simulate") || slices.Contains(s.Cmd, "--impairments") {
		t.Fatalf("cmd = %v, want node run without the simulator or impairments", s.Cmd)
	}
	for _, e := range []string{"OMNIGLASS_SERVER=http://server:8080", "OMNIGLASS_NODE_NAME=sweep-http-json", "OMNIGLASS_NODE_TOKEN=tok"} {
		if !slices.Contains(s.Env, e) {
			t.Fatalf("env %v lacks %s", s.Env, e)
		}
	}
	if s.NetworkOf != "" || s.Publish != nil {
		t.Fatalf("the node joins the lab network and publishes nothing: %+v", s)
	}
	if s.Labels["com.docker.compose.project"] != "og-sweep" || s.Labels["omniglass.lab"] != "og-sweep" {
		t.Fatalf("labels = %v, want the lab's own, so its teardown removes the node", s.Labels)
	}
	if !strings.HasPrefix(s.Container, "og-sweep-") {
		t.Fatalf("container = %q, want it named under the project", s.Container)
	}
}

// The simulator runs beside the node, in the node's network namespace, so
// it serves the simulated components on the loopback the node polls.
func TestASweepSimulatorSharesTheNodesLoopback(t *testing.T) {
	s, err := sweepSimService(sweepEnv(), "sweep-http-json", "tok", "node123")
	if err != nil {
		t.Fatal(err)
	}
	if s.NetworkOf != "node123" {
		t.Fatalf("network of = %q, want the node's container", s.NetworkOf)
	}
	if s.Cmd[0] != "sim" || s.Cmd[1] != "run" {
		t.Fatalf("cmd = %v, want sim run", s.Cmd)
	}
	if s.User != "0:0" {
		t.Fatalf("user = %q, want root, to bind the simulated ports below 1024", s.User)
	}
	if !slices.Contains(s.Env, "OMNIGLASS_NODE_NAME=sweep-http-json") || !slices.Contains(s.Env, "OMNIGLASS_NODE_TOKEN=tok") {
		t.Fatalf("env = %v, want the node's identity", s.Env)
	}
	if s.Labels["omniglass.lab"] != "og-sweep" {
		t.Fatalf("labels = %v, want the lab's own", s.Labels)
	}
	n, _ := sweepNodeService(sweepEnv(), "sweep-http-json", "tok")
	if s.Container == n.Container {
		t.Fatal("the simulator and the node need containers of their own names")
	}
}

func TestASweepNeedsTheLabsReadyVariables(t *testing.T) {
	for _, k := range []string{"LAB_PROJECT", "LAB_NETWORK", "LAB_SERVER_URL", "LAB_IMAGE", "LAB_LABELS"} {
		env := sweepEnv()
		delete(env, k)
		if _, err := sweepNodeService(env, "n", "t"); err == nil {
			t.Fatalf("a ready file without %s, want an error", k)
		}
	}
}

func TestASweepNodeIsNamedForItsDriver(t *testing.T) {
	if got := sweepNodeName("kestrel-relay-api"); got != "sweep-kestrel-relay-api" {
		t.Fatalf("node = %q", got)
	}
}

// A step adds enough components that the driver's task count changes by
// about the target, whatever the driver's poll count.
func TestAStepsComponentsMakeAboutTheTargetTasks(t *testing.T) {
	cases := []struct {
		polls  int
		target int
		want   int
	}{
		{1, 400, 400},
		{2, 400, 200},
		{3, 400, 134},
		{4, 400, 100},
		{2, 1, 1},
	}
	for _, c := range cases {
		polls := make([]time.Duration, c.polls)
		if got := componentsPerStep(c.target, polls); got != c.want {
			t.Errorf("%d polls, target %d: %d components, want %d", c.polls, c.target, got, c.want)
		}
	}
}

// A window holds at least one run of the driver's slowest poll, so a task
// on a 5 minute poll is counted in it.
func TestASweepWindowHoldsTheSlowestPoll(t *testing.T) {
	if got := sweepWindow(2*time.Minute, []time.Duration{5 * time.Minute, 30 * time.Second}); got != 5*time.Minute {
		t.Fatalf("window = %s, want the 5m poll", got)
	}
	if got := sweepWindow(2*time.Minute, []time.Duration{30 * time.Second}); got != 2*time.Minute {
		t.Fatalf("window = %s, want the configured 2m", got)
	}
}

func TestASweepsQueriesNameTheNodeAsALiteral(t *testing.T) {
	q := driverTasksQuery("sweep-o'brien")
	for _, want := range []string{"t.mode = 'poll'", "t.enabled", "'sweep-o''brien'"} {
		if !strings.Contains(q, want) {
			t.Fatalf("task query lacks %q:\n%s", want, q)
		}
	}
}

// A report is stamped at its window's end, so the step's poll CPU is the
// reports stamped from one report window into the step to its end: every
// run they hold ran inside the step.
func TestAStepsPollCPUIsTheReportsWhollyInsideIt(t *testing.T) {
	from := time.Unix(100, 0)
	q := pollCPUQuery("sweep-x", from, from.Add(2*time.Minute), time.Minute)
	for _, want := range []string{"'poll-cpu-time'", "'sweep-x'",
		"m.ts >= to_timestamp(160.000000)", "m.ts <= to_timestamp(220.000000)"} {
		if !strings.Contains(q, want) {
			t.Fatalf("cpu query lacks %q:\n%s", want, q)
		}
	}
}

// The sweep node runs the product's default cadence, what a deployment's
// node runs at, so the worklist pull and the heartbeat in its cgroup are a
// deployment's and not the lab's quicker ones.
func TestASweepNodeRunsTheProductsCadence(t *testing.T) {
	s, err := sweepNodeService(sweepEnv(), "sweep-x", "tok")
	if err != nil {
		t.Fatal(err)
	}
	cmd := strings.Join(s.Cmd, " ")
	for _, want := range []string{"--heartbeat " + node.DefaultHeartbeat.String(), "--probe-every " + node.DefaultProbeEvery.String(),
		"--worklist-every " + node.DefaultWorklistEvery.String()} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("cmd = %s, want %s", cmd, want)
		}
	}
}
