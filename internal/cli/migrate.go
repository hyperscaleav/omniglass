package cli

import (
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply embedded database migrations (dbmate)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := migrate.Run(cfg().DSN); err != nil {
				return err
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}
