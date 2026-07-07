package cli

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newSeedDevCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed-dev",
		Short: "Seed a dev database with example locations, users, and grants (idempotent; never for production)",
		Long: "Populate a fresh dev database with a small example estate so `make dev` comes up " +
			"with locations, sign-in-able users, and their grants instead of empty. The same " +
			"trusted direct-DB lane as bootstrap, and idempotent, so it runs on every `make dev`. " +
			"Not for production: these are operator rows, not ship-with reference data.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSeedDev(cmd.Context())
		},
	}
}

// runSeedDev ensures the schema and reference data exist, then installs the dev
// example estate idempotently (the same trusted lane as bootstrap). The audit
// actor is left empty: these are system-seeded dev rows, not an operator action.
func runSeedDev(ctx context.Context) error {
	c := cfg()
	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	gw, err := storage.NewPG(ctx, c.DSN)
	if err != nil {
		return err
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		return err
	}
	if err := devseed.Run(ctx, gw, ""); err != nil {
		return err
	}
	// Report the fixture's size so the message never drifts from the data. Sites are
	// the campus-typed roots (a multi-site estate); users all share the 'dev' password.
	doc, err := devseed.Fixtures()
	if err != nil {
		return err
	}
	sites := 0
	for _, l := range doc.Locations {
		if l.Type == "campus" {
			sites++
		}
	}
	fmt.Printf("dev example data seeded: %d sites, %d locations, %d users (password 'dev').\n",
		sites, len(doc.Locations), len(doc.Users))
	return nil
}
