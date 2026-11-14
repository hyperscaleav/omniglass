// Package cli is the single binary's command surface: the run modes (server,
// migrate) for the walking skeleton. Operator/dev subcommands (thin API
// clients, never direct Postgres) and the node run mode arrive in later slices.
package cli

import (
	"fmt"
	"os"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/spf13/cobra"
)

func newRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "omniglass",
		Short:         "Omniglass control plane (walking skeleton)",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// cobra's Print* default to stderr; route them to stdout so future data
	// commands stay pipeable. Errors continue through Execute -> os.Stderr.
	root.SetOut(os.Stdout)

	// Hand-written commands: the run modes and the trusted bootstrap lane.
	root.AddCommand(
		newServerCmd(version),
		newMigrateCmd(),
		newBootstrapCmd(),
		newTokenCmd(),
		newSetPasswordCmd(),
		newSeedDevCmd(),
		newSimCmd(),
		newLabCmd(version),
		newMCPCmd(version),
	)
	// Generated commands: one per API operation, sharing the connection flags.
	// The two sets compose on the same root; regeneration touches only the
	// generated set.
	addClientFlags(root)
	root.AddCommand(generatedCommands()...)

	// The edge run mode hangs off the generated `node` group rather than the
	// root, so `node run` sits beside `node list`. Attached after generation
	// because the group is generated; if the API ever stops exposing node
	// routes the mode would vanish silently, which TestNodeRunIsReachable
	// catches.
	if g, _, err := root.Find([]string{"node"}); err == nil && g != root {
		g.AddCommand(newNodeRunCmd())
	}

	rejectUnknownVerbs(root)

	return root
}

// groupAnnotation marks a command that is a pure group (children, no action of
// its own) so the docs generator and the docs-invocation check can still tell it
// from a runnable command after rejectUnknownVerbs gives it a RunE.
const groupAnnotation = "omniglass/group"

// IsGroup reports whether cmd is a pure command group: it only routes to its
// subcommands and takes no arguments of its own.
func IsGroup(cmd *cobra.Command) bool { return cmd.Annotations[groupAnnotation] == "true" }

// rejectUnknownVerbs walks the assembled tree and makes every pure group (children,
// no Run/RunE) reject an unrecognised positional. Cobra's own legacyArgs check
// applies only to the root, and it validates Args only on a runnable command, so
// a bare `Args: cobra.NoArgs` on a group is ignored and `component alarm raise x`
// would print help and exit 0. The group therefore gets a RunE that prints help
// when invoked bare and otherwise fails through cobra.NoArgs, whose message is
// `unknown command "raise" for "omniglass component alarm"`.
func rejectUnknownVerbs(parent *cobra.Command) {
	for _, c := range parent.Commands() {
		if c.HasSubCommands() && c.Run == nil && c.RunE == nil {
			c.Args = cobra.NoArgs
			c.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
			if c.Annotations == nil {
				c.Annotations = map[string]string{}
			}
			c.Annotations[groupAnnotation] = "true"
		}
		rejectUnknownVerbs(c)
	}
}

// Root returns the fully assembled command tree without executing it. It is the
// seam the docs generator (cmd/docsgen) walks to render the CLI reference, so the
// reference is always the real command surface, never a hand-maintained copy.
func Root(version string) *cobra.Command { return newRoot(version) }

// Execute runs the root command. version is the build-time-injected release
// tag (or "dev" for local builds), surfaced via `omniglass --version`.
func Execute(version string) {
	if err := newRoot(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// cfg resolves runtime configuration from the environment. Centralized here so
// every run mode reads the same resolved Config.
func cfg() config.Config { return config.Load() }
