// Package lab is `omniglass lab run`: one command that brings up a working
// omniglass on Docker, as one compose project of three containers (Postgres,
// the server, and an edge node running the simulator in process) serving the
// seeded dev fleet, and tears it down on Ctrl-C.
//
// It has two parts. The planner (this file) is pure: it turns the options
// into a project spec, every service, image, network, volume, capability,
// sysctl, environment variable, label and the one publish, so what the lab
// runs and with which privilege is reviewable and testable without Docker,
// and the lab guide's topology table is rendered from it. The runner
// (run.go) realizes a spec through the Docker Engine API, waits on
// readiness, and tears down.
package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/simnode"
)

// Defaults for the options a user does not set.
const (
	DefaultProject  = "omniglass-lab"
	DefaultPort     = 8080
	DefaultListen   = "127.0.0.1"
	DefaultScenario = "dev"
	// The node the dev seed places every simulated component on.
	NodeName = "edge-hq"
	// ServerName is the server's fixed identity: a container's hostname is
	// new each run, and this keeps one server platform component across
	// restarts on a kept database, the one a seed writes history onto.
	ServerName = "server"
	// The well-known login of every walkthrough, so a learner's screen
	// matches the lesson's.
	LoginUser     = "dev"
	LoginPassword = "dev"
	// PostgresImage is the database the lab runs.
	PostgresImage = "postgres:18"
	// DSN is the database's address on the lab network.
	DSN = "postgres://omniglass:omniglass@db:5432/omniglass?sslmode=disable"
	// ServerURL is the server's address on the lab network, where a
	// container that joins it reaches the API and the console.
	ServerURL = "http://server:8080"
)

// In-container paths and ports.
const (
	dataDir     = "/var/lib/omniglass"
	pgDataDir   = "/var/lib/postgresql"
	consolePort = 8080
	busPort     = 4222
	// The unprivileged pinger needs the node's groups admitted: root, and
	// distroless nonroot (65532). The range stays inside the groups a standard
	// rootless Docker maps (0 to 65536), since the kernel refuses a range
	// naming an unmapped group and the node would never start (#1506).
	pingGroupRange = "0 65535"
)

// Label keys. The compose keys make `docker compose ls`, `ps` and `logs`,
// and Docker Desktop, show the lab as one project; the lab's own keys name
// the owner (host and pid) the reaper checks.
const (
	LabelComposeProject = "com.docker.compose.project"
	LabelComposeService = "com.docker.compose.service"
	LabelComposeNumber  = "com.docker.compose.container-number"
	LabelComposeOneOff  = "com.docker.compose.oneoff"
	LabelComposeNetwork = "com.docker.compose.network"
	LabelComposeVolume  = "com.docker.compose.volume"
	LabelComposeWorkDir = "com.docker.compose.project.working_dir"
	LabelComposeVersion = "com.docker.compose.version"
	LabelComposeHash    = "com.docker.compose.config-hash"
	LabelComposeFiles   = "com.docker.compose.project.config_files"
	LabelLab            = "omniglass.lab"
	LabelLabHost        = "omniglass.lab.host"
	LabelLabPID         = "omniglass.lab.pid"
	LabelLabPIDNS       = "omniglass.lab.pidns"
	// LabelLabEphemeral marks a throwaway lab (a demo recorder's, #1197):
	// its data goes with it, and the reaper may remove an abandoned one of
	// any project, which it never does to a learner's lab.
	LabelLabEphemeral = "omniglass.lab.ephemeral"
	// LabelLabScenario and LabelLabSystems are the fleet the lab seeded
	// (#1719): its scenario, and a scale lab's size. A measurement of the
	// running lab reads them (FleetOf), so its report names the fleet the
	// lab really runs, never a flag that could disagree with it.
	LabelLabScenario      = "omniglass.lab.scenario"
	LabelLabSystems       = "omniglass.lab.systems"
	composeVersionClaimed = "2.0.0"
)

// Fleet is the fleet a lab seeded: its scenario, and the size a scale lab
// was resolved at (0 for a dev scenario, which has no size).
type Fleet struct {
	Scenario string
	Systems  int
}

// labels is the fleet as the service labels that carry it.
func (f Fleet) labels() map[string]string {
	l := map[string]string{LabelLabScenario: f.Scenario}
	if f.Systems > 0 {
		l[LabelLabSystems] = strconv.Itoa(f.Systems)
	}
	return l
}

// FleetOf reads the fleet from a lab container's labels. A container with no
// scenario label was brought up by a build before the labels, and one whose
// size is not a count is refused, rather than either read as a guess.
func FleetOf(labels map[string]string) (Fleet, error) {
	f := Fleet{Scenario: labels[LabelLabScenario]}
	if f.Scenario == "" {
		return Fleet{}, fmt.Errorf("the lab's containers name no scenario: it was brought up by a build before the fleet labels, so stop it and bring it up again with this build's `lab run`")
	}
	if v, ok := labels[LabelLabSystems]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Fleet{}, fmt.Errorf("the lab's size label %s=%q is not a count of systems", LabelLabSystems, v)
		}
		f.Systems = n
	}
	return f, nil
}

// Cadence is a cadence profile (#1277): how often the lab's node checks in
// and probes reachability. The lab's own profiles keep a change in the
// console visible within seconds; `default` runs the node at the product's
// own defaults, so the lab footprint can measure what a real deployment
// costs beside what a lab does.
type Cadence struct {
	Name string
	// Heartbeat is the node's check-in interval, and the interval of a
	// driver poll that declares no schedule of its own (#1634).
	Heartbeat string
	// ProbeEvery is the reachability probe interval, on the node's own
	// timer, apart from its check-in (#1634).
	ProbeEvery string
	// WorklistEvery is how often the node pulls its worklist (#1577), apart
	// from its check-in, so a short check-in does not rebuild every
	// worklist as often.
	WorklistEvery string
	// SimPoll is how often the node's simulator re-pulls its component
	// list: a lab's profiles every 2s, so a component added in the console
	// answers within seconds; `default` at the simulator's own default, so a
	// measurement of the product's cadence carries as little of the
	// simulator's own traffic as it can.
	SimPoll string
	// ReportSeconds is the platform's collection report window the lab's
	// seed sets (collection.report_seconds, #1646), so the node's delivery
	// and poll figures and the server's land within a walkthrough's minute.
	// Zero leaves the platform's own setting, as a learner's lab and a
	// measurement at the product's defaults do.
	ReportSeconds int
	// ClosedLoopBracket is how often the lab's server brackets its node for
	// Platform health's closed loop (#1647), its window ten of them: a
	// learner's lab every 10s, so the window fills in under two minutes; a
	// script's every 1s, so a walkthrough sees an impairment open a gap and
	// clear inside its recorded budget; `default` the server's own minute.
	ClosedLoopBracket string
	// For says who runs at it, in the lab guide's words.
	For string
}

// The cadence profiles, by name.
const (
	CadenceLab         = "lab"
	CadenceWalkthrough = "walkthrough"
	CadenceDefault     = "default"
)

// CadenceProfiles are the profiles a lab runs at, in the order the docs list
// them.
func CadenceProfiles() []Cadence {
	return []Cadence{
		// A learner's lab may run for days, and a probe on every 2s check-in
		// wrote most of its rows (#1273), so it probes every 10s.
		// A change in the console reaches the node at its next pull, so the
		// lab pulls every 5s: within seconds, without a 1,000-system lab
		// rebuilding every site's worklist every 2s (#1577).
		{Name: CadenceLab, Heartbeat: "2s", ProbeEvery: "10s", WorklistEvery: "5s", SimPoll: "2s", ClosedLoopBracket: "10s",
			For: "a learner's lab: a change in the console shows within seconds"},
		// A script's throwaway lab lives for minutes and probes every 2s, so
		// a walkthrough waiting on a probe's verdict keeps its recorded
		// budget.
		// Its report window is 10s, the setting's floor: at the platform's
		// 60s the first delivery and poll figures land as a minute's
		// recording ends (#1646). Its closed loop brackets every second, a
		// 10s window, so a walkthrough sees an impairment's attribution
		// clear inside its budget (#1647).
		{Name: CadenceWalkthrough, Heartbeat: "2s", ProbeEvery: "2s", WorklistEvery: "2s", SimPoll: "2s", ReportSeconds: 10, ClosedLoopBracket: "1s",
			For: "a script's throwaway lab: a walkthrough waiting on a probe's verdict sees it within seconds"},
		{Name: CadenceDefault, Heartbeat: node.DefaultHeartbeat.String(), ProbeEvery: node.DefaultProbeEvery.String(), WorklistEvery: node.DefaultWorklistEvery.String(), SimPoll: simnode.DefaultPoll.String(), ClosedLoopBracket: "1m",
			For: "the product's own defaults: what a deployment's node runs at"},
	}
}

// CadenceNames are the profiles' names, for a flag's help.
func CadenceNames() string {
	var names []string
	for _, c := range CadenceProfiles() {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// LookupCadence is the named profile. An empty name is the lab's own: a
// learner's lab runs at `lab`, a throwaway lab at `walkthrough`.
func LookupCadence(name string, ephemeral bool) (Cadence, error) {
	if name == "" {
		name = CadenceLab
		if ephemeral {
			name = CadenceWalkthrough
		}
	}
	for _, c := range CadenceProfiles() {
		if c.Name == name {
			return c, nil
		}
	}
	return Cadence{}, fmt.Errorf("--cadence %q is not a cadence profile; the profiles are %s", name, CadenceNames())
}

// Owner is the process that runs a lab: the reaper removes a lab whose
// owner on this host is gone (killed too hard to tear down).
type Owner struct {
	Host string
	PID  int
	// PIDNS is the owner's PID namespace: a pid means something only inside
	// it, so an owner in another one (a lab run from a container that shares
	// the host's name) is never judged by this namespace's pids.
	PIDNS string
}

// Options are a lab's settings.
type Options struct {
	// Project is the compose project name; every resource is named from it.
	Project string
	// Listen and Port are where the console is published on the host. Port
	// 0 lets Docker pick one, for a second lab beside the first.
	Listen string
	Port   int
	// Image is the omniglass image the server, node and seed run.
	Image string
	// Scenario is the dev seed's fleet.
	Scenario string
	// NodeToken is the node's enrollment token, minted per run: the lab
	// re-enrolls its node every time, so it stores no node secret.
	NodeToken     string
	NodeTokenHash string
	// Pushers are the outside actors the scenario declares (#1743), each
	// with its token minted per run the same way: the emulator on the
	// pusher's node holds the plaintext, and the seed grants the pusher's
	// principal and sets its credential from the hash and non-secret
	// prefix. Both lab kinds take them.
	Pushers []Pusher
	Owner   Owner
	// WorkDir is reported to compose as the project's working directory.
	WorkDir string
	// Ephemeral makes a throwaway lab, for a script (the demo pipeline's
	// recorders and the probe, #1197) rather than a learner: every resource
	// is labeled as such, and its volumes go at teardown.
	Ephemeral bool
	// NoNode makes a lab whose pages hold still, for a script that
	// photographs them (the docs screenshots, #1267): the seed lays the scenario's fleet down without the
	// simulated fleet's wiring and enrolls no node, so no node runs and
	// nothing collects. Only a throwaway lab may run without one.
	NoNode bool
	// PlatformHistory seeds an hour of the server's own platform figures
	// before the lab comes up (#1790), so a capture of platform health draws
	// full lines instead of the few minutes a fresh lab has. It is a
	// capture's, never a learner's: those figures are made up, so only a
	// throwaway lab takes it, and never a scale lab, whose figures are the
	// measurement.
	PlatformHistory bool
	// Cadence names the node's cadence profile (CadenceProfiles); empty is
	// the lab's own.
	Cadence string
	// Systems and SiteNodes make a scale lab (#1554): the scale lab
	// scenario Scenario resolved at Systems systems, with one node per site
	// in place of the one dev node. The seed lays the plan down and enrolls
	// each site's node with its token's hash.
	Systems   int
	SiteNodes []SiteNode
	// InduceUnregistered makes one simulated device of a scale lab answer a
	// property key no driver registers (#1698), so the delivery table reads
	// a property loss caused by refused-unregistered. Off by default; the
	// seed does the work.
	InduceUnregistered bool
}

// SiteNode is one site's node in a scale lab: its name, the enrollment
// token minted for it this run, and how many simulated components the plan
// places on it.
type SiteNode struct {
	Name       string
	Token      string
	TokenHash  string
	Components int
}

// Scale reports whether the options make a scale lab.
func (o Options) Scale() bool { return len(o.SiteNodes) > 0 }

// Mount is a named volume mounted into a container.
type Mount struct {
	Volume string
	Target string
}

// Publish is the one port the lab publishes on the host.
type Publish struct {
	HostIP        string
	HostPort      int
	ContainerPort int
}

// Service is one container of the project.
type Service struct {
	// Name is the compose service name, which is also its address on the
	// lab network.
	Name string
	// Container is the container's name.
	Container string
	Image     string
	Cmd       []string
	Env       []string
	// User overrides the image's user; empty keeps it (the omniglass image
	// runs nonroot).
	User    string
	CapAdd  []string
	Sysctls map[string]string
	Mounts  []Mount
	Publish *Publish
	// Health is a container healthcheck command (exec form), if any.
	Health []string
	// Restart is the restart policy: the node restarts if it crashes.
	Restart string
	// OneOff marks a run-once container (compose's oneoff label).
	OneOff bool
	Labels map[string]string
	// Role says what the container is for, in a learner's words: the lab
	// guide's topology table reads it.
	Role string
}

// Volume is a named volume the project keeps across runs.
type Volume struct {
	Key    string
	Name   string
	Labels map[string]string
	Holds  string
}

// Spec is the whole project: what the runner realizes.
type Spec struct {
	Project string
	// Owner is the run that owns what the spec creates; its own teardown
	// removes only what carries this owner.
	Owner Owner
	// Ephemeral is a throwaway lab's (Options.Ephemeral).
	Ephemeral bool
	Network   string
	// NetworkLabels label the lab network.
	NetworkLabels map[string]string
	Volumes       []Volume
	DB            Service
	Seed          Service
	Server        Service
	// Node is the lab's one node; a scale lab has none, and runs Nodes,
	// one per site, instead.
	Node  Service
	Nodes []Service
}

// Services are the project's containers in start order.
func (s Spec) Services() []Service {
	out := []Service{s.DB, s.Seed, s.Server}
	if s.Node.Name != "" {
		out = append(out, s.Node)
	}
	return append(out, s.Nodes...)
}

// ProjectLabels are the labels that select everything a project created.
func ProjectLabels(project string) map[string]string {
	return map[string]string{LabelComposeProject: project, LabelLab: project}
}

// OwnedLabels select what one run of the project created: its teardown
// removes only those, so a run never removes another live lab's (two runs
// racing to one project). A spec with no owner selects the whole project.
func OwnedLabels(s Spec) map[string]string {
	l := ProjectLabels(s.Project)
	if s.Owner.PID != 0 {
		l[LabelLabHost], l[LabelLabPID], l[LabelLabPIDNS] = s.Owner.Host, strconv.Itoa(s.Owner.PID), s.Owner.PIDNS
	}
	if s.Ephemeral {
		l[LabelLabEphemeral] = "true"
	}
	return l
}

// Plan turns the options into the project spec. Pure.
func Plan(o Options) (Spec, error) {
	if o.Project == "" {
		o.Project = DefaultProject
	}
	if o.Listen == "" {
		o.Listen = DefaultListen
	}
	if o.Scenario == "" {
		o.Scenario = DefaultScenario
	}
	if net.ParseIP(o.Listen) == nil {
		return Spec{}, fmt.Errorf("--listen %q is not an IP address", o.Listen)
	}
	if o.Port < 0 || o.Port > 65535 {
		return Spec{}, fmt.Errorf("--port %d is not a port", o.Port)
	}
	if o.Image == "" {
		return Spec{}, fmt.Errorf("lab: no image to run")
	}
	if o.Ephemeral && o.Project == DefaultProject {
		return Spec{}, fmt.Errorf("a throwaway lab removes its data when it stops, so it needs a --project of its own, never %s", DefaultProject)
	}
	cadence, err := LookupCadence(o.Cadence, o.Ephemeral)
	if err != nil {
		return Spec{}, err
	}
	if o.Scale() && (o.NoNode || o.Systems <= 0) {
		return Spec{}, fmt.Errorf("a scale lab runs a node per site at a size: it needs --systems and cannot run with --no-node")
	}
	if err := checkPusherNodes(o); err != nil {
		return Spec{}, err
	}
	if o.InduceUnregistered && !o.Scale() {
		return Spec{}, fmt.Errorf("--induce-unregistered is a scale lab's: it needs --systems")
	}
	if o.PlatformHistory && o.Scale() {
		return Spec{}, fmt.Errorf("a scale lab's platform figures are its measurement, so it cannot take --platform-history")
	}
	if o.PlatformHistory && !o.Ephemeral {
		return Spec{}, fmt.Errorf("--platform-history seeds made-up platform figures for a capture, so it needs --ephemeral: a learner's lab reads its own")
	}
	if o.NoNode && !o.Ephemeral {
		return Spec{}, fmt.Errorf("a lab with no node is a script's, so it needs --ephemeral: a learner's lab keeps its fleet, and one seeded without the simulated wiring would never collect")
	}
	owner := map[string]string{
		LabelLab: o.Project, LabelLabHost: o.Owner.Host, LabelLabPID: strconv.Itoa(o.Owner.PID), LabelLabPIDNS: o.Owner.PIDNS,
		LabelComposeProject: o.Project,
	}
	if o.Ephemeral {
		owner[LabelLabEphemeral] = "true"
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range owner {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	fleet := Fleet{Scenario: o.Scenario}
	if o.Scale() {
		fleet.Systems = o.Systems
	}
	fleetLabels := fleet.labels()
	volDB, volData := o.Project+"_db", o.Project+"_data"
	spec := Spec{
		Project:       o.Project,
		Owner:         o.Owner,
		Ephemeral:     o.Ephemeral,
		Network:       o.Project + "_default",
		NetworkLabels: with(map[string]string{LabelComposeNetwork: "default", LabelComposeVersion: composeVersionClaimed}),
		Volumes: []Volume{
			{Key: "db", Name: volDB, Holds: "the database",
				Labels: with(map[string]string{LabelComposeVolume: "db", LabelComposeVersion: composeVersionClaimed})},
			{Key: "data", Name: volData, Holds: "the server's data directory: the lab's own encryption key and the bus's store",
				Labels: with(map[string]string{LabelComposeVolume: "data", LabelComposeVersion: composeVersionClaimed})},
		},
	}
	svc := func(name string, oneOff bool) map[string]string {
		of := "False"
		if oneOff {
			of = "True"
		}
		l := with(map[string]string{
			LabelComposeService: name, LabelComposeNumber: "1", LabelComposeOneOff: of,
			LabelComposeWorkDir: o.WorkDir, LabelComposeVersion: composeVersionClaimed,
		})
		maps.Copy(l, fleetLabels)
		return l
	}
	dsn := DSN
	appEnv := []string{
		"OMNIGLASS_DSN=" + dsn,
		"OMNIGLASS_DATA_DIR=" + dataDir,
	}
	spec.DB = Service{
		Name: "db", Container: o.Project + "-db-1", Image: PostgresImage,
		Env:    []string{"POSTGRES_USER=omniglass", "POSTGRES_PASSWORD=omniglass", "POSTGRES_DB=omniglass"},
		Mounts: []Mount{{Volume: volDB, Target: pgDataDir}},
		Health: []string{"CMD-SHELL", "pg_isready -U omniglass -d omniglass"},
		Labels: svc("db", false),
		Role:   "Postgres, the lab's own database. On the lab network only, never published.",
	}
	seedCmd := []string{"lab", "seed", "--scenario", o.Scenario, "--node", NodeName,
		"--user", LoginUser, "--password", LoginPassword}
	if o.NoNode {
		seedCmd = append(seedCmd, "--no-node")
	} else if cadence.ReportSeconds > 0 {
		// A lab with no node has no collection to report, and its pages
		// hold still for the docs shots, so only a lab with its node sets
		// the window.
		seedCmd = append(seedCmd, "--report-seconds", strconv.Itoa(cadence.ReportSeconds))
	}
	if o.PlatformHistory {
		seedCmd = append(seedCmd, "--platform-history")
	}
	pusherTokens := PusherTokensEnv + "=" + FormatPusherTokens(o.Pushers)
	seedEnv := append(append([]string{}, appEnv...), "OMNIGLASS_LAB_NODE_TOKEN_HASH="+o.NodeTokenHash, pusherTokens)
	if o.Scale() {
		seedCmd = []string{"lab", "seed", "--scenario", o.Scenario, "--systems", strconv.Itoa(o.Systems),
			"--user", LoginUser, "--password", LoginPassword}
		if o.InduceUnregistered {
			seedCmd = append(seedCmd, "--induce-unregistered")
		}
		if !o.Ephemeral {
			// A kept scale lab (#1633): the seed keeps the fleet its
			// database already holds and re-enrolls the site nodes.
			seedCmd = append(seedCmd, "--keep")
		}
		seedEnv = append(append([]string{}, appEnv...), SiteNodeTokenHashesEnv+"="+FormatSiteNodeTokenHashes(o.SiteNodes), pusherTokens)
	}
	spec.Seed = Service{
		Name: "seed", Container: o.Project + "-seed-run-1", Image: o.Image,
		Cmd:    seedCmd,
		Env:    seedEnv,
		Mounts: []Mount{{Volume: volData, Target: dataDir}},
		OneOff: true,
		Labels: svc("seed", true),
		Role: "Runs once per lab start: migrates, marks the database as a lab's, seeds the fleet and the " +
			"login on the first run, and enrolls the node afresh.",
	}
	if o.Scale() {
		// The scale lab's measurement reads each row's ingest lag from its
		// commit time (#1556), which Postgres keeps only when asked at start.
		spec.DB.Cmd = []string{"postgres", "-c", "track_commit_timestamp=on"}
	}
	spec.Server = Service{
		Name: "server", Container: o.Project + "-server-1", Image: o.Image,
		Cmd: []string{"server", "--lab"},
		Env: append(append([]string{}, appEnv...),
			"OMNIGLASS_ADDR=0.0.0.0:"+strconv.Itoa(consolePort),
			"OMNIGLASS_NATS_ADDR=0.0.0.0:"+strconv.Itoa(busPort),
			"OMNIGLASS_NATS_URL=nats://server:"+strconv.Itoa(busPort),
			"OMNIGLASS_NATS_STORE_DIR="+dataDir+"/nats",
			"OMNIGLASS_SERVER_NAME="+ServerName,
			// The closed loop's bracket, and the cadence the node runs at,
			// which a server is otherwise not told (#1647).
			"OMNIGLASS_CLOSED_LOOP_BRACKET="+cadence.ClosedLoopBracket,
			"OMNIGLASS_CLOSED_LOOP_HEARTBEAT="+cadence.Heartbeat,
			"OMNIGLASS_CLOSED_LOOP_PROBE_EVERY="+cadence.ProbeEvery),
		Mounts:  []Mount{{Volume: volData, Target: dataDir}},
		Publish: &Publish{HostIP: o.Listen, HostPort: o.Port, ContainerPort: consolePort},
		Restart: "on-failure",
		Labels:  svc("server", false),
		Role: "The control plane and the console, in lab mode. Its console is the one published port; " +
			"its bus listens on the lab network for the node.",
	}
	nodeCmd := []string{"node", "run", "--simulate", "--impairments", "--heartbeat", cadence.Heartbeat,
		"--probe-every", cadence.ProbeEvery, "--worklist-every", cadence.WorklistEvery, "--poll", cadence.SimPoll}
	nodeService := func(service, name, token, role string) Service {
		return Service{
			Name: service, Container: o.Project + "-" + service + "-1", Image: o.Image,
			Cmd: nodeCmd,
			Env: []string{
				"OMNIGLASS_SERVER=http://server:" + strconv.Itoa(consolePort),
				"OMNIGLASS_NODE_NAME=" + name,
				"OMNIGLASS_NODE_TOKEN=" + token,
				simnode.PushersEnv + "=" + simnode.FormatPushers(nodePushers(o.Pushers, name)),
			},
			// Distroless grants an added capability to root only, so the node
			// runs as root inside its own container, to hold NET_ADMIN for the
			// impairments; it reaches that container's namespace, never the
			// host's.
			User:    "0:0",
			CapAdd:  []string{"NET_ADMIN"},
			Sysctls: map[string]string{"net.ipv4.ping_group_range": pingGroupRange},
			Restart: "on-failure",
			Labels:  svc(service, false),
			Role:    role,
		}
	}
	services := []*Service{&spec.DB, &spec.Seed, &spec.Server}
	if o.Scale() {
		// One node per site, each its own container with its own loopback,
		// where its simulator serves that site's components.
		spec.Nodes = make([]Service, len(o.SiteNodes))
		for i, n := range o.SiteNodes {
			spec.Nodes[i] = nodeService(n.Name, n.Name, n.Token, fmt.Sprintf(
				"The edge node %s with the simulator in its own process, serving its site's %d simulated components on its loopback.",
				n.Name, n.Components))
			services = append(services, &spec.Nodes[i])
		}
	} else {
		spec.Node = nodeService("node", NodeName, o.NodeToken,
			"The edge node "+NodeName+" with the simulator in its own process: the simulated components "+
				"sit on its loopback, and it collects from them for real. NET_ADMIN, for the impairments, "+
				"reaches only this container's network.")
		services = append(services, &spec.Node)
	}
	// compose lists a container as its project's only when it carries a
	// config hash (and warns without a config-files label), so each service
	// gets the hash of what it runs, as compose's own would be.
	for _, p := range services {
		p.Labels[LabelComposeHash] = configHash(*p)
		p.Labels[LabelComposeFiles] = ""
	}
	return spec, nil
}

// SiteNodeTokenHashesEnv carries a scale lab's node enrollment token hashes
// to its seed, as FormatSiteNodeTokenHashes writes them.
const SiteNodeTokenHashesEnv = "OMNIGLASS_LAB_SITE_NODE_TOKEN_HASHES"

// FormatSiteNodeTokenHashes writes each site node's token hash as
// name=hash, comma separated; ParseSiteNodeTokenHashes reads it back.
func FormatSiteNodeTokenHashes(nodes []SiteNode) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = n.Name + "=" + n.TokenHash
	}
	return strings.Join(parts, ",")
}

// ParseSiteNodeTokenHashes reads FormatSiteNodeTokenHashes back, by node
// name.
func ParseSiteNodeTokenHashes(s string) (map[string]string, error) {
	out := map[string]string{}
	if s == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		name, hash, ok := strings.Cut(part, "=")
		if !ok || name == "" || hash == "" {
			return nil, fmt.Errorf("lab: %s: %q is not name=hash", SiteNodeTokenHashesEnv, part)
		}
		out[name] = hash
	}
	return out, nil
}

// Pusher is one outside actor a lab runs (#1743): a pusher its scenario
// declared, the profile its emulator device runs, the node whose emulator
// runs it, and the token minted for its principal this run. Token reaches
// that node's env alone; TokenHash and TokenPrefix reach the seed.
type Pusher struct {
	Name, Profile, Node    string
	Token                  string
	TokenHash, TokenPrefix string
}

// nodePushers is the pushers the named node's emulator runs. Pure.
func nodePushers(ps []Pusher, node string) []simnode.Pusher {
	var out []simnode.Pusher
	for _, p := range ps {
		if p.Node == node {
			out = append(out, simnode.Pusher{Name: p.Name, Profile: p.Profile, Token: p.Token})
		}
	}
	return out
}

// checkPusherNodes refuses a pusher planned onto a node the lab does not
// run: its principal would be seeded and nothing would ever push for it.
func checkPusherNodes(o Options) error {
	nodes := map[string]bool{}
	switch {
	case o.Scale():
		for _, n := range o.SiteNodes {
			nodes[n.Name] = true
		}
	case !o.NoNode:
		nodes[NodeName] = true
	}
	for _, p := range o.Pushers {
		if !nodes[p.Node] {
			return fmt.Errorf("lab: pusher %s is planned onto node %s, which this lab does not run", p.Name, p.Node)
		}
	}
	return nil
}

// PusherTokensEnv carries every pusher's token hash and non-secret prefix
// to the seed, as FormatPusherTokens writes them; never a plaintext token.
const PusherTokensEnv = "OMNIGLASS_LAB_PUSHER_TOKENS"

// PusherToken is what the seed sets a pusher's credential from.
type PusherToken struct {
	Hash, Prefix string
}

// FormatPusherTokens writes each pusher as name=hash:prefix, comma
// separated; ParsePusherTokens reads it back.
func FormatPusherTokens(ps []Pusher) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.Name + "=" + p.TokenHash + ":" + p.TokenPrefix
	}
	return strings.Join(parts, ",")
}

// ParsePusherTokens reads FormatPusherTokens back, by pusher name. A part
// missing a field, or a pusher given twice, is refused. Pure.
func ParsePusherTokens(s string) (map[string]PusherToken, error) {
	out := map[string]PusherToken{}
	if s == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		name, rest, ok := strings.Cut(part, "=")
		hash, prefix, ok2 := strings.Cut(rest, ":")
		if !ok || !ok2 || name == "" || hash == "" || prefix == "" {
			return nil, fmt.Errorf("lab: %s: %q is not name=hash:prefix", PusherTokensEnv, part)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("lab: %s: pusher %q is given twice", PusherTokensEnv, name)
		}
		out[name] = PusherToken{Hash: hash, Prefix: prefix}
	}
	return out, nil
}

// configHash is a service's compose config hash: the sha256 of what it runs
// (its image, command, user, capabilities and sysctls; never its env, which
// carries the node's token).
func configHash(s Service) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%q\x00%s\x00%q", s.Image, s.Cmd, s.User, s.CapAdd)
	for _, k := range SortedKeys(s.Sysctls) {
		fmt.Fprintf(h, "\x00%s=%s", k, s.Sysctls[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SortedKeys is a map's keys in order, for stable rendering.
func SortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// RestartCommand is the command that brings this lab back with its data, for
// the stop line's hint. A learner's lab comes back with a bare lab run; a
// kept scale lab (#1633) needs its own scenario, size and --keep (and its
// cadence, when not the lab's own, and its project, when not the scale lab's
// own, since the data lives in that project's volumes), or a bare run would
// start the dev fleet.
func RestartCommand(o Options) string {
	cmd := "omniglass lab run"
	if !o.Scale() {
		return cmd
	}
	cmd += " --scenario " + o.Scenario + " --systems " + strconv.Itoa(o.Systems)
	if o.Cadence != "" && o.Cadence != CadenceLab {
		cmd += " --cadence " + o.Cadence
	}
	if o.Project != "" && o.Project != ScaleProject {
		cmd += " --project " + o.Project
	}
	if !o.Ephemeral {
		cmd += " --keep"
	}
	return cmd
}
