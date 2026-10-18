package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/simnode"
)

// newSimCmd is the simulator run mode's group. `sim run` serves the devices
// placed on a node, beside that node, with that node's credential; it reads
// the same OMNIGLASS_* identity env as `node run`, so a sidecar in the
// node's pod needs no secret of its own.
func newSimCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "sim",
		Short: "Simulate the devices placed on a node",
	}
	var (
		serverURL   string
		name        string
		token       string
		portOffset  int
		poll        time.Duration
		impairments bool
	)
	run := &cobra.Command{
		Use:   "run",
		Short: "Serve the node's simulated devices and follow changes until interrupted",
		Long: "Serves each component placed on the node whose product has a simulator profile and whose " +
			"endpoints target a loopback address, on that address, and reports every request it answers " +
			"to the server. Re-pulls the device list on an interval, so devices added or removed on the " +
			"server start or stop without a restart.",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
				return fmt.Errorf("sim: --server, --name, and --token are the node's (or set OMNIGLASS_SERVER / OMNIGLASS_NODE_NAME / OMNIGLASS_NODE_TOKEN)")
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return simnode.Run(ctx, simnode.Config{
				ServerURL: serverURL, Name: name, Token: token,
				PortOffset: portOffset, Poll: poll, Impairments: impairments,
			})
		},
	}
	run.Flags().StringVar(&serverURL, "server", "", "Omniglass server base URL (env OMNIGLASS_SERVER)")
	run.Flags().StringVar(&name, "name", "", "the node this simulator runs beside (env OMNIGLASS_NODE_NAME)")
	run.Flags().StringVar(&token, "token", "", "that node's enrollment token (env OMNIGLASS_NODE_TOKEN)")
	run.Flags().IntVar(&portOffset, "port-offset", 0, "lift the simulated ports below 1024 by this much, to serve without the privilege to bind them")
	run.Flags().DurationVar(&poll, "poll", 10*time.Second, "how often to re-pull the device list")
	run.Flags().BoolVar(&impairments, "impairments", false, "apply impairments (unplug, latency, loss) and truthful ping by rewriting the firewall and traffic shaping of this network namespace; needs NET_ADMIN, and is meant for a namespace of the simulator's own (a pod or container)")
	group.AddCommand(run)
	return group
}
