package lab

import (
	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/simnode"

	"fmt"
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
	if !slices.Equal(n.CapAdd, []string{"NET_ADMIN"}) || n.User != "0:0" {
		t.Fatalf("node caps %v user %q", n.CapAdd, n.User)
	}
	// The range admits root (the node's user) and distroless nonroot (65532),
	// and stays inside the 65537 groups a standard rootless Docker maps
	// (/etc/subgid's 65536 plus the user's own): a group outside the mapping
	// makes the kernel refuse the write and the node never starts (#1506).
	var low, high int
	if _, err := fmt.Sscanf(n.Sysctls["net.ipv4.ping_group_range"], "%d %d", &low, &high); err != nil {
		t.Fatalf("node sysctls %v: %v", n.Sysctls, err)
	}
	if low != 0 || high < 65532 || high > 65536 {
		t.Fatalf("ping_group_range %d %d, want 0 through at least 65532 and at most 65536", low, high)
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
// on every check-in writes most of its rows. A script's throwaway lab probes
// every 2s: it lives for minutes, and a walkthrough that waits on a probe's
// verdict is recorded against a budget. Since #1634 a probe runs on the node's
// own timer, never the check-in, so every lab names its interval.
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
	if got := flag(s.Node.Cmd, "--probe-every"); got != "2s" {
		t.Fatalf("a throwaway lab: --probe-every %q in %v, want 2s", got, s.Node.Cmd)
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

// A throwaway lab's collection report window (#1646): a walkthrough has a
// minute of video, and the platform's own 60s window would land the first
// delivery and poll figures as the recording ends, so the walkthrough
// profile's seed sets collection.report_seconds to 10. A learner's lab keeps
// the platform's setting (an operator's change to it survives a restart), as
// does a throwaway lab with no node, which has no collection to report and
// whose pages hold still for the docs shots.
func TestPlanWalkthroughLabReportsEvery10s(t *testing.T) {
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
	if slices.Contains(s.Seed.Cmd, "--report-seconds") {
		t.Fatalf("a learner's lab: the seed runs %v, want the platform's report window left as it is", s.Seed.Cmd)
	}
	o := testOptions()
	o.Project, o.Ephemeral = "demo-1", true
	if s, err = Plan(o); err != nil {
		t.Fatal(err)
	}
	if got := flag(s.Seed.Cmd, "--report-seconds"); got != "10" {
		t.Fatalf("a throwaway lab: --report-seconds %q in %v, want 10", got, s.Seed.Cmd)
	}
	o.NoNode = true
	if s, err = Plan(o); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(s.Seed.Cmd, "--report-seconds") {
		t.Fatalf("a throwaway lab with no node: the seed runs %v, want the report window left as it is", s.Seed.Cmd)
	}
}

// A cadence profile (#1277) names how often the lab's node checks in and
// probes, so the lab footprint can measure a lab at the product's default
// cadence beside the lab's own. A lab given none keeps the cadence it has
// always run at, an unknown one is refused, and each profile lands on the
// node's command line as its heartbeat and probe interval.
func TestPlanCadenceProfiles(t *testing.T) {
	flag := func(cmd []string, name string) string {
		i := slices.Index(cmd, name)
		if i < 0 || i+1 >= len(cmd) {
			return ""
		}
		return cmd[i+1]
	}
	cases := []struct {
		cadence   string
		ephemeral bool
		heartbeat string
		probe     string
		worklist  string
		simPoll   string
		bracket   string
	}{
		{"", false, "2s", "10s", "5s", "2s", "10s"},
		{"", true, "2s", "2s", "2s", "2s", "1s"},
		{"lab", true, "2s", "10s", "5s", "2s", "10s"},
		{"walkthrough", false, "2s", "2s", "2s", "2s", "1s"},
		// The product's own: the node's and the simulator's defaults, so
		// the profile cannot drift from what a deployment runs.
		{"default", true, node.DefaultHeartbeat.String(), node.DefaultProbeEvery.String(), node.DefaultWorklistEvery.String(), simnode.DefaultPoll.String(), "1m"},
	}
	for _, c := range cases {
		o := testOptions()
		o.Cadence = c.cadence
		if c.ephemeral {
			o.Project, o.Ephemeral = "foot-1", true
		}
		s, err := Plan(o)
		if err != nil {
			t.Fatalf("cadence %q: %v", c.cadence, err)
		}
		if got := flag(s.Node.Cmd, "--heartbeat"); got != c.heartbeat {
			t.Errorf("cadence %q: --heartbeat %q in %v, want %s", c.cadence, got, s.Node.Cmd, c.heartbeat)
		}
		if got := flag(s.Node.Cmd, "--poll"); got != c.simPoll {
			t.Errorf("cadence %q: --poll %q in %v, want %s", c.cadence, got, s.Node.Cmd, c.simPoll)
		}
		if got := flag(s.Node.Cmd, "--worklist-every"); got != c.worklist {
			t.Errorf("cadence %q: --worklist-every %q in %v, want %s", c.cadence, got, s.Node.Cmd, c.worklist)
		}
		if got := flag(s.Node.Cmd, "--probe-every"); got != c.probe {
			t.Errorf("cadence %q: --probe-every %q in %v, want %q", c.cadence, got, s.Node.Cmd, c.probe)
		}
		// The server is told the bracket its closed loop takes and the
		// cadence its node runs at (#1647), so Platform health counts the
		// poll targets the node really ran.
		env := map[string]string{}
		for _, kv := range s.Server.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		if got := env["OMNIGLASS_CLOSED_LOOP_BRACKET"]; got != c.bracket {
			t.Errorf("cadence %q: closed-loop bracket %q, want %s", c.cadence, got, c.bracket)
		}
		if got := env["OMNIGLASS_CLOSED_LOOP_HEARTBEAT"]; got != c.heartbeat {
			t.Errorf("cadence %q: closed-loop heartbeat %q, want %s", c.cadence, got, c.heartbeat)
		}
		if got := env["OMNIGLASS_CLOSED_LOOP_PROBE_EVERY"]; got != c.probe {
			t.Errorf("cadence %q: closed-loop probe interval %q, want %q", c.cadence, got, c.probe)
		}
		if _, err := closedloop.ParseSettings(closedloop.RawSettings{Bracket: env["OMNIGLASS_CLOSED_LOOP_BRACKET"],
			Heartbeat: env["OMNIGLASS_CLOSED_LOOP_HEARTBEAT"], ProbeEvery: env["OMNIGLASS_CLOSED_LOOP_PROBE_EVERY"]}, node.DefaultHeartbeat); err != nil {
			t.Errorf("cadence %q: the server would refuse its closed-loop settings: %v", c.cadence, err)
		}
	}
	o := testOptions()
	o.Cadence = "fast"
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "walkthrough") {
		t.Fatalf("an unknown cadence: %v, want a refusal listing the profiles", err)
	}
}

func scaleOptions() Options {
	o := testOptions()
	o.Project, o.Ephemeral, o.Scenario, o.Systems = "omniglass-scale", true, "corporate", 100
	o.SiteNodes = []SiteNode{
		{Name: "edge-hq", Token: "t-hq", TokenHash: "h-hq", Components: 230},
		{Name: "edge-regional-01", Token: "t-r1", TokenHash: "h-r1", Components: 68},
		{Name: "edge-branch-01", Token: "t-b1", TokenHash: "h-b1", Components: 24},
	}
	return o
}

// A scale lab runs one node container per site, in place of the dev node,
// each enrolled as its site's node with its own token, and each with the
// privilege the dev node has and nothing more.
func TestPlanScaleLabRunsOneNodePerSite(t *testing.T) {
	o := scaleOptions()
	s, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if s.Node.Name != "" {
		t.Fatalf("a scale lab planned the dev node %s", s.Node.Name)
	}
	if len(s.Nodes) != len(o.SiteNodes) {
		t.Fatalf("%d node containers for %d sites", len(s.Nodes), len(o.SiteNodes))
	}
	containers := map[string]bool{}
	for i, n := range s.Nodes {
		site := o.SiteNodes[i]
		if !slices.Contains(n.Env, "OMNIGLASS_NODE_NAME="+site.Name) || !slices.Contains(n.Env, "OMNIGLASS_NODE_TOKEN="+site.Token) {
			t.Errorf("node %s env %v does not enroll it as %s with its own token", n.Name, n.Env, site.Name)
		}
		if containers[n.Container] {
			t.Errorf("container %s planned twice", n.Container)
		}
		containers[n.Container] = true
		if n.Labels[LabelComposeService] != n.Name || n.Labels[LabelComposeHash] == "" {
			t.Errorf("node %s labels %v", n.Name, n.Labels)
		}
		if !slices.Equal(n.CapAdd, []string{"NET_ADMIN"}) || n.Publish != nil {
			t.Errorf("node %s caps %v publish %v", n.Name, n.CapAdd, n.Publish)
		}
	}
	var names []string
	for _, svc := range s.Services() {
		names = append(names, svc.Name)
	}
	want := []string{"db", "seed", "server", "edge-hq", "edge-regional-01", "edge-branch-01"}
	if !slices.Equal(names, want) {
		t.Fatalf("services %v, want %v", names, want)
	}
}

// The scale lab's seed resolves the scenario at the size and enrolls every
// site's node from the hashes it is handed, never the tokens.
func TestPlanScaleLabSeedEnrollsEverySiteNode(t *testing.T) {
	o := scaleOptions()
	s, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	cmd := strings.Join(s.Seed.Cmd, " ")
	if cmd != "lab seed --scenario corporate --systems 100 --user dev --password dev" {
		t.Fatalf("seed command %q", cmd)
	}
	var hashes string
	for _, e := range s.Seed.Env {
		if v, ok := strings.CutPrefix(e, SiteNodeTokenHashesEnv+"="); ok {
			hashes = v
		}
		for _, n := range o.SiteNodes {
			if strings.Contains(e, n.Token) {
				t.Errorf("the seed's env %q carries %s's token", e, n.Name)
			}
		}
	}
	got, err := ParseSiteNodeTokenHashes(hashes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["edge-hq"] != "h-hq" || got["edge-branch-01"] != "h-b1" {
		t.Fatalf("token hashes %v", got)
	}
}

// A kept scale lab (#1633) tells its seed so, with a hidden --keep: the seed
// then keeps the fleet a database already holds and re-enrolls its nodes. A
// throwaway one's seed carries no --keep, so a database holding data stays
// refused.
func TestPlanScaleLabSeedCarriesKeepOnlyWhenKept(t *testing.T) {
	for _, tc := range []struct {
		ephemeral bool
		want      string
	}{
		{true, "lab seed --scenario corporate --systems 100 --user dev --password dev"},
		{false, "lab seed --scenario corporate --systems 100 --user dev --password dev --keep"},
	} {
		o := scaleOptions()
		o.Ephemeral = tc.ephemeral
		s, err := Plan(o)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(s.Seed.Cmd, " "); got != tc.want {
			t.Errorf("ephemeral %v: seed command %q, want %q", tc.ephemeral, got, tc.want)
		}
	}
}

// The stop line's restart hint (#1633) is the command that brings this lab
// back: a bare lab run for a learner's lab, and for a kept scale lab its own
// scenario, size and --keep (and its cadence, when not the lab's own), which
// a bare lab run would not bring back.
func TestRestartCommandNamesAKeptScaleLabsOwnFlags(t *testing.T) {
	if got := RestartCommand(testOptions()); got != "omniglass lab run" {
		t.Errorf("a learner's lab: %q", got)
	}
	o := scaleOptions()
	o.Ephemeral, o.Systems, o.Cadence = false, 3000, CadenceLab
	if got := RestartCommand(o); got != "omniglass lab run --scenario corporate --systems 3000 --keep" {
		t.Errorf("a kept scale lab: %q", got)
	}
	o.Cadence = CadenceDefault
	if got := RestartCommand(o); got != "omniglass lab run --scenario corporate --systems 3000 --cadence default --keep" {
		t.Errorf("a kept scale lab at the default cadence: %q", got)
	}
	// A kept scale lab under a project of its own keeps its data there, so a
	// restart without that project would start an empty lab beside it.
	o.Cadence, o.Project = CadenceLab, "omniglass-scale-it-7"
	if got := RestartCommand(o); got != "omniglass lab run --scenario corporate --systems 3000 --project omniglass-scale-it-7 --keep" {
		t.Errorf("a kept scale lab under its own project: %q", got)
	}
}

// A scale lab's Postgres records commit timestamps, which `lab measure` reads
// the ingest lag from (a row's commit time less its probe time). A dev lab's
// does not: it pays for nothing it does not read.
func TestPlanScaleLabRecordsCommitTimestamps(t *testing.T) {
	s, err := Plan(scaleOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.DB.Cmd, " "); got != "postgres -c track_commit_timestamp=on" {
		t.Fatalf("a scale lab's database runs %q, want postgres with commit timestamps on", got)
	}
	dev, err := Plan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.DB.Cmd) != 0 {
		t.Fatalf("a dev lab's database runs %q, want the image's own command", dev.DB.Cmd)
	}
}

func TestPlanScaleLabNeedsASize(t *testing.T) {
	o := scaleOptions()
	o.Systems = 0
	if _, err := Plan(o); err == nil {
		t.Fatal("a scale lab with no size planned")
	}
	o = scaleOptions()
	o.NoNode = true
	if _, err := Plan(o); err == nil {
		t.Fatal("a scale lab with no node planned")
	}
}

func TestParseSiteNodeTokenHashesRefusesAMalformedPair(t *testing.T) {
	for _, s := range []string{"edge-hq", "=h", "edge-hq=", "a=b,c"} {
		if _, err := ParseSiteNodeTokenHashes(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
	if got, err := ParseSiteNodeTokenHashes(""); err != nil || len(got) != 0 {
		t.Errorf("empty: %v %v", got, err)
	}
}

// Every service carries the fleet the lab seeded (#1719): the scenario, and
// for a scale lab its size, so a measurement of the running lab reads them
// from the lab itself and never from a flag that could disagree with it.
func TestPlanStampsTheFleetOnEveryService(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Options
		want Fleet
	}{
		{"dev", testOptions(), Fleet{Scenario: DefaultScenario}},
		{"scale", scaleOptions(), Fleet{Scenario: "corporate", Systems: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Plan(tc.o)
			if err != nil {
				t.Fatal(err)
			}
			for _, svc := range s.Services() {
				got, err := FleetOf(svc.Labels)
				if err != nil {
					t.Fatalf("%s: %v", svc.Name, err)
				}
				if got != tc.want {
					t.Errorf("%s: fleet %+v, want %+v", svc.Name, got, tc.want)
				}
			}
		})
	}
}

// FleetOf reads the fleet back from a container's labels, and refuses a
// container a build before the labels started, or one whose size label is
// not a count, rather than guess.
func TestFleetOf(t *testing.T) {
	for _, tc := range []struct {
		name    string
		labels  map[string]string
		want    Fleet
		wantErr string
	}{
		{"scale", map[string]string{LabelLabScenario: "corporate", LabelLabSystems: "300"}, Fleet{Scenario: "corporate", Systems: 300}, ""},
		{"dev", map[string]string{LabelLabScenario: "dev"}, Fleet{Scenario: "dev"}, ""},
		{"unlabelled", map[string]string{LabelLab: "omniglass-scale"}, Fleet{},
			"name no scenario: it was brought up by a build before the fleet labels, so stop it and bring it up again with this build's `lab run`"},
		{"bad size", map[string]string{LabelLabScenario: "corporate", LabelLabSystems: "lots"}, Fleet{},
			`size label omniglass.lab.systems="lots" is not a count of systems`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FleetOf(tc.labels)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v, want it to say %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("FleetOf = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
