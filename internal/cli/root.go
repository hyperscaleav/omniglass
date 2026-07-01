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
	)
	// Generated commands: one per API operation, sharing the connection flags.
	// The two sets compose on the same root; regeneration touches only the
	// generated set.
	addClientFlags(root)
	root.AddCommand(generatedCommands()...)

	return root
}

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
