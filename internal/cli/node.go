package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/hyperscaleav/omniglass/internal/node"
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
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the edge node: claim, pull the worklist, and heartbeat over NATS",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if serverURL == "" {
				serverURL = os.Getenv("OMNIGLASS_SERVER")
			}
			if name == "" {
				name = os.Getenv("OMNIGLASS_NODE_NAME")
			}
			if token == "" {
				token = os.Getenv("OMNIGLASS_NODE_TOKEN")
			}
			if serverURL == "" || name == "" || token == "" {
				return fmt.Errorf("node: --server, --name, and --token are required (or set OMNIGLASS_SERVER / OMNIGLASS_NODE_NAME / OMNIGLASS_NODE_TOKEN)")
			}
			wl, err := node.Run(cmd.Context(), node.Config{
				ServerURL: serverURL, Name: name, Token: token,
				HeartbeatEvery: heartbeat, Once: once,
			})
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
	return cmd
}
