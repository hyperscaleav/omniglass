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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
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
	group.AddCommand(newLabRunCmd(version), newLabSeedCmd())
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
		script   labScript
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
				script: script,
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", lab.DefaultPort, "the console's port on this machine; 0 lets Docker pick a free one, printed when the lab is ready")
	cmd.Flags().StringVar(&listen, "listen", lab.DefaultListen, "the address the console is published on; anything but loopback exposes the lab, and its well-known login, to your network")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "remove the lab's data first and start over from the seed")
	cmd.Flags().StringVar(&scenario, "scenario", lab.DefaultScenario, "the fleet the lab seeds on its first run (the seed-dev command lists them)")
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
	for _, name := range []string{"ephemeral", "ready-file", "while-pid", "no-node", "cadence"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
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
	script                           labScript
}

func runLab(ctx context.Context, out io.Writer, version string, f labFlags) error {
	sc, err := devseed.LookupScenario(f.scenario)
	if err != nil {
		return err
	}
	faults, err := devseed.PlantedFaults(sc)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Starting your omniglass lab. The first run takes a few minutes; later ones, under one.")
	fmt.Fprintln(out)

	home, _ := os.UserHomeDir()
	daemon, err := lab.ResolveDaemon(lab.Env{Getenv: config.Get, ReadFile: os.ReadFile, Home: home, GOOS: runtime.GOOS})
	if err != nil {
		return &lab.PreflightError{Cause: "Could not tell which Docker to use: " + err.Error(),
			Fix: "check DOCKER_HOST, DOCKER_CONTEXT and `docker context ls`.", Err: err}
	}
	if !lab.IsLocal(daemon.Host) {
		return lab.RemoteDaemon(daemon)
	}
	if err := dialDaemon(daemon.Host); err != nil {
		return lab.ClassifyPing(daemon, err)
	}
	eng, err := lab.NewDockerEngine(daemon.Host)
	if err != nil {
		return lab.ClassifyPing(daemon, err)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	info, err := eng.Ping(pctx)
	cancel()
	if err != nil {
		return lab.ClassifyPing(daemon, err)
	}
	fmt.Fprintf(out, "  %-10s %s\n", "docker", fmt.Sprintf("running (Engine API %s, %s, from %s)", info.APIVersion, daemon.Host, daemon.Source))

	image := f.image
	if image == "" {
		image = config.Get("OMNIGLASS_LAB_IMAGE")
	}
	choice := lab.SelectImage(version, image)
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
	r := &lab.Runner{
		Engine: eng, Daemon: daemon, Out: out, Host: host,
		Alive: lab.ProcessAlive, PIDNS: lab.CurrentPIDNS(), PortFree: lab.PortFree,
		HTTP: &http.Client{Timeout: 10 * time.Second}, Poll: time.Second, ReadyTimeout: 4 * time.Minute,
	}
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
	var noNode bool
	cmd := &cobra.Command{
		Use:    "seed",
		Short:  "The lab's one-off: migrate, mark the database as a lab's, seed it on the first run, and enroll the node",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLabSeed(cmd.Context(), cmd.OutOrStdout(), scenario, node, user, password, config.Get("OMNIGLASS_LAB_NODE_TOKEN_HASH"), noNode,
				pushCredential{Hash: config.Get("OMNIGLASS_LAB_PUSH_TOKEN_HASH"), Prefix: config.Get("OMNIGLASS_LAB_PUSH_TOKEN_PREFIX")})
		},
	}
	cmd.Flags().StringVar(&scenario, "scenario", lab.DefaultScenario, "the fleet to seed on the first run")
	cmd.Flags().StringVar(&node, "node", lab.NodeName, "the node to enroll")
	cmd.Flags().StringVar(&user, "user", lab.LoginUser, "the owner's username")
	cmd.Flags().StringVar(&password, "password", lab.LoginPassword, "the owner's password")
	cmd.Flags().BoolVar(&noNode, "no-node", false, "seed the fleet without the simulated wiring and enroll no node, so the lab runs none")
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

func runLabSeed(ctx context.Context, out io.Writer, scenario, node, user, password, nodeTokenHash string, noNode bool, push pushCredential) error {
	sc, err := devseed.LookupScenario(scenario)
	if err != nil {
		return err
	}
	c := cfg()
	// Judge the database before migrating it: a real database is refused as
	// it is, never first moved to the lab's schema.
	if err := labSeedGate(ctx, c.DSN); err != nil {
		return err
	}
	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	kek, _, err := secret.LoadKEK(config.Get, c.DataDir, func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) })
	if err != nil {
		return err
	}
	gw, err := storage.NewPG(ctx, c.DSN, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		return err
	}
	defer gw.Close()
	st, err := gw.LabState(ctx)
	if err != nil {
		return err
	}
	if err := storage.CheckLabMark(true, st.Marked, st.HoldsData); err != nil {
		return err
	}
	if err := gw.MarkLab(ctx); err != nil {
		return err
	}
	if err := seed.Run(ctx, gw); err != nil {
		return err
	}
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	summary := "kept this lab's fleet from its last run"
	if !st.HoldsData {
		_, tokHash, prefix, err := auth.NewBearerToken()
		if err != nil {
			return err
		}
		if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
			Username: user, SecretHash: tokHash, Prefix: prefix, PasswordHash: pwHash,
		}); err != nil {
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
