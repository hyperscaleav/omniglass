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
	"net"
	"sort"
	"strconv"
)

// Defaults for the options a user does not set.
const (
	DefaultProject  = "omniglass-lab"
	DefaultPort     = 8080
	DefaultListen   = "127.0.0.1"
	DefaultScenario = "dev"
	// The node the dev seed places every simulated component on.
	NodeName = "edge-hq"
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
	// The cadence every walkthrough is recorded at: a change in the console shows in the
	// lab within seconds. The product defaults are unchanged.
	labCadence = "2s"
	// How often a learner's lab probes reachability (#1273). It may run for
	// days, and a probe on every 2s check-in wrote most of its rows; a
	// script's throwaway lab lives for minutes and probes on every check-in,
	// so a walkthrough waiting on a probe's verdict keeps its recorded budget.
	labProbeEvery = "10s"
	// The unprivileged pinger needs the whole group range admitted.
	pingGroupRange = "0 2147483647"
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
	LabelLabEphemeral     = "omniglass.lab.ephemeral"
	composeVersionClaimed = "2.0.0"
)

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
	Owner         Owner
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
}

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
	Node          Service
}

// Services are the project's containers in start order.
func (s Spec) Services() []Service { return []Service{s.DB, s.Seed, s.Server, s.Node} }

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
		return with(map[string]string{
			LabelComposeService: name, LabelComposeNumber: "1", LabelComposeOneOff: of,
			LabelComposeWorkDir: o.WorkDir, LabelComposeVersion: composeVersionClaimed,
		})
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
	}
	spec.Seed = Service{
		Name: "seed", Container: o.Project + "-seed-run-1", Image: o.Image,
		Cmd:    seedCmd,
		Env:    append(append([]string{}, appEnv...), "OMNIGLASS_LAB_NODE_TOKEN_HASH="+o.NodeTokenHash),
		Mounts: []Mount{{Volume: volData, Target: dataDir}},
		OneOff: true,
		Labels: svc("seed", true),
		Role: "Runs once per lab start: migrates, marks the database as a lab's, seeds the fleet and the " +
			"login on the first run, and enrolls the node afresh.",
	}
	spec.Server = Service{
		Name: "server", Container: o.Project + "-server-1", Image: o.Image,
		Cmd: []string{"server", "--lab"},
		Env: append(append([]string{}, appEnv...),
			"OMNIGLASS_ADDR=0.0.0.0:"+strconv.Itoa(consolePort),
			"OMNIGLASS_NATS_ADDR=0.0.0.0:"+strconv.Itoa(busPort),
			"OMNIGLASS_NATS_URL=nats://server:"+strconv.Itoa(busPort),
			"OMNIGLASS_NATS_STORE_DIR="+dataDir+"/nats"),
		Mounts:  []Mount{{Volume: volData, Target: dataDir}},
		Publish: &Publish{HostIP: o.Listen, HostPort: o.Port, ContainerPort: consolePort},
		Restart: "on-failure",
		Labels:  svc("server", false),
		Role: "The control plane and the console, in lab mode. Its console is the one published port; " +
			"its bus listens on the lab network for the node.",
	}
	nodeCmd := []string{"node", "run", "--simulate", "--impairments", "--heartbeat", labCadence}
	if !o.Ephemeral {
		nodeCmd = append(nodeCmd, "--probe-every", labProbeEvery)
	}
	spec.Node = Service{
		Name: "node", Container: o.Project + "-node-1", Image: o.Image,
		Cmd: append(nodeCmd, "--poll", labCadence),
		Env: []string{
			"OMNIGLASS_SERVER=http://server:" + strconv.Itoa(consolePort),
			"OMNIGLASS_NODE_NAME=" + NodeName,
			"OMNIGLASS_NODE_TOKEN=" + o.NodeToken,
		},
		// Distroless grants an added capability to root only, so the node
		// runs as root inside its own container, to hold NET_ADMIN for the
		// impairments; it reaches that container's namespace, never the
		// host's.
		User:    "0:0",
		CapAdd:  []string{"NET_ADMIN"},
		Sysctls: map[string]string{"net.ipv4.ping_group_range": pingGroupRange},
		Restart: "on-failure",
		Labels:  svc("node", false),
		Role: "The edge node " + NodeName + " with the simulator in its own process: the simulated components " +
			"sit on its loopback, and it collects from them for real. NET_ADMIN, for the impairments, " +
			"reaches only this container's network.",
	}
	// compose lists a container as its project's only when it carries a
	// config hash (and warns without a config-files label), so each service
	// gets the hash of what it runs, as compose's own would be.
	for _, p := range []*Service{&spec.DB, &spec.Seed, &spec.Server, &spec.Node} {
		p.Labels[LabelComposeHash] = configHash(*p)
		p.Labels[LabelComposeFiles] = ""
	}
	return spec, nil
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
