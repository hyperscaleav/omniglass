package cli

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token <username>",
		Short: "Mint an additional bearer token for an existing principal (direct DB)",
		Long: "Issues a new bearer credential for an existing principal, addressed by username, and prints " +
			"the token once. The same trusted direct-DB lane as bootstrap: token reissue, break-glass, or a " +
			"fresh login token for `make dev` when the owner already exists.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToken(cmd.Context(), args[0])
		},
	}
}

// runToken ensures the schema exists, mints a bearer token, and attaches it to
// the named principal. Errors clearly if the username does not exist.
func runToken(ctx context.Context, username string) error {
	c := cfg()
	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	gw, err := storage.NewPG(ctx, c.DSN)
	if err != nil {
		return err
	}
	defer gw.Close()

	token, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		return err
	}
	// A CLI-minted API token does not expire (nil expiry); session cookies from the
	// web login are the ones with a bounded lifetime.
	ok, err := gw.IssueBearerCredential(ctx, username, hash, prefix, nil)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no principal with username %q (create one first: omniglass bootstrap %s)", username, username)
	}
	fmt.Printf("Bearer token for %q (shown once, store it now):\n\n  %s\n\n", username, token)
	return nil
}
