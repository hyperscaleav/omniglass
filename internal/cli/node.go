package cli

import (
	"fmt"
	"os"
	"os/signal"
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
		serverURL string
		name      string
		token     string
		heartbeat time.Duration
		once      bool
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
			nodeCfg := node.Config{
				ServerURL: serverURL, Name: name, Token: token,
				HeartbeatEvery: heartbeat, Once: once,
			}
			if simulate {
				// A signal stops both halves gracefully, so the simulator
				// clears any impairment it applied before the process exits.
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return simnode.RunWithNode(ctx, nodeCfg, simnode.Config{
					ServerURL: serverURL, Name: name, Token: token,
					PortOffset: portOffset, Poll: poll, Impairments: impairments,
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
	cmd.Flags().DurationVar(&heartbeat, "heartbeat", 30*time.Second, "heartbeat interval")
	cmd.Flags().BoolVar(&once, "once", false, "run a single claim + pull + heartbeat cycle and exit")
	cmd.Flags().BoolVar(&simulate, "simulate", false, "also run the simulator in this process, serving the simulated components placed on this node on its own loopback")
	cmd.Flags().BoolVar(&impairments, "impairments", false, "with --simulate: apply impairments (unplug, latency, loss) and truthful ping by rewriting the firewall and traffic shaping of this network namespace; needs NET_ADMIN, and is meant for a namespace of the node's own (a container)")
	cmd.Flags().IntVar(&portOffset, "port-offset", 0, "with --simulate: lift the simulated ports below 1024 by this much, to serve without the privilege to bind them")
	cmd.Flags().DurationVar(&poll, "poll", 10*time.Second, "with --simulate: how often the simulator re-pulls its component list")
	return cmd
}
