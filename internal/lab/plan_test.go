package lab

import (
	"slices"
	"strings"
	"testing"
)

func testOptions() Options {
	return Options{Image: "omniglass:dev", Port: DefaultPort, NodeToken: "tok", NodeTokenHash: "abc",
		Owner: Owner{Host: "box", PID: 42}}
}

func TestPlanDefaults(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if s.Project != DefaultProject || s.Network != "omniglass-lab_default" {
		t.Fatalf("project %q network %q, want the default project and its network", s.Project, s.Network)
	}
	var names []string
	for _, svc := range s.Services() {
		names = append(names, svc.Name)
	}
	if !slices.Equal(names, []string{"db", "seed", "server", "node"}) {
		t.Fatalf("services %v, want db, seed, server, node in start order", names)
	}
	if s.DB.Image != PostgresImage || s.Server.Image != "omniglass:dev" || s.Node.Image != "omniglass:dev" {
		t.Fatalf("images db=%s server=%s node=%s", s.DB.Image, s.Server.Image, s.Node.Image)
	}
}

// Exactly one port is published, the console, on loopback by default.
func TestPlanPublishesOnlyTheConsoleOnLoopback(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	var published []string
	for _, svc := range s.Services() {
		if svc.Publish != nil {
			published = append(published, svc.Name)
		}
	}
	if !slices.Equal(published, []string{"server"}) {
		t.Fatalf("published services %v, want only the server", published)
	}
	if p := s.Server.Publish; p.HostIP != "127.0.0.1" || p.HostPort != 8080 || p.ContainerPort != 8080 {
		t.Fatalf("publish %+v, want 127.0.0.1:8080 -> 8080", *p)
	}
}

func TestPlanListenAndPort(t *testing.T) {
	o := testOptions()
	o.Listen, o.Port = "0.0.0.0", 0
	s, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Server.Publish; p.HostIP != "0.0.0.0" || p.HostPort != 0 {
		t.Fatalf("publish %+v, want 0.0.0.0 with a Docker-picked port", *p)
	}
	o.Listen = "not-an-ip"
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--listen") {
		t.Fatalf("a bad --listen: %v, want a refusal naming --listen", err)
	}
	o.Listen, o.Port = "", 70000
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--port") {
		t.Fatalf("a bad --port: %v, want a refusal naming --port", err)
	}
}

// NET_ADMIN is held by the node alone, as root inside its own container, with
// the ping sysctl; the server and the database hold no added capability.
func TestPlanPrivilegeSitsOnTheNodeOnly(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range s.Services() {
		if svc.Name == "node" {
			continue
		}
		if len(svc.CapAdd) != 0 || svc.User != "" || len(svc.Sysctls) != 0 {
			t.Errorf("%s: caps %v user %q sysctls %v, want none", svc.Name, svc.CapAdd, svc.User, svc.Sysctls)
		}
	}
	n := s.Node
	if !slices.Equal(n.CapAdd, []string{"NET_ADMIN"}) || n.User != "0:0" ||
		n.Sysctls["net.ipv4.ping_group_range"] != "0 2147483647" {
		t.Fatalf("node caps %v user %q sysctls %v", n.CapAdd, n.User, n.Sysctls)
	}
	if !slices.Contains(n.Cmd, "--simulate") || !slices.Contains(n.Cmd, "--impairments") {
		t.Fatalf("node cmd %v, want node run --simulate --impairments", n.Cmd)
	}
}

// Every resource carries the compose project labels and the lab's owner
// labels, so compose sees one project and the reaper can find the owner.
func TestPlanLabelsEveryResource(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, l map[string]string) {
		t.Helper()
		for k, want := range map[string]string{
			LabelComposeProject: "omniglass-lab", LabelLab: "omniglass-lab", LabelLabHost: "box", LabelLabPID: "42",
		} {
			if l[k] != want {
				t.Errorf("%s: label %s = %q, want %q", what, k, l[k], want)
			}
		}
	}
	check("network", s.NetworkLabels)
	for _, v := range s.Volumes {
		check("volume "+v.Name, v.Labels)
	}
	for _, svc := range s.Services() {
		check(svc.Name, svc.Labels)
		if svc.Labels[LabelComposeHash] == "" {
			t.Errorf("%s: no compose config hash, so compose would not list it", svc.Name)
		}
		if _, ok := svc.Labels[LabelComposeFiles]; !ok {
			t.Errorf("%s: no compose config-files label, so compose warns on every command", svc.Name)
		}
		if svc.Labels[LabelComposeService] != svc.Name {
			t.Errorf("%s: compose service label %q", svc.Name, svc.Labels[LabelComposeService])
		}
		wantOneOff := "False"
		if svc.Name == "seed" {
			wantOneOff = "True"
		}
		if svc.Labels[LabelComposeOneOff] != wantOneOff {
			t.Errorf("%s: oneoff %q, want %q", svc.Name, svc.Labels[LabelComposeOneOff], wantOneOff)
		}
	}
}

// The node runs with a token minted for this run, and the seed gets only its
// hash, so the database never holds a usable node secret from the lab.
func TestPlanNodeTokenFlow(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Node.Env, "OMNIGLASS_NODE_TOKEN=tok") {
		t.Fatalf("node env %v, want the run's token", s.Node.Env)
	}
	for _, e := range s.Seed.Env {
		if strings.Contains(e, "=tok") {
			t.Fatalf("the seed sees the plaintext token: %s", e)
		}
	}
	if !slices.Contains(s.Seed.Env, "OMNIGLASS_LAB_NODE_TOKEN_HASH=abc") {
		t.Fatalf("seed env %v, want the token's hash", s.Seed.Env)
	}
}

// The simulated pusher's token (#1370) flows as the node's does: minted per
// run, the node's simulator holds the plaintext, and the seed gets only its
// hash and its non-secret prefix. A lab with no node runs no simulator, so
// it carries none.
func TestPlanPushTokenFlow(t *testing.T) {
	o := testOptions()
	o.PushToken, o.PushTokenHash, o.PushTokenPrefix = "ptok", "pushhash", "0a0b0c0d"
	s, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Node.Env, "OMNIGLASS_SIM_PUSH_TOKEN=ptok") {
		t.Fatalf("node env %v, want the run's push token", s.Node.Env)
	}
	for _, e := range s.Seed.Env {
		if strings.Contains(e, "=ptok") {
			t.Fatalf("the seed sees the plaintext push token: %s", e)
		}
	}
	for _, want := range []string{"OMNIGLASS_LAB_PUSH_TOKEN_HASH=pushhash", "OMNIGLASS_LAB_PUSH_TOKEN_PREFIX=0a0b0c0d"} {
		if !slices.Contains(s.Seed.Env, want) {
			t.Fatalf("seed env %v, want %s", s.Seed.Env, want)
		}
	}
	if h := configHash(s.Node); strings.Contains(h, "ptok") {
		t.Fatal("the node's config hash carries the token")
	}
}

// The database and the data directory are named volumes kept across runs;
// the server and the seed share the data directory, so the key the seed's
// secrets are sealed with is the one the server opens them with.
func TestPlanVolumes(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Volumes) != 2 || s.Volumes[0].Name != "omniglass-lab_db" || s.Volumes[1].Name != "omniglass-lab_data" {
		t.Fatalf("volumes %+v", s.Volumes)
	}
	if s.Seed.Mounts[0] != s.Server.Mounts[0] {
		t.Fatalf("seed mounts %v, server mounts %v: want the one data volume at one path", s.Seed.Mounts, s.Server.Mounts)
	}
}

// A learner's lab probes every 10s (#1273): it checks in every 2s so a change
// in the console shows within seconds, but it may run for days, and a probe
// on every check-in writes most of its rows. A script's throwaway lab sets no
// probe interval, so it probes on every 2s check-in: it lives for minutes, and
// a walkthrough that waits on a probe's verdict is recorded against a budget.
func TestPlanProbesALearnersLabEvery10s(t *testing.T) {
	flag := func(cmd []string, name string) string {
		i := slices.Index(cmd, name)
		if i < 0 || i+1 >= len(cmd) {
			return ""
		}
		return cmd[i+1]
	}
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := flag(s.Node.Cmd, "--probe-every"); got != "10s" {
		t.Fatalf("a learner's lab: --probe-every %q in %v, want 10s", got, s.Node.Cmd)
	}
	if got := flag(s.Node.Cmd, "--heartbeat"); got != "2s" {
		t.Fatalf("a learner's lab: --heartbeat %q, want 2s", got)
	}
	o := testOptions()
	o.Project, o.Ephemeral = "demo-1", true
	if s, err = Plan(o); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(s.Node.Cmd, "--probe-every") {
		t.Fatalf("a throwaway lab: %v sets a probe interval, want none (every check-in)", s.Node.Cmd)
	}
	if got := flag(s.Node.Cmd, "--heartbeat"); got != "2s" {
		t.Fatalf("a throwaway lab: --heartbeat %q, want 2s", got)
	}
}

// A lab with no node (#1267) is a throwaway lab whose seed is told so: it
// lays the fleet down unwired and enrolls nothing, so no node runs. A
// learner's lab is refused one, and a lab without the option seeds as ever.
func TestPlanNoNode(t *testing.T) {
	s, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(s.Seed.Cmd, "--no-node") {
		t.Fatalf("a lab with its node: the seed runs %v, want no --no-node", s.Seed.Cmd)
	}
	o := testOptions()
	o.NoNode = true
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--ephemeral") {
		t.Fatalf("a learner's lab with no node: %v, want a refusal naming --ephemeral", err)
	}
	o.Project, o.Ephemeral = "shots-1", true
	if s, err = Plan(o); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Seed.Cmd, "--no-node") {
		t.Fatalf("a throwaway lab with no node: the seed runs %v, want --no-node", s.Seed.Cmd)
	}
}
