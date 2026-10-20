package cli

import (
	"context"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"os"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newSeedDevCmd() *cobra.Command {
	var simFleet bool
	var simPortOffset int
	var scenario string
	cmd := &cobra.Command{
		Use:   "seed-dev",
		Short: "Seed a dev database with example locations, users, and grants (idempotent; never for production)",
		Long: "Populate a fresh dev database with a small example fleet so `make dev` comes up " +
			"with locations, sign-in-able users, and their grants instead of empty. The same " +
			"trusted direct-DB lane as bootstrap, and idempotent, so it runs on every `make dev`. " +
			"Not for production: these are operator rows, not ship-with reference data.\n\n" +
			"--scenario picks the fleet:\n" + scenarioHelp(),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := devseed.LookupScenario(scenario)
			if err != nil {
				return err
			}
			return runSeedDev(cmd.Context(), sc, simFleet, simPortOffset)
		},
	}
	cmd.Flags().BoolVar(&simFleet, "sim-fleet", true, "wire the simulated fleet: addresses and endpoints on the dev node for every component the simulator serves (make dev-sim collects from them)")
	cmd.Flags().IntVar(&simPortOffset, "sim-port-offset", devseed.SimPortOffset, "the lift the simulator applies to ports below 1024, which the simulated fleet's endpoints target: the default for make dev-sim's unprivileged simulator, 0 for one that binds the real ports (in a container)")
	cmd.Flags().StringVar(&scenario, "scenario", devseed.Scenarios()[0].Name, "the fleet to seed (see the list above)")
	return cmd
}

// scenarioHelp lists the scenarios from the registry, so the help never drifts
// from what the flag accepts.
func scenarioHelp() string {
	var b strings.Builder
	for _, s := range devseed.Scenarios() {
		fmt.Fprintf(&b, "  %-9s %s\n", s.Name, s.Summary)
	}
	return b.String()
}

// runSeedDev ensures the schema and reference data exist, then installs the dev
// example fleet idempotently (the same trusted lane as bootstrap). The audit
// actor is left empty: these are system-seeded dev rows, not an operator action.
func runSeedDev(ctx context.Context, sc devseed.Scenario, simFleet bool, simPortOffset int) error {
	c := cfg()
	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	// The same key the server loads, so the secrets the dev fleet seeds (the
	// simulator's SNMP community) are ones the server can decrypt.
	kek, _, err := secret.LoadKEK(config.Get, c.DataDir, func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) })
	if err != nil {
		return err
	}
	gw, err := storage.NewPG(ctx, c.DSN, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		return err
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		return err
	}
	if err := devseed.RunScenario(ctx, gw, "", sc, devseed.Options{SimFleet: simFleet, SimPortOffset: simPortOffset}); err != nil {
		return err
	}
	// Report the fixture's size so the message never drifts from the data. Roots
	// are the locations with no parent, whatever their type: location types are
	// customer data, so nothing here counts by a type's name. Users all share the
	// 'dev' password.
	doc, err := sc.Doc()
	if err != nil {
		return err
	}
	roots := 0
	for _, l := range doc.Locations {
		if l.Parent == "" {
			roots++
		}
	}
	fmt.Printf("dev scenario %q seeded: %d top-level locations, %d locations in all, %d components, %d users (password 'dev').\n",
		sc.Name, roots, len(doc.Locations), len(doc.Components), len(doc.Users))
	return nil
}
