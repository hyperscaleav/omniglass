package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newTokenCmd() *cobra.Command {
	var ttl time.Duration
	var description string
	cmd := &cobra.Command{
		Use:   "token <username>",
		Short: "Mint an additional bearer token for an existing principal (direct DB)",
		Long: "Issues a new bearer credential for an existing principal, addressed by username, and prints " +
			"the token once. The same trusted direct-DB lane as bootstrap: token reissue, break-glass, or a " +
			"fresh login token for `make dev` when the owner already exists. A --description (required) names " +
			"what the token is for. The token expires after --ttl (default 90 days, hard maximum 365 days); " +
			"every credential is time-bounded.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToken(cmd.Context(), args[0], ttl, description)
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", auth.DefaultTokenLifetime,
		"how long the token is valid before it expires (max 365 days)")
	cmd.Flags().StringVar(&description, "description", "", "what the token is for (required)")
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

// runToken ensures the schema exists, mints a bearer token with a bounded lifetime,
// and attaches it to the named principal. It rejects a --ttl above the hard cap and
// errors clearly if the username does not exist.
func runToken(ctx context.Context, username string, ttl time.Duration, description string) error {
	if ttl > auth.MaxTokenLifetime {
		return fmt.Errorf("--ttl %s exceeds the maximum token lifetime of %s", ttl, auth.MaxTokenLifetime)
	}
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
	// A CLI-minted API token is a bounded 'token' credential: no eternal secret. It
	// carries the operator's description; the CLI has no browser, so no user-agent / ip.
	expiresAt := time.Now().Add(ttl)
	ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{
		Username: username, SecretHash: hash, Prefix: prefix, Purpose: "token",
		ExpiresAt: &expiresAt, Description: description,
	})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no principal with username %q (create one first: omniglass bootstrap %s)", username, username)
	}
	fmt.Printf("Bearer token for %q (shown once, store it now; expires %s):\n\n  %s\n\n",
		username, expiresAt.Format(time.RFC3339), token)
	return nil
}
