package cli

import (
	"context"
	"fmt"
	"os"
)

// Dev points the stack at an already-running Postgres (dsn), mints a dev owner
// (idempotent; the token is printed once and persists with the data), and serves
// the API and embedded console until Ctrl-C. The embedded-Postgres lifecycle
// lives in cmd/ogdev, not here, so the shipped binary never links that
// dependency; this is the reusable seam ogdev drives.
func Dev(ctx context.Context, dsn, version string) error {
	_ = os.Setenv("OMNIGLASS_DSN", dsn)
	if err := runBootstrap(ctx, "dev", "", ""); err != nil {
		return err
	}
	fmt.Println("\nconsole: http://localhost:8080/web  (paste the token above to sign in)")
	return runServer(ctx, version)
}
