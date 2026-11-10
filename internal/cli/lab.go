package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
	labscenario "github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// newLabCmd is `omniglass lab`: a working omniglass on Docker with one
// command, to learn on (#1193). `lab run` is the run mode (ADR-0058: a run
// mode is a verb under its noun); `lab seed` is its one-off, run inside the
// lab's own image, and hidden.
func newLabCmd(version string) *cobra.Command {
	group := &cobra.Command{
		Use:   "lab",
		Short: "A working omniglass on Docker, to learn on",
	}
	group.AddCommand(newLabRunCmd(version), newLabMeasureCmd(), newLabSeedCmd())
	return group
}

func newLabRunCmd(version string) *cobra.Command {
	var (
		port     int
		listen   string
		fresh    bool
		scenario string
		image    string
		project  string
		systems  int
		plan     bool
		script   labScript
		scale    labScale
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Bring up a lab: omniglass, its database and a node collecting from a simulated fleet, until Ctrl-C",
		Long: "Brings up a working omniglass on this machine's Docker as one compose project of three containers: " +
			"Postgres, the server, and the edge node edge-hq running the simulator in its own process, serving the " +
			"seeded dev fleet. The node collects from the fleet for real, and impairments work. It prints what it is " +
			"doing while it comes up, then the console's address and the login, and holds until Ctrl-C, which removes " +
			"the containers and the network and keeps the data for the next run.\n\n" +
			"Docker is the only requirement. Only the console is published, on 127.0.0.1 (--listen moves it, and says " +
			"what that exposes). A binary built by make runs the image the same make built; a release binary runs its " +
			"release's image. The lab's database is marked as a lab's, and a lab and a real server refuse each other's " +
			"database.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sigs := []os.Signal{os.Interrupt, syscall.SIGTERM}
			if script.whilePID != 0 {
				// A lab held for a script is the script's to stop, with
				// one SIGTERM from its trap: the terminal's Ctrl-C and
				// hangup, which reach the whole process group, are the
				// script's, a second signal would cut the teardown short,
				// and a reader of its output that went away (the script's
				// prefixer, a tee) must not end it before it tears down.
				signal.Ignore(os.Interrupt, syscall.SIGHUP, syscall.SIGPIPE)
				sigs = sigs[1:]
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), sigs...)
			defer stop()
			// A second Ctrl-C during teardown kills the process: the next run
			// reaps what is left.
			go func() { <-ctx.Done(); stop() }()
			return runLab(ctx, cmd.OutOrStdout(), version, labFlags{
				port: port, listen: listen, fresh: fresh, scenario: scenario, image: image, project: project,
				systems: systems, plan: plan, script: script, scale: scale,
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", lab.DefaultPort, "the console's port on this machine; 0 lets Docker pick a free one, printed when the lab is ready")
	cmd.Flags().StringVar(&listen, "listen", lab.DefaultListen, "the address the console is published on; anything but loopback exposes the lab, and its well-known login, to your network")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "remove the lab's data first and start over from the seed")
	cmd.Flags().StringVar(&scenario, "scenario", lab.DefaultScenario, "the fleet the lab seeds on its first run (the seed-dev command lists them), or a scale lab scenario ("+strings.Join(labscenario.Names(), ", ")+") sized by --systems")
	// The scale lab (#1357): a scenario manifest resolved at a size, brought
	// up as one node per site (#1554), or printed with --plan.
	cmd.Flags().IntVar(&systems, "systems", 0, "how many systems a scale lab scenario resolves to")
	cmd.Flags().BoolVar(&plan, "plan", false, "print the scale lab scenario's resolved plan (sites, systems per standard, components per product) and exit, starting nothing")
	cmd.Flags().StringVar(&image, "image", "", "run this omniglass image instead of the one this binary was built with (env OMNIGLASS_LAB_IMAGE); one that is not this binary's own build is warned about")
	cmd.Flags().StringVar(&project, "project", lab.DefaultProject, "the Docker compose project name")
	_ = cmd.Flags().MarkHidden("project")
	// For scripts, not learners (the demo pipeline's recorders and the
	// probe, web/e2e/lib/lab.sh, #1197): a throwaway lab of its own project,
	// held for the script's process, saying how to reach it in a file.
	cmd.Flags().BoolVar(&script.ephemeral, "ephemeral", false, "a throwaway lab: its data goes when it stops (needs its own --project)")
	cmd.Flags().StringVar(&script.readyFile, "ready-file", "", "once ready, write how to reach the lab here, as shell assignments")
	cmd.Flags().IntVar(&script.whilePID, "while-pid", 0, "stop the lab once this process is gone")
	// The docs screenshots (#1267) photograph pages that must hold still: a
	// fleet with no node collecting from it.
	cmd.Flags().BoolVar(&script.noNode, "no-node", false, "run no node: seed the fleet without the simulated wiring, so nothing collects (needs --ephemeral)")
	// The lab footprint (#1277) measures a lab at a chosen cadence profile.
	cmd.Flags().StringVar(&script.cadence, "cadence", "", "the node's cadence profile ("+lab.CadenceNames()+"); empty is lab, or walkthrough for a throwaway lab")
	// The scale lab's knobs for proving it: a forced low memory limit shows
	// the refusal without a small machine, and the bring-up's bound.
	cmd.Flags().StringVar(&scale.hostMemory, "host-memory", "", "a scale lab: plan against this much host memory (e.g. 4g) instead of what Docker reports")
	cmd.Flags().DurationVar(&scale.timeout, "ready-timeout", defaultScaleTimeout, "a scale lab: how long its bring-up may take before it is removed, naming what is not up")
	// The delivery table's proof that a refusal reads as loss (#1698).
	cmd.Flags().BoolVar(&scale.induce, "induce-unregistered", false, "a scale lab: make one simulated device answer a property key no driver registers, so its samples are refused (refused-unregistered)")
	for _, name := range []string{"ephemeral", "ready-file", "while-pid", "no-node", "cadence", "host-memory", "ready-timeout", "induce-unregistered"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}

// runLabScenario resolves a scale lab scenario manifest at --systems and,
// with --plan, prints the plan and starts nothing. Without --plan it brings
// the plan up (#1554): Postgres, the server, and one node per site, each
// simulating its site's components, after a check that the host can fit it.
func runLabScenario(ctx context.Context, out io.Writer, m *labscenario.Manifest, f labFlags) error {
	if f.systems == 0 {
		return fmt.Errorf("scenario %s is a scale lab scenario: say how many systems with --systems N (%d to %d)", m.Name, labscenario.MinSystems(m), labscenario.MaxSystems)
	}
	cat, err := labscenario.ShippedCatalog()
	if err != nil {
		return err
	}
	plan, err := labscenario.Resolve(m, cat, f.systems)
	if err != nil {
		return err
	}
	hostMemory, err := parseByteSize(f.scale.hostMemory)
	if err != nil {
		return fmt.Errorf("--host-memory: %w", err)
	}
	scale := &lab.ScaleRun{
		Shape: func(n int) (int, int, error) {
			p, err := labscenario.Resolve(m, cat, n)
			if err != nil {
				return 0, 0, err
			}
			return len(p.Sites), p.Totals.Components, nil
		},
		MinSystems: labscenario.MinSystems(m), MaxSystems: labscenario.MaxSystems,
		Cost: lab.MeasuredCost, HostMemory: hostMemory, Timeout: f.scale.timeout,
	}
	if f.plan {
		if err := labscenario.WritePlan(out, plan); err != nil {
			return err
		}
		readHost := f.readHost
		if readHost == nil {
			readHost = scaleHost
		}
		return writeScaleCapacity(ctx, out, scale, plan, readHost)
	}
	nodes := make([]lab.SiteNode, len(plan.Sites))
	for i, site := range plan.Sites {
		token, hash, _, err := auth.NewBearerToken()
		if err != nil {
			return err
		}
		nodes[i] = lab.SiteNode{Name: labscenario.SiteNode(site.Name), Token: token, TokenHash: hex.EncodeToString(hash),
			Components: site.Totals.Components}
	}
	project := f.project
	if project == lab.DefaultProject {
		project = lab.ScaleProject
	}
	cadence := f.script.cadence
	if cadence == "" {
		cadence = lab.CadenceLab
	}
	fmt.Fprintf(out, "Starting a scale lab: scenario %s at %d systems, %d sites (a node each), %d simulated components.\n",
		m.Name, plan.Totals.Systems, len(plan.Sites), plan.Totals.Components)
	fmt.Fprintln(out)
	eng, daemon, err := labEngine(ctx, out)
	if err != nil {
		return err
	}
	choice := lab.SelectImage(f.version, labImage(f))
	r := labRunner(eng, daemon, out)
	r.Poll, r.ReadyTimeout = 3*time.Second, f.scale.timeout
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	_, err = r.Run(ctx, lab.Config{
		Options: lab.Options{
			Project: project, Listen: f.listen, Port: f.port, Image: choice.Ref, Scenario: m.Name,
			Systems: plan.Totals.Systems, SiteNodes: nodes,
			Owner: lab.Owner{Host: host, PID: os.Getpid(), PIDNS: lab.CurrentPIDNS()}, WorkDir: wd,
			Ephemeral: true, Cadence: cadence, InduceUnregistered: f.scale.induce,
		},
		Image: choice, Hold: true, ReadyFile: f.script.readyFile, WhilePID: f.script.whilePID,
		ScaleRun: scale,
	})
	return err
}

// writeScaleCapacity follows a plan with its preflight: the estimate from
// the measured costs and, when the Docker host can be read, the decision
// bring-up would make on it. It starts nothing, and a refusal is printed, not
// returned: the plan was asked for, not the lab.
func writeScaleCapacity(ctx context.Context, out io.Writer, scale *lab.ScaleRun, plan *labscenario.Plan, readHost hostReader) error {
	need := scale.Cost.Estimate(len(plan.Sites), plan.Totals.Components)
	fmt.Fprintf(out, "\nPreflight: about %s of memory and %.1f cores (the server and Postgres, a node for each of %d sites, %d simulated components).\n",
		lab.FormatMiB(need.MemMiB), need.CPU, len(plan.Sites), plan.Totals.Components)
	host, err := readHost(ctx, scale.HostMemory)
	if err != nil {
		fmt.Fprintf(out, "No decision: %v\n", err)
		return nil
	}
	c, err := lab.AssessCapacity(*scale, host, plan.Totals.Systems, len(plan.Sites), plan.Totals.Components)
	if err != nil {
		return err
	}
	use := host.Usable()
	fmt.Fprintf(out, "This Docker host can spare %s and %.1f cores (%.0f%% of its %s and %d cores).\n",
		lab.FormatMiB(use.MemMiB), use.CPU, (1-lab.Headroom)*100, lab.FormatMiB(float64(host.MemBytes)/(1<<20)), host.CPUs)
	if err := c.Err(); err != nil {
		fmt.Fprintf(out, "Decision: refused. %v\n", err)
		return nil
	}
	fmt.Fprintln(out, "Decision: fits.")
	return nil
}

// hostReader reads the host a scale lab plans against, with its memory
// forced when forcedMem is set.
type hostReader func(ctx context.Context, forcedMem int64) (lab.HostResources, error)

// scaleHost reads this machine's Docker host for a plan's preflight,
// quietly; a Docker that does not answer is the error.
func scaleHost(ctx context.Context, forcedMem int64) (lab.HostResources, error) {
	eng, _, err := labEngine(ctx, io.Discard)
	if err != nil {
		return lab.HostResources{}, err
	}
	return lab.ScaleHost(ctx, eng, forcedMem)
}

// defaultScaleTimeout bounds a scale lab's bring-up, the seed included.
const defaultScaleTimeout = 20 * time.Minute

// labScale are a scale lab's proving knobs.
type labScale struct {
	hostMemory string
	timeout    time.Duration
	// induce makes one simulated device answer a property key no driver
	// registers (#1698), so the delivery table shows a refusal as loss.
	induce bool
}

// parseByteSize reads a size like 512m or 4g (binary units; a bare number is
// bytes). Empty is zero.
func parseByteSize(in string) (int64, error) {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1 << 10
	case strings.HasSuffix(s, "m"):
		mult = 1 << 20
	case strings.HasSuffix(s, "g"):
		mult = 1 << 30
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q is not a size like 512m or 4g", in)
	}
	return n * mult, nil
}

// labScript are lab run's script-only settings.
type labScript struct {
	ephemeral bool
	noNode    bool
	readyFile string
	whilePID  int
	cadence   string
}

type labFlags struct {
	port                             int
	listen, scenario, image, project string
	fresh                            bool
	systems                          int
	plan                             bool
	script                           labScript
	scale                            labScale
	version                          string
	// readHost, when set, stands in for this machine's Docker host in a
	// plan's preflight.
	readHost hostReader
}

func runLab(ctx context.Context, out io.Writer, version string, f labFlags) error {
	f.version = version
	if m, err := labscenario.Shipped(f.scenario); err == nil {
		return runLabScenario(ctx, out, m, f)
	}
	sc, err := devseed.LookupScenario(f.scenario)
	if err != nil {
		return fmt.Errorf("%w; scale lab scenarios: %s", err, strings.Join(labscenario.Names(), ", "))
	}
	if f.systems != 0 || f.plan {
		return fmt.Errorf("--systems and --plan size a scale lab scenario (%s); %q is a seeded fleet, which has its own size",
			strings.Join(labscenario.Names(), ", "), f.scenario)
	}
	if f.scale.induce {
		return fmt.Errorf("--induce-unregistered is a scale lab's (%s); %q is a seeded fleet", strings.Join(labscenario.Names(), ", "), f.scenario)
	}
	faults, err := devseed.PlantedFaults(sc)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Starting your omniglass lab. The first run takes a few minutes; later ones, under one.")
	fmt.Fprintln(out)

	eng, daemon, err := labEngine(ctx, out)
	if err != nil {
		return err
	}
	choice := lab.SelectImage(version, labImage(f))
	token, hash, _, err := auth.NewBearerToken()
	if err != nil {
		return err
	}
	// The simulated pusher's token (#1370), minted per run like the node's.
	pushToken, pushHash, pushPrefix, err := auth.NewBearerToken()
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	r := labRunner(eng, daemon, out)
	_, err = r.Run(ctx, lab.Config{
		Options: lab.Options{
			Project: f.project, Listen: f.listen, Port: f.port, Image: choice.Ref, Scenario: sc.Name,
			NodeToken: token, NodeTokenHash: hex.EncodeToString(hash),
			PushToken: pushToken, PushTokenHash: hex.EncodeToString(pushHash), PushTokenPrefix: pushPrefix,
			Owner: lab.Owner{Host: host, PID: os.Getpid(), PIDNS: lab.CurrentPIDNS()}, WorkDir: wd,
			Ephemeral: f.script.ephemeral, NoNode: f.script.noNode, Cadence: f.script.cadence,
		},
		Image: choice, Fresh: f.fresh, PlantedFaults: plantedLabels(faults), Hold: true,
		ReadyFile: f.script.readyFile, WhilePID: f.script.whilePID,
	})
	return err
}

// labEngine finds this machine's Docker and connects to it, refusing one on
// another machine, with the cause and the fix for each failure.
func labEngine(ctx context.Context, out io.Writer) (lab.Engine, lab.Daemon, error) {
	home, _ := os.UserHomeDir()
	daemon, err := lab.ResolveDaemon(lab.Env{Getenv: config.Get, ReadFile: os.ReadFile, Home: home, GOOS: runtime.GOOS})
	if err != nil {
		return nil, daemon, &lab.PreflightError{Cause: "Could not tell which Docker to use: " + err.Error(),
			Fix: "check DOCKER_HOST, DOCKER_CONTEXT and `docker context ls`.", Err: err}
	}
	if !lab.IsLocal(daemon.Host) {
		return nil, daemon, lab.RemoteDaemon(daemon)
	}
	if err := dialDaemon(daemon.Host); err != nil {
		return nil, daemon, lab.ClassifyPing(daemon, err)
	}
	eng, err := lab.NewDockerEngine(daemon.Host)
	if err != nil {
		return nil, daemon, lab.ClassifyPing(daemon, err)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	info, err := eng.Ping(pctx)
	cancel()
	if err != nil {
		return nil, daemon, lab.ClassifyPing(daemon, err)
	}
	fmt.Fprintf(out, "  %-10s %s\n", "docker", fmt.Sprintf("running (Engine API %s, %s, from %s)", info.APIVersion, daemon.Host, daemon.Source))
	return eng, daemon, nil
}

// labImage is the image the flags or the environment ask for, empty for
// this binary's own.
func labImage(f labFlags) string {
	if f.image != "" {
		return f.image
	}
	return config.Get("OMNIGLASS_LAB_IMAGE")
}

// labRunner is the runner over this machine's Docker.
func labRunner(eng lab.Engine, daemon lab.Daemon, out io.Writer) *lab.Runner {
	host, _ := os.Hostname()
	return &lab.Runner{
		Engine: eng, Daemon: daemon, Out: out, Host: host,
		Alive: lab.ProcessAlive, PIDNS: lab.CurrentPIDNS(), PortFree: lab.PortFree,
		HTTP: &http.Client{Timeout: 10 * time.Second}, Poll: time.Second, ReadyTimeout: 4 * time.Minute, RestartGrace: 30 * time.Second,
	}
}

// dialDaemon reaches a socket daemon directly, so a missing socket, a
// permission refusal and a refused connection keep their cause for the
// preflight message (the client reports all three alike). Other transports
// are left to the ping.
func dialDaemon(host string) error {
	u, err := url.Parse(host)
	if err != nil || u.Scheme != "unix" {
		return nil
	}
	if _, err := os.Stat(u.Path); err != nil {
		return err
	}
	c, err := net.DialTimeout("unix", u.Path, 5*time.Second)
	if err != nil {
		return err
	}
	return c.Close()
}

func newLabSeedCmd() *cobra.Command {
	var scenario, node, user, password string
	var noNode, induce bool
	var systems, reportSeconds int
	cmd := &cobra.Command{
		Use:    "seed",
		Short:  "The lab's one-off: migrate, mark the database as a lab's, seed it on the first run, and enroll the node",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if systems > 0 {
				return runLabScaleSeed(cmd.Context(), cmd.OutOrStdout(), scenario, systems, user, password, config.Get(lab.SiteNodeTokenHashesEnv), induce)
			}
			if induce {
				return errors.New("--induce-unregistered is a scale lab's: it needs --systems")
			}
			return runLabSeed(cmd.Context(), cmd.OutOrStdout(), scenario, node, user, password, config.Get("OMNIGLASS_LAB_NODE_TOKEN_HASH"), noNode,
				pushCredential{Hash: config.Get("OMNIGLASS_LAB_PUSH_TOKEN_HASH"), Prefix: config.Get("OMNIGLASS_LAB_PUSH_TOKEN_PREFIX")}, reportSeconds)
		},
	}
	cmd.Flags().StringVar(&scenario, "scenario", lab.DefaultScenario, "the fleet to seed on the first run")
	cmd.Flags().StringVar(&node, "node", lab.NodeName, "the node to enroll")
	cmd.Flags().StringVar(&user, "user", lab.LoginUser, "the owner's username")
	cmd.Flags().StringVar(&password, "password", lab.LoginPassword, "the owner's password")
	cmd.Flags().BoolVar(&noNode, "no-node", false, "seed the fleet without the simulated wiring and enroll no node, so the lab runs none")
	cmd.Flags().IntVar(&reportSeconds, "report-seconds", 0, "set the platform's collection report window (collection.report_seconds) to this many seconds; 0 leaves it as it is")
	cmd.Flags().IntVar(&systems, "systems", 0, "seed the scale lab scenario --scenario resolved at this many systems, with a node per site")
	cmd.Flags().BoolVar(&induce, "induce-unregistered", false, "with --systems: make one simulated device answer a property key no driver registers, so its samples are refused (refused-unregistered)")
	return cmd
}

// runLabSeed is the lab's seed one-off, in the trusted direct-DB lane like
// bootstrap and seed-dev. It refuses a database that holds data and no lab
// mark, marks a fresh one, seeds the fleet and the owner only on the first
// run (a later run keeps the estate), makes sure the login works, and
// enrolls the node with the hash of the token the lab minted for this run.
// With noNode (a throwaway lab whose pages hold still, #1267) it seeds the
// fleet without the simulated wiring, which nothing would collect from, and
// enrolls no node, so the lab runs none.
// pushCredential is the simulated pusher's token for this run (#1370): its
// hash and non-secret prefix, never the token itself. Empty, none is set.
type pushCredential struct{ Hash, Prefix string }

// reportSeconds, when above zero, sets the platform's collection report
// window (#1646) before the server and the node start, so both read it for
// their first window: a walkthrough lab's 10s, where the platform's 60s would
// land the first figures as its recording ends.
func runLabSeed(ctx context.Context, out io.Writer, scenario, node, user, password, nodeTokenHash string, noNode bool, push pushCredential, reportSeconds int) error {
	sc, err := devseed.LookupScenario(scenario)
	if err != nil {
		return err
	}
	gw, st, err := openLabDB(ctx)
	if err != nil {
		return err
	}
	defer gw.Close()
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	summary := "kept this lab's fleet from its last run"
	if !st.HoldsData {
		if err := bootstrapLabOwner(ctx, gw, user, pwHash); err != nil {
			return err
		}
		if err := devseed.RunScenario(ctx, gw, "", sc, devseed.Options{SimFleet: !noNode}); err != nil {
			return err
		}
		summary = fmt.Sprintf("the %s fleet, seeded on this lab's first run", sc.Name)
	} else if ok, err := gw.SetPassword(ctx, user, pwHash); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("this lab has no user %q to sign in as; start over with omniglass lab run --fresh", user)
	}
	if reportSeconds > 0 {
		if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": reportSeconds}); err != nil {
			return err
		}
	}
	all := scope.Set{All: true}
	// With no node to run, none is enrolled.
	present := false
	if !noNode {
		present = true
		if _, err := gw.GetNode(ctx, node, all); errors.Is(err, storage.ErrNodeNotFound) {
			present = false
		} else if err != nil {
			return err
		}
	}
	if present {
		if nodeTokenHash == "" {
			return errors.New("lab seed: OMNIGLASS_LAB_NODE_TOKEN_HASH is not set")
		}
		if _, err := gw.SetEnrollmentToken(ctx, "", node, nodeTokenHash, all); err != nil {
			return err
		}
		if err := setPushToken(ctx, out, gw, push); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, lab.FormatSeedResult(lab.SeedResult{Node: present, Summary: summary}))
	return nil
}

// openLabDB is the lab seed's way into its database: it refuses one that
// holds data and no lab mark (before migrating it), migrates, opens the
// Gateway, marks the database as a lab's and seeds the reference data. It
// reports the state found, so a first run is told from a later one.
func openLabDB(ctx context.Context) (*storage.PG, storage.LabState, error) {
	c := cfg()
	// Judge the database before migrating it: a real database is refused as
	// it is, never first moved to the lab's schema.
	if err := labSeedGate(ctx, c.DSN); err != nil {
		return nil, storage.LabState{}, err
	}
	if err := migrate.Run(c.DSN); err != nil {
		return nil, storage.LabState{}, err
	}
	kek, _, err := secret.LoadKEK(config.Get, c.DataDir, func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) })
	if err != nil {
		return nil, storage.LabState{}, err
	}
	gw, err := storage.NewPG(ctx, c.DSN, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		return nil, storage.LabState{}, err
	}
	st, err := gw.LabState(ctx)
	if err == nil {
		err = storage.CheckLabMark(true, st.Marked, st.HoldsData)
	}
	if err == nil {
		err = gw.MarkLab(ctx)
	}
	if err == nil {
		err = seed.Run(ctx, gw)
	}
	if err != nil {
		gw.Close()
		return nil, storage.LabState{}, err
	}
	return gw, st, nil
}

// bootstrapLabOwner creates the lab's owner, whose password is the lab's
// login.
func bootstrapLabOwner(ctx context.Context, gw storage.Gateway, user string, pwHash string) error {
	_, tokHash, prefix, err := auth.NewBearerToken()
	if err != nil {
		return err
	}
	_, err = gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: user, SecretHash: tokHash, Prefix: prefix, PasswordHash: pwHash})
	return err
}

// runLabScaleSeed is a scale lab's seed (#1554): the scale lab scenario
// resolved at systems, laid down by devseed.SeedPlan (the one function the
// bulk seed, #1555, replaces) on a throwaway lab's empty database, and each
// site's node enrolled with the hash of the token the lab minted for it.
// With induce (#1698), one device answers a property key no driver
// registers, named in the summary.
func runLabScaleSeed(ctx context.Context, out io.Writer, scenario string, systems int, user, password, tokenHashes string, induce bool) error {
	m, err := labscenario.Shipped(scenario)
	if err != nil {
		return err
	}
	cat, err := labscenario.ShippedCatalog()
	if err != nil {
		return err
	}
	plan, err := labscenario.Resolve(m, cat, systems)
	if err != nil {
		return err
	}
	hashes, err := lab.ParseSiteNodeTokenHashes(tokenHashes)
	if err != nil {
		return err
	}
	// Every site's node needs its hash, checked before the database is touched.
	for _, site := range plan.Sites {
		if node := labscenario.SiteNode(site.Name); hashes[node] == "" {
			return fmt.Errorf("lab seed: %s carries no token hash for the node %s", lab.SiteNodeTokenHashesEnv, node)
		}
	}
	gw, st, err := openLabDB(ctx)
	if err != nil {
		return err
	}
	defer gw.Close()
	if st.HoldsData {
		return errors.New("lab seed: a scale lab seeds an empty database, and this one holds data; a scale lab is a throwaway lab, so start it again")
	}
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := bootstrapLabOwner(ctx, gw, user, pwHash); err != nil {
		return err
	}
	start := time.Now()
	all := scope.Set{All: true}
	// A site's node must exist before its endpoints name it, and the site it
	// sits at does not until the plan is seeded: create each node first, place
	// it at its site after.
	for _, site := range plan.Sites {
		node := labscenario.SiteNode(site.Name)
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{
			Name: node, Label: siteNodeLabel(site) + " Edge Node", Description: "the scale lab's node for " + site.Name,
		}, all, all); err != nil {
			return fmt.Errorf("lab seed: create site node %s: %w", node, err)
		}
	}
	seeded, err := devseed.SeedPlan(ctx, gw, "", plan, devseed.PlanOptions{Endpoints: true, NodeFor: labscenario.SiteNode, InduceUnregistered: induce})
	if err != nil {
		return err
	}
	for _, site := range plan.Sites {
		node, at := labscenario.SiteNode(site.Name), site.Name
		if _, err := gw.UpdateNode(ctx, "", node, storage.NodePatch{LocationName: &at}, all, all, all); err != nil {
			return fmt.Errorf("lab seed: place %s at %s: %w", node, site.Name, err)
		}
		if _, err := gw.SetEnrollmentToken(ctx, "", node, hashes[node], all); err != nil {
			return err
		}
	}
	summary := fmt.Sprintf("scenario %s at %d systems: %d sites, %d components, seeded in %s",
		m.Name, plan.Totals.Systems, len(plan.Sites), plan.Totals.Components, time.Since(start).Round(time.Second))
	if in := seeded.Induced; in != nil {
		summary += fmt.Sprintf("; %s answers %s through %s, refused-unregistered", in.Component, in.Key, in.Driver)
	}
	fmt.Fprintln(out, lab.FormatSeedResult(lab.SeedResult{Node: true, Summary: summary}))
	return nil
}

// siteNodeLabel is what a site is called on its node: its label, else its name.
func siteNodeLabel(site labscenario.SitePlan) string {
	if site.Label != "" {
		return site.Label
	}
	return site.Name
}

func plantedLabels(fs []devseed.PlantedFault) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Label)
	}
	return out
}

// labSeedGate is the seed's refusal of a database that holds data and no lab
// mark, read before any migration.
func labSeedGate(ctx context.Context, dsn string) error {
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		return err
	}
	defer gw.Close()
	st, err := gw.LabState(ctx)
	if err != nil {
		return err
	}
	return storage.CheckLabMark(true, st.Marked, st.HoldsData)
}

// setPushToken gives the simulated pusher (#1370) this run's token, replacing
// the last run's, so the simulator beside the node can push with it. A fleet
// with no pusher, or a run with no token, sets none.
func setPushToken(ctx context.Context, out io.Writer, gw storage.Gateway, push pushCredential) error {
	if push.Hash == "" {
		return nil
	}
	doc, err := devseed.Fixtures()
	if err != nil {
		return err
	}
	if doc.SimPusher == nil {
		return nil
	}
	hash, err := hex.DecodeString(push.Hash)
	if err != nil {
		return fmt.Errorf("lab seed: OMNIGLASS_LAB_PUSH_TOKEN_HASH: %w", err)
	}
	ok, err := gw.SetServiceToken(ctx, "", doc.SimPusher.Service, hash, push.Prefix)
	if err != nil {
		return err
	}
	if !ok {
		// A lab kept from before the pusher (#1370): its fleet was seeded
		// without one, and a kept lab is never seeded again.
		fmt.Fprintf(out, "note: this lab's fleet predates the simulated booking service (%s), so nothing pushes; omniglass lab run --fresh adds it\n", doc.SimPusher.Service)
	}
	return nil
}
