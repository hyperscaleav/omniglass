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
	var revokeTokens bool
	cmd := &cobra.Command{
		Use:   "set-password <username> <password>",
		Short: "Set or rotate a user's console password, revoking their sessions (direct DB)",
		Long: "Installs or replaces a human's password credential (argon2id), addressed by username, and " +
			"revokes the user's live SESSIONS so a break-glass reset locks out any stolen login at once. API " +
			"tokens are a separate bearer secret, not tied to the password, and are kept unless --revoke-tokens " +
			"is given (a full lockout of a compromised account). The same trusted direct-DB lane as bootstrap " +
			"and token: dev setup, break-glass, or a password reset before the admin UI lands.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetPassword(cmd.Context(), args[0], args[1], revokeTokens)
		},
	}
	cmd.Flags().BoolVar(&revokeTokens, "revoke-tokens", false,
		"also revoke the user's API tokens (a full lockout of a compromised account)")
	return cmd
}

// runSetPassword ensures the schema exists, hashes the password, and sets it on the
// named human, then revokes the user's live sessions so a break-glass reset takes
// effect at once (a stolen session cookie stops working). API tokens are kept unless
// revokeTokens is set. Errors clearly if the username does not exist.
func runSetPassword(ctx context.Context, username, password string, revokeTokens bool) error {
	c := cfg()
	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	gw, err := storage.NewPG(ctx, c.DSN)
	if err != nil {
		return err
	}
	defer gw.Close()

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
	// Break-glass is a lockout: after rotating the secret, revoke the user's live
	// sessions so any stolen login stops at once. Tokens are their own bearer secret,
	// not tied to the password, so they are kept unless --revoke-tokens asks for a full
	// lockout of a compromised account.
	pid, err := gw.ResolvePrincipalRef(ctx, username)
	if err != nil {
		return fmt.Errorf("resolve %q after set-password: %w", username, err)
	}
	nSessions, err := gw.RevokeBearersByPurpose(ctx, pid, "session")
	if err != nil {
		return err
	}
	nTokens := 0
	if revokeTokens {
		if nTokens, err = gw.RevokeBearersByPurpose(ctx, pid, "token"); err != nil {
			return err
		}
	}
	if revokeTokens {
		fmt.Printf("password set for %q; revoked %d session(s) and %d token(s).\n", username, nSessions, nTokens)
	} else {
		fmt.Printf("password set for %q; revoked %d session(s) (tokens kept; pass --revoke-tokens to revoke them too).\n", username, nSessions)
	}
	return nil
}
