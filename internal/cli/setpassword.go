package cli

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newSetPasswordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-password <username> <password>",
		Short: "Set or rotate a user's console password (direct DB)",
		Long: "Installs or replaces a human's password credential (argon2id), addressed by username. The " +
			"same trusted direct-DB lane as bootstrap and token: dev setup, break-glass, or a password reset " +
			"before the admin UI lands.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetPassword(cmd.Context(), args[0], args[1])
		},
	}
}

// runSetPassword ensures the schema exists, hashes the password, and sets it on
// the named human. Errors clearly if the username does not exist.
func runSetPassword(ctx context.Context, username, password string) error {
	c := cfg()
	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	gw, err := storage.NewPG(ctx, c.DSN)
	if err != nil {
		return err
	}
	defer gw.Close()

	if err := auth.ValidatePassword(password, username); err != nil {
		return fmt.Errorf("password rejected (%s): %w", auth.PasswordRequirements(), err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	ok, err := gw.SetPassword(ctx, username, hash)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no principal with username %q (create one first: omniglass bootstrap %s)", username, username)
	}
	fmt.Printf("password set for %q.\n", username)
	return nil
}
