package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/simnode"
	"github.com/spf13/cobra"
)

// newNodeRunCmd is the edge run mode: enroll (claim the NATS credential), pull
// the worklist, and heartbeat. Outbound-only: the node dials the server, never
// the reverse. Flags fall back to the OMNIGLASS_* env so a systemd unit or
// container can supply them without putting the token on the command line.
//
// It is `node run`, a leaf under the generated `node` group, not a top-level
// `node`. As a top-level command it occupied the same name as the generated
// group and swallowed every node API command: `omniglass node list` resolved to
// the daemon and failed asking for --token. A mode reads as a verb anyway,
// beside `node list` and `node enroll`.
func newNodeRunCmd() *cobra.Command {
	var (
		serverURL  string
		name       string
		token      string
		heartbeat  time.Duration
		probeEvery time.Duration
		// laneWorkers is how many endpoint lanes run at once (#1634, #1249):
		// a count, or auto to size them from the node's limits (#1869).
		laneWorkers string
		// worklistEvery is the pull interval (#1577), apart from the heartbeat.
		worklistEvery time.Duration
		// sessionDials bounds the held sessions dialing at once (#1636).
		sessionDials int
		once         bool
		// The simulator in process (--simulate) and its own settings, which
		// mean nothing without it.
		simulate    bool
		impairments bool
		portOffset  int
		poll        time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the edge node: claim, pull the worklist, and heartbeat over NATS",
		Long: "Runs the edge node: claims its NATS credential with the enrollment token, pulls its worklist, " +
			"collects, and heartbeats until interrupted. Outbound-only: the node dials the server, never the reverse.\n\n" +
			"--simulate also runs the simulator in the same process, serving the simulated components placed on " +
			"this node on its own loopback (each on its 127.x address), with this node's identity: one process, " +
			"one enrollment, nothing to arrange between them. --impairments, --port-offset and --poll are the " +
			"simulator's and are refused without --simulate. `omniglass sim run` is the same simulator as a " +
			"process of its own, beside a node.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !simulate {
				for _, f := range []string{"impairments", "port-offset", "poll"} {
					if cmd.Flags().Changed(f) {
						return fmt.Errorf("node: --%s is the simulator's and needs --simulate", f)
					}
				}
			} else if once {
				return fmt.Errorf("node: --once is one collection cycle and cannot hold a simulator up; drop --once or --simulate")
			}
			if serverURL == "" {
				serverURL = config.Get("OMNIGLASS_SERVER")
			}
			if name == "" {
				name = config.Get("OMNIGLASS_NODE_NAME")
			}
			if token == "" {
				token = config.Get("OMNIGLASS_NODE_TOKEN")
			}
			if serverURL == "" || name == "" || token == "" {
				return fmt.Errorf("node: --server, --name, and --token are required (or set OMNIGLASS_SERVER / OMNIGLASS_NODE_NAME / OMNIGLASS_NODE_TOKEN)")
			}
			if probeEvery <= 0 {
				return fmt.Errorf("node: --probe-every %s: a probe runs on an interval of its own, so it must be positive", probeEvery)
			}
			lanes, err := laneWorkersFrom(laneWorkers, cmd.Flags().Changed("lane-workers"), config.Get("OMNIGLASS_LANE_WORKERS"))
			if err != nil {
				return err
			}
			dials, err := sessionDialsFrom(sessionDials, cmd.Flags().Changed("session-dials"), config.Get("OMNIGLASS_NODE_SESSION_DIALS"))
			if err != nil {
				return err
			}
			nodeCfg := node.Config{
				ServerURL: serverURL, Name: name, Token: token,
				HeartbeatEvery: heartbeat, ProbeEvery: probeEvery, LaneWorkers: lanes, WorklistEvery: worklistEvery,
				SessionDials: dials, Once: once,
			}
			if simulate {
				// A signal stops both halves gracefully, so the simulator
				// clears any impairment it applied before the process exits.
				pushers, err := simnode.ParsePushers(config.Get(simnode.PushersEnv))
				if err != nil {
					return err
				}
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return simnode.RunWithNode(ctx, nodeCfg, simnode.Config{
					ServerURL: serverURL, Name: name, Token: token,
					PortOffset: portOffset, Poll: poll, Impairments: impairments,
					Pushers: pushers,
				})
			}
			wl, err := node.Run(cmd.Context(), nodeCfg)
			if err != nil {
				return err
			}
			cmd.Printf("node %q: worklist has %d task(s), config generation %d\n", name, len(wl.Tasks), wl.ConfigGeneration)
			return nil
		},
	}
	cmd.Flags().StringVar(&serverURL, "server", "", "Omniglass server base URL (env OMNIGLASS_SERVER)")
	cmd.Flags().StringVar(&name, "name", "", "this node's registered name (env OMNIGLASS_NODE_NAME)")
	cmd.Flags().StringVar(&token, "token", "", "enrollment token from POST /nodes/{name}:enroll (env OMNIGLASS_NODE_TOKEN)")
	cmd.Flags().DurationVar(&heartbeat, "heartbeat", node.DefaultHeartbeat, "heartbeat interval")
	cmd.Flags().DurationVar(&probeEvery, "probe-every", node.DefaultProbeEvery, "how often each reachability probe runs, on the node's own timer, apart from the heartbeat. Keep it under the console's 150s staleness window, past which a probe-only endpoint reads stale between probes")
	cmd.Flags().StringVar(&laneWorkers, "lane-workers", laneWorkersAuto, "how many endpoint lanes run at once, each sending one request to its endpoint at a time, an operator's ahead of the scheduled; auto sizes them from the CPU and memory the node may use (env OMNIGLASS_LANE_WORKERS)")
	cmd.Flags().DurationVar(&worklistEvery, "worklist-every", node.DefaultWorklistEvery, "how often the node pulls its worklist, apart from the heartbeat; a change made on the server reaches the node at its next pull")
	cmd.Flags().IntVar(&sessionDials, "session-dials", node.DefaultSessionDials, "how many held sessions make their first dial and handshake at once (a redial takes no slot); a session waiting for a slot reads establishing (env OMNIGLASS_NODE_SESSION_DIALS)")
	cmd.Flags().BoolVar(&once, "once", false, "run a single claim + pull + heartbeat cycle and exit")
	cmd.Flags().BoolVar(&simulate, "simulate", false, "also run the simulator in this process, serving the simulated components placed on this node on its own loopback")
	cmd.Flags().BoolVar(&impairments, "impairments", false, "with --simulate: apply impairments (unplug, latency, loss) and truthful ping by rewriting the firewall and traffic shaping of this network namespace; needs NET_ADMIN, and is meant for a namespace of the node's own (a container)")
	cmd.Flags().IntVar(&portOffset, "port-offset", 0, "with --simulate: lift the simulated ports below 1024 by this much, to serve without the privilege to bind them")
	cmd.Flags().DurationVar(&poll, "poll", simnode.DefaultPoll, "with --simulate: how often the simulator re-pulls its component list")
	return cmd
}

// laneWorkersAuto sizes the lane pool from the node's limits (#1869).
const laneWorkersAuto = "auto"

// laneWorkersFrom is the node's lane count: the flag when given, else
// OMNIGLASS_LANE_WORKERS when set, else auto. Zero is auto, which the node
// resolves from its limits as it starts.
func laneWorkersFrom(flag string, changed bool, env string) (int, error) {
	v, from := flag, "--lane-workers"
	if !changed && env != "" {
		v, from = env, "OMNIGLASS_LANE_WORKERS"
	}
	if strings.EqualFold(v, laneWorkersAuto) {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("node: %s %q is neither a count of lanes nor auto", from, v)
	}
	if n < 1 {
		return 0, fmt.Errorf("node: %s is %d: a node needs at least one lane worker to run its tasks (or auto to size them)", from, n)
	}
	return n, nil
}

// sessionDialsFrom is the node's session-dials bound: the flag when given,
// else OMNIGLASS_NODE_SESSION_DIALS when set, else the flag's default.
func sessionDialsFrom(flag int, changed bool, env string) (int, error) {
	n, from := flag, "--session-dials"
	if !changed && env != "" {
		parsed, err := strconv.Atoi(env)
		if err != nil {
			return 0, fmt.Errorf("node: OMNIGLASS_NODE_SESSION_DIALS %q is not a count of dials", env)
		}
		n, from = parsed, "OMNIGLASS_NODE_SESSION_DIALS"
	}
	if n < 1 {
		return 0, fmt.Errorf("node: %s is %d; it must be at least 1", from, n)
	}
	return n, nil
}
