package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/mailsim"
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
	group.AddCommand(run, newSimMailCmd())
	return group
}

// newSimMailCmd is `sim mail`: the mail relay simulator (#1317), an SMTP server
// that keeps what it is sent and sends nothing on, so a lab or a demo proves
// an email notification without a real relay. Each accepted message is one
// JSON line on --out; a --down-file whose content is "down" makes it refuse
// every message with a temporary error, as a relay outage would.
func newSimMailCmd() *cobra.Command {
	var listen, out, downFile string
	cmd := &cobra.Command{
		Use:   "mail",
		Short: "Run a mail relay simulator that keeps what it is sent",
		Long: "Serves SMTP on --listen and keeps every message it accepts, writing each as one JSON line " +
			"(from, to, subject, text, message_id) to --out (standard output by default). It sends nothing on. " +
			"Point the notification settings' mail_relay at it with mail_security none and name its address in " +
			"egress_allow. While --down-file holds the word down, it refuses every message with a temporary error.",
		RunE: func(cmd *cobra.Command, _ []string) (retErr error) {
			w := cmd.OutOrStdout()
			if out != "" {
				f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err != nil {
					return err
				}
				// A failed close can lose kept messages, so it is the command's
				// error unless an earlier one already is.
				defer func() {
					if cerr := f.Close(); cerr != nil && retErr == nil {
						retErr = cerr
					}
				}()
				w = f
			}
			opts := mailsim.Options{Out: w}
			if downFile != "" {
				opts.Failing = func() bool {
					b, err := os.ReadFile(downFile)
					return err == nil && strings.TrimSpace(string(b)) == "down"
				}
			}
			srv, err := mailsim.Start(listen, opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "mail sim listening on %s\n", srv.Addr())
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			<-ctx.Done()
			return srv.Close()
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:2525", "the address to serve SMTP on")
	cmd.Flags().StringVar(&out, "out", "", "append each accepted message as a JSON line to this file (default standard output)")
	cmd.Flags().StringVar(&downFile, "down-file", "", "while this file holds the word down, refuse every message with a temporary error")
	return cmd
}
