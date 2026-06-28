package cli

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newServerCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the control-plane server (HTTP API)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(cmd.Context(), version)
		},
	}
}

// runServer boots the control plane: apply migrations, open the Storage
// Gateway, serve the HTTP API, and shut down gracefully on SIGINT/SIGTERM.
func runServer(ctx context.Context, _ string) error {
	c := cfg()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := migrate.Run(c.DSN); err != nil {
		return err
	}
	log.Info("migrations applied")

	gw, err := storage.NewPG(ctx, c.DSN)
	if err != nil {
		return err
	}
	defer gw.Close()

	srv := &http.Server{
		Addr:              c.Addr,
		Handler:           api.NewHandler(gw),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("http api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errc:
		return err
	case <-sig:
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
