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

	return root
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
