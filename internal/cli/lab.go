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
	"path/filepath"
	"runtime"
	"slices"
	"sort"
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
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// newLabCmd is `omniglass lab`: a working omniglass on Docker with one
// command, to learn on (#1193). `lab run` is the run mode (ADR-0058: a run
// mode is a verb under its noun); `lab seed` is its one-off, run inside the
// lab's own image, and hidden.
func newLabCmd(version, imageDigest string) *cobra.Command {
	group := &cobra.Command{
		Use:   "lab",
		Short: "A working omniglass on Docker, to learn on",
	}
	group.AddCommand(newLabRunCmd(version, imageDigest), newLabMeasureCmd(), newLabPerfCmd(), newLabSoakCmd(), newLabSeedCmd())
	return group
}

func newLabRunCmd(version, imageDigest string) *cobra.Command {
	var (
		port     int
		listen   string
		fresh    bool
		keep     bool
		scenario string
		image    string
		project  string
		systems  int
		plan     bool
		keyEnv   string
		replay   string
		script   labScript
		scale    labScale
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Bring up a lab: omniglass, its database and an outpost collecting from a simulated fleet, until Ctrl-C",
		Long: "Brings up a working omniglass on this machine's Docker as one compose project of three containers: " +
			"Postgres, the server, and the edge outpost edge-hq running the simulator in its own process, serving the " +
			"seeded dev fleet. The outpost collects from the fleet for real, and impairments work. It prints what it is " +
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
				imageDigest: imageDigest, port: port, listen: listen, fresh: fresh, keep: keep, scenario: scenario, image: image, project: project,
				systems: systems, plan: plan, script: script, scale: scale, omnibotKeyEnv: keyEnv, omnibotReplay: replay,
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", lab.DefaultPort, "the console's port on this machine; 0 lets Docker pick a free one, printed when the lab is ready")
	cmd.Flags().StringVar(&listen, "listen", lab.DefaultListen, "the address the console is published on; anything but loopback exposes the lab, and its well-known login, to your network")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "remove the lab's data first and start over from the seed")
	cmd.Flags().StringVar(&scenario, "scenario", lab.DefaultScenario, "the fleet the lab seeds on its first run (the seed-dev command lists them), or a scale lab scenario ("+strings.Join(labscenario.Names(), ", ")+") sized by --systems")
	// The scale lab (#1357): a scenario manifest resolved at a size, brought
	// up as one outpost per site (#1554), or printed with --plan.
	cmd.Flags().IntVar(&systems, "systems", 0, "how many systems a scale lab scenario resolves to")
	// A kept scale lab (#1633): a soak runs for days through restarts, so
	// its data outlives a stop the way a learner's lab's does.
	cmd.Flags().BoolVar(&keep, "keep", false, "a scale lab: keep its data when it stops, so the next run with the same scenario, size and --keep picks up where it left off (--fresh starts over)")
	cmd.Flags().BoolVar(&plan, "plan", false, "print the scale lab scenario's resolved plan (sites, systems per standard, components per product) and exit, starting nothing")
	cmd.Flags().StringVar(&image, "image", "", "run this omniglass image instead of the one this binary was built with (env OMNIGLASS_LAB_IMAGE); one that is not this binary's own build is warned about")
	// A live omnibot (#2109): the key stays in the host's env, named here,
	// and reaches the server as an env secret (#2223), seeded or scale lab (#2298).
	cmd.Flags().StringVar(&keyEnv, "omnibot-key-env", "", "run omnibot on a live model (OpenRouter by default) with the key in this environment variable, e.g. OPENROUTER_API_KEY, handed to the server as its env secret lab-omnibot-key; without it, or the shell's own OMNIGLASS_SETTING__ASSISTANT__ and OMNIGLASS_SECRET__ variables, omnibot plays the recording --omnibot-replay names")
	cmd.Flags().StringVar(&replay, "omnibot-replay", "", "the recording omnibot plays when no live model is configured, by name ("+strings.Join(lab.OmnibotReplays(), ", ")+"); default: the one written for the scenario's fleet (corporate-outage for corporate, else default)")
	cmd.Flags().StringVar(&project, "project", lab.DefaultProject, "the Docker compose project name")
	_ = cmd.Flags().MarkHidden("project")
	// For scripts, not learners (the demo pipeline's recorders and the
	// probe, web/e2e/lib/lab.sh, #1197): a throwaway lab of its own project,
	// held for the script's process, saying how to reach it in a file.
	cmd.Flags().BoolVar(&script.ephemeral, "ephemeral", false, "a throwaway lab: its data goes when it stops (needs its own --project)")
	cmd.Flags().StringVar(&script.readyFile, "ready-file", "", "once ready, write how to reach the lab here, as shell assignments")
	cmd.Flags().IntVar(&script.whilePID, "while-pid", 0, "stop the lab once this process is gone")
	// The docs screenshots (#1267) photograph pages that must hold still: a
	// fleet with no outpost collecting from it.
	cmd.Flags().BoolVar(&script.noOutpost, "no-outpost", false, "run no outpost: seed the fleet without the simulated wiring, so nothing collects (needs --ephemeral)")
	// The docs screenshots (#1790) photograph platform health with full
	// sparklines: an hour of the server's figures, made up, for a capture only.
	cmd.Flags().BoolVar(&script.platformHistory, "platform-history", false, "seed an hour of made-up platform history on the server, so platform health draws full lines in a capture (needs --ephemeral)")
	// The single sign-on walkthrough (#2362) signs in through an identity
	// provider on the lab network, which only a script's browser reaches.
	cmd.Flags().BoolVar(&script.sso, "sso", false, "run Dex beside the lab as its identity provider, with single sign-on configured against it and a demo person linked (needs --ephemeral)")
	// The lab footprint (#1277) measures a lab at a chosen cadence profile.
	cmd.Flags().StringVar(&script.cadence, "cadence", "", "the outpost's cadence profile ("+lab.CadenceNames()+"); empty is lab, or walkthrough for a throwaway lab")
	// The scale lab's knobs for proving it: a forced low memory limit shows
	// the refusal without a small machine, and the bring-up's bound.
	cmd.Flags().StringVar(&scale.hostMemory, "host-memory", "", "a scale lab: plan against this much host memory (e.g. 4g) instead of what Docker reports")
	cmd.Flags().DurationVar(&scale.timeout, "ready-timeout", defaultScaleTimeout, "a scale lab: how long its bring-up may take before it is removed, naming what is not up")
	// The delivery table's proof that a refusal reads as loss (#1698).
	cmd.Flags().BoolVar(&scale.induce, "induce-unregistered", false, "a scale lab: make one simulated device answer a property key no driver registers, so its samples are refused (refused-unregistered)")
	// The marketing captures (#2728) photograph a fleet that has been running.
	cmd.Flags().BoolVar(&scale.healthHistory, "health-history", false, "a throwaway scale lab (not --keep): seed the scenario's health storyline, 30 days of made-up health and the alarms behind it, for a capture")
	for _, name := range []string{"ephemeral", "ready-file", "while-pid", "no-outpost", "platform-history", "sso", "cadence", "host-memory", "ready-timeout", "induce-unregistered", "health-history"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}

// runLabScenario resolves a scale lab scenario manifest at --systems and,
// with --plan, prints the plan and starts nothing. Without --plan it brings
// the plan up (#1554): Postgres, the server, and one outpost per site, each
// simulating its site's components, after a check that the host can fit it.
func runLabScenario(ctx context.Context, out io.Writer, m *labscenario.Manifest, f labFlags) error {
	if f.keep && f.script.ephemeral {
		return errors.New("--keep and --ephemeral contradict each other: a kept scale lab keeps its data when it stops, a throwaway one removes it")
	}
	if f.script.sso {
		return errors.New("--sso is a seeded fleet's: a scale lab runs no identity provider")
	}
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
	outposts := make([]lab.SiteOutpost, len(plan.Sites))
	for i, site := range plan.Sites {
		token, hash, _, err := auth.NewBearerToken()
		if err != nil {
			return err
		}
		outposts[i] = lab.SiteOutpost{Name: labscenario.SiteOutpost(site.Name), Token: token, TokenHash: hex.EncodeToString(hash),
			Components: site.Simulated()}
	}
	pushers, err := scaleLabPushers(plan, auth.NewBearerToken)
	if err != nil {
		return err
	}
	project := f.project
	if project == lab.DefaultProject {
		project = lab.ScaleProject
	}
	cadence := f.script.cadence
	if cadence == "" {
		cadence = lab.CadenceLab
	}
	if f.omnibotReplay != "" {
		if err := lab.CheckOmnibotReplay(f.omnibotReplay); err != nil {
			return err
		}
	}
	omnibotKey, err := labOmnibotKey(f.omnibotKeyEnv, config.Named)
	if err != nil {
		return err
	}
	settingsEnv, err := labServerEnv(append(config.Environ(settings.EnvPrefix), config.Environ(secret.EnvPrefix)...))
	if err != nil {
		return err
	}
	choice, err := lab.SelectImage(f.version, f.imageDigest, labImage(f))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Starting a scale lab: scenario %s at %d systems, %d sites (an outpost each), %d simulated components.\n",
		m.Name, plan.Totals.Systems, len(plan.Sites), plan.Simulated())
	if names := labServerEnvNames(settingsEnv); len(names) > 0 {
		fmt.Fprintf(out, "Your server takes %d setting and secret variable(s) from this shell: %s.\n", len(names), strings.Join(names, ", "))
	}
	fmt.Fprintln(out)
	start := f.start
	if start == nil {
		start = startScaleLab
	}
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	return start(ctx, out, lab.Config{
		Options: lab.Options{
			Project: project, Listen: f.listen, Port: f.port, Image: choice.Ref, Scenario: m.Name,
			Systems: plan.Totals.Systems, SiteOutposts: outposts, Pushers: pushers,
			Owner: lab.CurrentOwner(host), WorkDir: wd,
			Ephemeral: !f.keep, Cadence: cadence, InduceUnregistered: f.scale.induce, HealthHistory: f.scale.healthHistory,
			ServerEnv: settingsEnv, OmnibotKey: omnibotKey, OmnibotReplay: f.omnibotReplay,
		},
		Image: choice, Fresh: f.fresh, Hold: true, ReadyFile: f.script.readyFile, WhilePID: f.script.whilePID,
		ScaleRun: scale,
	})
}

// startScaleLab brings a scale lab's config up on this machine's Docker,
// held until it is stopped.
func startScaleLab(ctx context.Context, out io.Writer, cfg lab.Config) error {
	eng, daemon, err := labEngine(ctx, out)
	if err != nil {
		return err
	}
	r := labRunner(eng, daemon, out)
	r.Poll, r.ReadyTimeout = 3*time.Second, cfg.ScaleRun.Timeout
	_, err = r.Run(ctx, cfg)
	return err
}

// writeScaleCapacity follows a plan with its preflight: the estimate from
// the measured costs and, when the Docker host can be read, Postgres's
// shared buffers sized to it and the decision bring-up would make on it. It starts nothing, and a refusal is printed, not
// returned: the plan was asked for, not the lab.
func writeScaleCapacity(ctx context.Context, out io.Writer, scale *lab.ScaleRun, plan *labscenario.Plan, readHost hostReader) error {
	need := scale.Cost.Estimate(len(plan.Sites), plan.Totals.Components)
	fmt.Fprintf(out, "\nPreflight: about %s of memory and %.1f cores (the server and Postgres, an outpost for each of %d sites, %d simulated components).\n",
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
	fmt.Fprintf(out, "Postgres's shared buffers, sized to this host and fleet, come to %s: about %s in all.\n",
		lab.FormatMiB(float64(c.BuffersMiB)), lab.FormatMiB(c.Need.MemMiB))
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
	// healthHistory seeds the scenario's health storyline (#2728) on a
	// throwaway scale lab, for the marketing captures.
	healthHistory bool
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
	ephemeral       bool
	noOutpost       bool
	platformHistory bool
	sso             bool
	readyFile       string
	whilePID        int
	cadence         string
}

type labFlags struct {
	port                             int
	listen, scenario, image, project string
	fresh, keep                      bool
	systems                          int
	plan                             bool
	// omnibotKeyEnv names the host env var holding a live model's key.
	omnibotKeyEnv string
	// omnibotReplay names the recording omnibot plays with no live model.
	omnibotReplay string
	script        labScript
	scale         labScale
	version       string
	// imageDigest is the release image's digest stamped into this binary.
	imageDigest string
	// readHost, when set, stands in for this machine's Docker host in a
	// plan's preflight.
	readHost hostReader
	// start, when set, stands in for bringing a scale lab's config up on
	// Docker.
	start func(ctx context.Context, out io.Writer, cfg lab.Config) error
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
	if f.scale.healthHistory {
		return fmt.Errorf("--health-history seeds a scale scenario's storyline (%s); %q is a seeded fleet", strings.Join(labscenario.Names(), ", "), f.scenario)
	}
	if f.keep {
		return fmt.Errorf("--keep keeps a scale lab scenario's data (%s); %q is a seeded fleet, whose lab keeps its data already",
			strings.Join(labscenario.Names(), ", "), f.scenario)
	}
	if f.omnibotReplay != "" {
		if err := lab.CheckOmnibotReplay(f.omnibotReplay); err != nil {
			return err
		}
	}
	omnibotKey, err := labOmnibotKey(f.omnibotKeyEnv, config.Named)
	if err != nil {
		return err
	}
	settingsEnv, err := labServerEnv(append(config.Environ(settings.EnvPrefix), config.Environ(secret.EnvPrefix)...))
	if err != nil {
		return err
	}
	faults, err := devseed.PlantedFaults(sc)
	if err != nil {
		return err
	}
	choice, err := lab.SelectImage(version, f.imageDigest, labImage(f))
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Starting your omniglass lab. The first run takes a few minutes; later ones, under one.")
	if names := labServerEnvNames(settingsEnv); len(names) > 0 {
		fmt.Fprintf(out, "Your server takes %d setting and secret variable(s) from this shell: %s.\n", len(names), strings.Join(names, ", "))
	}
	fmt.Fprintln(out)

	eng, daemon, err := labEngine(ctx, out)
	if err != nil {
		return err
	}
	token, hash, _, err := auth.NewBearerToken()
	if err != nil {
		return err
	}
	// The fleet's pushers' tokens (#1370, #1743), minted per run like the outpost's.
	doc, err := devseed.Fixtures()
	if err != nil {
		return err
	}
	pushers, err := devLabPushers(doc.SimPushers, f.script.noOutpost, auth.NewBearerToken)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	r := labRunner(eng, daemon, out)
	_, err = r.Run(ctx, lab.Config{
		Options: lab.Options{
			Project: f.project, Listen: f.listen, Port: f.port, Image: choice.Ref, Scenario: sc.Name,
			OutpostToken: token, OutpostTokenHash: hex.EncodeToString(hash),
			Pushers: pushers,
			Owner:   lab.CurrentOwner(host), WorkDir: wd,
			Ephemeral: f.script.ephemeral, NoOutpost: f.script.noOutpost, Cadence: f.script.cadence,
			PlatformHistory: f.script.platformHistory, SSO: f.script.sso, OmnibotKey: omnibotKey, OmnibotReplay: f.omnibotReplay,
			ServerEnv: settingsEnv,
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
	var scenario, outpost, user, password, replay string
	var noOutpost, induce, keep, platformHistory, ssoDemo, healthHistory bool
	var systems, reportSeconds int
	cmd := &cobra.Command{
		Use:    "seed",
		Short:  "The lab's one-off: migrate, mark the database as a lab's, seed it on the first run, and enroll the outpost",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pushers, err := lab.ParsePusherTokens(config.Get(lab.PusherTokensEnv))
			if err != nil {
				return err
			}
			if err := lab.CheckOmnibotReplay(replay); err != nil {
				return err
			}
			if systems > 0 {
				err = runLabScaleSeed(cmd.Context(), cmd.OutOrStdout(), scenario, systems, user, password, config.Get(lab.SiteOutpostTokenHashesEnv), pushers, induce, keep, healthHistory)
			} else if induce {
				return errors.New("--induce-unregistered is a scale lab's: it needs --systems")
			} else if healthHistory {
				return errors.New("--health-history seeds a scale scenario's storyline: it needs --systems")
			} else {
				err = runLabSeed(cmd.Context(), cmd.OutOrStdout(), scenario, outpost, user, password, config.Get("OMNIGLASS_LAB_OUTPOST_TOKEN_HASH"), noOutpost,
					pushers, reportSeconds, platformHistory, ssoDemo)
			}
			if err != nil {
				return err
			}
			// The simulators read the world their plan resolves to (#2773): one
			// file per site outpost, beside the omnibot recording in the data
			// volume the outposts mount read-only.
			if systems > 0 {
				if err := writeLabWorlds(scenario, systems, filepath.Join(cfg().DataDir, filepath.Base(lab.WorldDir))); err != nil {
					return err
				}
			}
			// The server reads the lab's omnibot recording as it starts (#2121),
			// a seeded lab's or a scale lab's (#2298), and plays it whenever no
			// live model is configured.
			return lab.WriteOmnibotReplay(cfg().DataDir, replay)
		},
	}
	cmd.Flags().StringVar(&scenario, "scenario", lab.DefaultScenario, "the fleet to seed on the first run")
	cmd.Flags().StringVar(&outpost, "outpost", lab.OutpostName, "the outpost to enroll")
	cmd.Flags().StringVar(&user, "user", lab.LoginUser, "the owner's username")
	cmd.Flags().StringVar(&password, "password", lab.LoginPassword, "the owner's password")
	cmd.Flags().BoolVar(&noOutpost, "no-outpost", false, "seed the fleet without the simulated wiring and enroll no outpost, so the lab runs none")
	cmd.Flags().BoolVar(&platformHistory, "platform-history", false, "on the first run, seed an hour of made-up platform history on the server, for a capture (#1790)")
	cmd.Flags().IntVar(&reportSeconds, "report-seconds", 0, "set the platform's collection report window (collection.report_seconds) to this many seconds; 0 leaves it as it is")
	cmd.Flags().IntVar(&systems, "systems", 0, "seed the scale lab scenario --scenario resolved at this many systems, with an outpost per site")
	cmd.Flags().BoolVar(&induce, "induce-unregistered", false, "with --systems: make one simulated device answer a property key no driver registers, so its samples are refused (refused-unregistered)")
	cmd.Flags().BoolVar(&keep, "keep", false, "a kept scale lab: keep the fleet a database already holds and re-enroll its site outposts")
	cmd.Flags().BoolVar(&healthHistory, "health-history", false, "with --systems: seed the scenario's health storyline, 30 days of made-up health and the alarms behind it (#2728)")
	cmd.Flags().StringVar(&replay, "omnibot-replay", lab.DefaultOmnibotReplay, "the omnibot recording to write for the server to play")
	cmd.Flags().BoolVar(&ssoDemo, "sso", false, "configure single sign-on against the lab's Dex and link its demo person (#2362)")
	return cmd
}

// writeLabWorlds resolves a scale lab's scenario at its size to each site's
// world and writes them under dir (#2773): the sim is, and a world file is how
// the simulator beside each outpost learns it, not from the server.
func writeLabWorlds(scenario string, systems int, dir string) error {
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
	worlds, err := labscenario.SiteWorlds(m, cat, plan)
	if err != nil {
		return err
	}
	_, err = lab.WriteWorlds(dir, worlds)
	return err
}

// runLabSeed is the lab's seed one-off, in the trusted direct-DB lane like
// bootstrap and seed-dev. It refuses a database that holds data and no lab
// mark, marks a fresh one, seeds the fleet and the owner only on the first
// run (a later run keeps the estate), makes sure the login works, and
// enrolls the outpost with the hash of the token the lab minted for this run.
// With noOutpost (a throwaway lab whose pages hold still, #1267) it seeds the
// fleet without the simulated wiring, which nothing would collect from, and
// enrolls no outpost, so the lab runs none.
// pushers is each pusher's token for this run (#1370, #1743), by name: its
// hash and non-secret prefix, never the token itself. Empty, none is set.

// reportSeconds, when above zero, sets the platform's collection report
// window (#1646) before the server and the outpost start, so both read it for
// their first window: a walkthrough lab's 10s, where the platform's 60s would
// land the first figures as its recording ends.
// platformHistory seeds, on the first run only, an hour of made-up figures on
// the server's platform component (#1790), so a capture of platform health
// draws full lines. Only a capture asks for it: a learner's lab reads what
// its own server measured.
// ssoDemo configures single sign-on against the lab's Dex (#2362, the lab's
// --sso): see seedLabSSO.
func runLabSeed(ctx context.Context, out io.Writer, scenario, outpost, user, password, outpostTokenHash string, noOutpost bool, pushers map[string]lab.PusherToken, reportSeconds int, platformHistory, ssoDemo bool) error {
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
		if err := devseed.RunScenario(ctx, gw, "", sc, devseed.Options{SimFleet: !noOutpost}); err != nil {
			return err
		}
		summary = fmt.Sprintf("the %s fleet, seeded on this lab's first run", sc.Name)
		if platformHistory {
			if err := devseed.SeedPlatformHistory(ctx, gw, lab.ServerName, time.Now()); err != nil {
				return err
			}
			summary += ", with an hour of platform history"
		}
	} else if ok, err := gw.SetPassword(ctx, user, pwHash); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("this lab has no user %q to sign in as; start over with omniglass lab run --fresh", user)
	}
	if reportSeconds > 0 {
		if _, err := gw.MergePatchSettingBinding(ctx, "", "platform", "collection", map[string]any{"report_seconds": reportSeconds}); err != nil {
			return err
		}
	}
	if ssoDemo {
		if err := seedLabSSO(ctx, gw); err != nil {
			return err
		}
		summary += ", with single sign-on through the lab's Dex"
	}
	all := scope.Set{All: true}
	// With no outpost to run, none is enrolled.
	present := false
	if !noOutpost {
		present = true
		if _, err := gw.GetOutpost(ctx, outpost, all); errors.Is(err, storage.ErrOutpostNotFound) {
			present = false
		} else if err != nil {
			return err
		}
	}
	if present {
		if outpostTokenHash == "" {
			return errors.New("lab seed: OMNIGLASS_LAB_OUTPOST_TOKEN_HASH is not set")
		}
		if _, err := gw.SetEnrollmentToken(ctx, "", outpost, outpostTokenHash, all); err != nil {
			return err
		}
		if err := setPusherTokens(ctx, out, gw, pushers); err != nil {
			return err
		}
	}
	if err := dropStoredLabOmnibotKey(ctx, gw); err != nil {
		return err
	}
	fmt.Fprintln(out, lab.FormatSeedResult(lab.SeedResult{Outpost: present, Summary: summary}))
	return nil
}

// seedLabSSO configures single sign-on against the lab's Dex (#2362): the
// platform oauth2-client secret holding the lab's demo client, the sso
// settings naming the issuer and that secret (offline_access among the
// scopes, so the re-check has a refresh token to redeem), Dex's host in the
// egress allow list (it answers plain http on the lab network), and the demo
// person, a viewer, with their identity at Dex linked. The seed's trusted
// lane writes the settings directly, as the settings file would. Idempotent:
// what is already there is kept.
func seedLabSSO(ctx context.Context, gw *storage.PG) error {
	all := scope.Set{All: true}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{Name: lab.SSOClientSecret, SecretType: "oauth2-client", OwnerKind: "platform",
		Label:  "The lab's demo client at its Dex",
		Fields: map[string]string{"client_id": lab.DexClientID, "client_secret": lab.DexClientSecret}}, all, true); err != nil && !errors.Is(err, storage.ErrSecretExists) {
		return fmt.Errorf("lab seed: the sso client secret: %w", err)
	}
	bindings, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		return err
	}
	allow := []any{}
	for _, b := range bindings {
		if b.Namespace == "notification" {
			if l, ok := b.Doc["egress_allow"].([]any); ok {
				allow = l
			}
		}
	}
	if !slices.Contains(allow, any(lab.DexService)) {
		allow = append(allow, lab.DexService)
		if _, err := gw.MergePatchSettingBinding(ctx, "", "platform", "notification", map[string]any{"egress_allow": allow}); err != nil {
			return err
		}
	}
	if _, err := gw.MergePatchSettingBinding(ctx, "", "platform", "sso", map[string]any{
		"issuer": lab.DexIssuer, "client_secret": lab.SSOClientSecret, "scopes": "openid email profile offline_access",
	}); err != nil {
		return err
	}
	u, err := gw.CreatePerson(ctx, "", storage.PersonSpec{Username: lab.SSODemoUser, Email: lab.SSODemoEmail, Label: lab.SSODemoLabel}, all)
	if errors.Is(err, storage.ErrUsernameTaken) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lab seed: the sso demo person: %w", err)
	}
	if _, err := gw.CreateGrant(ctx, "", u.ID, storage.GrantSpec{Role: lab.SSODemoRole, ScopeKind: "all"}, all); err != nil {
		return fmt.Errorf("lab seed: the sso demo person's grant: %w", err)
	}
	if _, err := gw.LinkIdentity(ctx, "", u.ID, lab.SSODemoIdentity()); err != nil {
		return fmt.Errorf("lab seed: link the sso demo person: %w", err)
	}
	return nil
}

// labServerEnv is the host's OMNIGLASS_SETTING__ and OMNIGLASS_SECRET__
// variables (#2223), in name order, for the lab's server: the env settings
// layer and the env secrets the server reads at boot. They are judged here as
// the server will judge them (the secrets' JSON; their fields meet the type
// catalog at the server's boot), so a typo or a value that does not parse
// stops the run before anything is created, naming the variable and never
// its value.
func labServerEnv(environ []string) ([]string, error) {
	var out, sets []string
	for _, e := range environ {
		switch {
		case strings.HasPrefix(e, settings.EnvPrefix):
			sets = append(sets, e)
			out = append(out, e)
		case strings.HasPrefix(e, secret.EnvPrefix):
			out = append(out, e)
		}
	}
	if _, err := settings.LoadEnv(sets); err != nil {
		return nil, fmt.Errorf("lab: your shell's %w", err)
	}
	if _, err := secret.ParseEnv(out); err != nil {
		return nil, fmt.Errorf("lab: your shell's %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// labServerEnvNames is the variables' names alone, for the start line: a
// value may be a URL, a secret's name or the secret itself, so the line names
// only the keys.
func labServerEnvNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		names = append(names, name)
	}
	return names
}

// labOmnibotKey reads the live model key from the host env var name names;
// no name is no key. A name that is unset or empty is refused, so a typo
// never quietly falls back to the recording.
func labOmnibotKey(name string, getenv func(string) string) (string, error) {
	if name == "" {
		return "", nil
	}
	key := getenv(name)
	if key == "" {
		return "", fmt.Errorf("--omnibot-key-env: %s is not set (or is empty) in this shell, so there is no key to run omnibot on", name)
	}
	return key, nil
}

// dropStoredLabOmnibotKey undoes what a lab seeded before #2223 left: the
// model key sealed as the platform secret lab.OmnibotKeySecret and the
// assistant settings naming it. --omnibot-key-env now declares that name as
// the server's env secret, which a stored one of the same name would fail at
// boot, and the binding would shadow the env layer the alias sets. An
// assistant setting an operator made with a key of their own is left alone.
func dropStoredLabOmnibotKey(ctx context.Context, gw *storage.PG) error {
	all := scope.Set{All: true}
	bindings, err := gw.GetSettingBindings(ctx, "platform")
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if b.Namespace != "assistant" || b.Doc["key_secret"] != lab.OmnibotKeySecret {
			continue
		}
		if _, err := gw.MergePatchSettingBinding(ctx, "", "platform", "assistant", map[string]any{"provider": nil, "key_secret": nil}); err != nil {
			return err
		}
	}
	if err := gw.DeleteSecret(ctx, "", lab.OmnibotKeySecret, all, all, true, true); err != nil && !errors.Is(err, storage.ErrSecretNotFound) {
		return err
	}
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
// bulk seed, #1555, replaces) on an empty database, and each site's outpost
// enrolled with the hash of the token the lab minted for it, and each
// declared pusher (#1743) given the token the lab minted for it. With induce
// (#1698), one device answers a property key no driver registers, named in
// the summary. With keep (a kept scale lab, #1633), a database already
// holding this plan's fleet is kept as it is: its site outposts are re-enrolled
// with this run's hashes, its pushers given this run's tokens, and the login
// is made to work, and nothing is seeded, so a kept lab induces only if its
// first seed did.
func runLabScaleSeed(ctx context.Context, out io.Writer, scenario string, systems int, user, password, tokenHashes string, pushers map[string]lab.PusherToken, induce, keep, healthHistory bool) error {
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
	if healthHistory {
		if m.HealthHistory == nil {
			return fmt.Errorf("lab seed: the %s scenario declares no health storyline for --health-history", m.Name)
		}
		if missing := plan.MissingStorylineSystems(m.HealthHistory); len(missing) > 0 {
			return fmt.Errorf("lab seed: the health storyline names systems a %d-system %s lab has not: %s", systems, m.Name, strings.Join(missing, ", "))
		}
	}
	hashes, err := lab.ParseSiteOutpostTokenHashes(tokenHashes)
	if err != nil {
		return err
	}
	// Every site's outpost needs its hash, checked before the database is touched.
	for _, site := range plan.Sites {
		if outpost := labscenario.SiteOutpost(site.Name); hashes[outpost] == "" {
			return fmt.Errorf("lab seed: %s carries no token hash for the outpost %s", lab.SiteOutpostTokenHashesEnv, outpost)
		}
		for _, p := range site.Pushers {
			if p.IsComponent() {
				continue // a webhook sender signs its posts, a polled service is read: neither needs a bearer token
			}
			if _, ok := pushers[p.Name]; !ok {
				return fmt.Errorf("lab seed: %s carries no token for the pusher %s", lab.PusherTokensEnv, p.Name)
			}
		}
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
	if st.HoldsData {
		if !keep {
			return errors.New("lab seed: a throwaway scale lab seeds an empty database, and this one holds data; start it again with --keep to keep that data, or with --fresh to start over")
		}
		worlds, err := labscenario.SiteWorlds(m, cat, plan)
		if err != nil {
			return err
		}
		return reenrollKeptScaleLab(ctx, out, gw, plan, worlds, hashes, pushers, user, pwHash)
	}
	if err := bootstrapLabOwner(ctx, gw, user, pwHash); err != nil {
		return err
	}
	start := time.Now()
	all := scope.Set{All: true}
	// A site's outpost must exist before its endpoints name it, and the site it
	// sits at does not until the plan is seeded: create each outpost first, place
	// it at its site after.
	for _, site := range plan.Sites {
		outpost := labscenario.SiteOutpost(site.Name)
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{
			Name: outpost, Label: siteOutpostLabel(site) + " Edge Outpost", Description: "the scale lab's outpost for " + site.Name,
		}, all, all); err != nil {
			return fmt.Errorf("lab seed: create site outpost %s: %w", outpost, err)
		}
	}
	seeded, err := devseed.SeedPlan(ctx, gw, "", plan, devseed.PlanOptions{Endpoints: true, OutpostFor: labscenario.SiteOutpost, InduceUnregistered: induce})
	if err != nil {
		return err
	}
	for _, site := range plan.Sites {
		outpost, at := labscenario.SiteOutpost(site.Name), site.Name
		if _, err := gw.UpdateOutpost(ctx, "", outpost, storage.OutpostPatch{LocationName: &at}, all, all, all); err != nil {
			return fmt.Errorf("lab seed: place %s at %s: %w", outpost, site.Name, err)
		}
		if _, err := gw.SetEnrollmentToken(ctx, "", outpost, hashes[outpost], all); err != nil {
			return err
		}
	}
	if err := setPusherTokens(ctx, out, gw, pushers); err != nil {
		return err
	}
	// The storyline ends a moment before the seed began, so every system's
	// own record (created, staffed) follows the seeded past (ADR-2726).
	if healthHistory {
		if err := devseed.SeedHealthHistory(ctx, gw, m.HealthHistory, start.Add(-time.Minute)); err != nil {
			return fmt.Errorf("lab seed: %w", err)
		}
	}
	summary := fmt.Sprintf("scenario %s at %d systems: %d sites, %d components, %d pushers, %d webhook senders, seeded in %s",
		m.Name, plan.Totals.Systems, len(plan.Sites), plan.Totals.Components, seeded.Pushers, seeded.Senders, time.Since(start).Round(time.Second))
	if in := seeded.Induced; in != nil {
		summary += fmt.Sprintf("; %s answers %s through %s, refused-unregistered", in.Component, in.Key, in.Driver)
	}
	fmt.Fprintln(out, lab.FormatSeedResult(lab.SeedResult{Outpost: true, Summary: summary}))
	return nil
}

// reenrollKeptScaleLab is a kept scale lab's seed on a later run (#1633):
// the database must hold this plan's fleet (an outpost per site, and each
// simulating its site's components), or the lab was kept at another size or
// scenario and is refused, naming --fresh. Then each site outpost is enrolled
// with this run's hash, each pusher given this run's token, the login is
// reset, and nothing is seeded.
func reenrollKeptScaleLab(ctx context.Context, out io.Writer, gw storage.Gateway, plan *labscenario.Plan, worlds map[string]*world.World, hashes map[string]string, pushers map[string]lab.PusherToken, user, pwHash string) error {
	all := scope.Set{All: true}
	notThisPlan := func(why string) error {
		return fmt.Errorf("lab seed: this kept scale lab's fleet is not scenario %s at %d systems (%s); "+
			"run it at the scenario and size it was kept at, or start over with --fresh", plan.Scenario, plan.Totals.Systems, why)
	}
	outposts, err := gw.ListOutposts(ctx, all)
	if err != nil {
		return err
	}
	if len(outposts) != len(plan.Sites) {
		return notThisPlan(fmt.Sprintf("it has %d outposts for the plan's %d sites", len(outposts), len(plan.Sites)))
	}
	for _, site := range plan.Sites {
		outpost := labscenario.SiteOutpost(site.Name)
		n, err := gw.GetOutpost(ctx, outpost, all)
		if errors.Is(err, storage.ErrOutpostNotFound) {
			return notThisPlan("it has no outpost " + outpost)
		} else if err != nil {
			return err
		}
		// A first run stopped between seeding the fleet and placing its
		// outposts leaves an outpost at no site.
		if n.LocationName == nil || *n.LocationName != site.Name {
			return notThisPlan(fmt.Sprintf("%s is not placed at its site %s", outpost, site.Name))
		}
		devices, err := gw.OutpostSimDevices(ctx, outpost, all)
		if err != nil {
			return err
		}
		// A webhook sender (#1747) is a component on its site's outpost too.
		if len(devices) != site.Simulated() {
			return notThisPlan(fmt.Sprintf("%s simulates %d components, the plan's site %d", outpost, len(devices), site.Simulated()))
		}
		// The world names every component (#2773): a fleet kept from before
		// that holds names the world does not, and its simulator would place
		// none of them in a room.
		w := worlds[outpost]
		for _, d := range devices {
			if _, placed := w.PlaceDevice(d.Name); !placed && w.Covered(d.Name) == nil {
				return notThisPlan(fmt.Sprintf("%s simulates %s, which the plan's world does not name", outpost, d.Name))
			}
		}
	}
	for _, site := range plan.Sites {
		outpost := labscenario.SiteOutpost(site.Name)
		if _, err := gw.SetEnrollmentToken(ctx, "", outpost, hashes[outpost], all); err != nil {
			return err
		}
	}
	if err := setPusherTokens(ctx, out, gw, pushers); err != nil {
		return err
	}
	if ok, err := gw.SetPassword(ctx, user, pwHash); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("this kept scale lab has no user %q to sign in as; start over with --fresh", user)
	}
	fmt.Fprintln(out, lab.FormatSeedResult(lab.SeedResult{Outpost: true, Summary: fmt.Sprintf(
		"kept this scale lab's fleet from its last run: scenario %s at %d systems, %d sites, %d components",
		plan.Scenario, plan.Totals.Systems, len(plan.Sites), plan.Totals.Components)}))
	return nil
}

// siteOutpostLabel is what a site is called on its outpost: its label, else its name.
func siteOutpostLabel(site labscenario.SitePlan) string {
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
