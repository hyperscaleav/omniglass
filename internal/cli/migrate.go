package cli

import (
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply embedded database migrations (dbmate)",
		Long: "Applies every embedded migration the database has not run. Migrations only move forward, " +
			"so a database a newer release migrated is refused, naming its newest migration and the newest " +
			"this binary knows: run that release or a later one.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := migrate.Run(cfg().DSN); err != nil {
				return err
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}
