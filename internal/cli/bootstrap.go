package cli

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newBootstrapCmd() *cobra.Command {
	var email, displayName, password string
	cmd := &cobra.Command{
		Use:   "bootstrap <username>",
		Short: "Create the first owner (idempotent per username) and mint its bearer token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBootstrap(cmd.Context(), args[0], email, displayName, password)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "owner email (optional)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "owner display name (optional)")
	cmd.Flags().StringVar(&password, "password", "", "owner password, so the owner can sign in to the console (optional)")
	return cmd
}

// runBootstrap ensures the schema and official roles exist, then creates the
// first owner directly (the same trusted lane as migrate). Idempotent: a second
// run with the same username mints no second token. A --password also installs a
// password credential so the owner can sign in to the console.
func runBootstrap(ctx context.Context, username, email, displayName, password string) error {
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

	token, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		return err
	}
	var passwordHash string
	if password != "" {
		if err := auth.ValidatePassword(password, username); err != nil {
			return fmt.Errorf("password rejected (%s): %w", auth.PasswordRequirements(), err)
		}
		if passwordHash, err = auth.HashPassword(password); err != nil {
			return err
		}
	}
	created, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
		Username:     username,
		Email:        email,
		DisplayName:  displayName,
		SecretHash:   hash,
		Prefix:       prefix,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return err
	}
	if !created {
		fmt.Printf("owner %q already exists; no token minted.\n", username)
		return nil
	}
	fmt.Printf("owner %q created. Bearer token (shown once, store it now):\n\n  %s\n\n", username, token)
	return nil
}
