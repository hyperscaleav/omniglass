package cli

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
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

	kek, source, err := secret.LoadKEK(os.Getenv, c.DataDir, func(msg string) { log.Warn(msg) })
	if err != nil {
		return err
	}
	log.Info("secret key loaded", "source", source)

	gw, err := storage.NewPG(ctx, c.DSN, storage.WithSecretProvider(secret.NewStaticProvider(kek)))
	if err != nil {
		return err
	}
	defer gw.Close()

	if err := seed.Run(ctx, gw); err != nil {
		return err
	}
	log.Info("boot seed applied")

	// The embedded NATS server carries the node-server protocol (worklist,
	// heartbeat, and the checkpoint-3 telemetry stream). It is hosted in-process
	// and shut down with the server.
	busSrv, err := startBus(c, gw)
	if err != nil {
		return err
	}
	defer busSrv.Shutdown()
	log.Info("embedded nats-server listening", "addr", c.NatsAddr)

	srv := &http.Server{
		Addr:              c.Addr,
		Handler:           api.NewHandler(gw, api.WithSecureCookies(c.SecureCookies), api.WithNatsURL(c.NatsURL)),
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

// startBus parses the configured NATS listen address and starts the embedded
// server. A "host:" or bare port resolves through net.SplitHostPort; a parse
// failure is a config error surfaced at boot.
func startBus(c config.Config, gw storage.Gateway) (*bus.Server, error) {
	host, portStr, err := net.SplitHostPort(c.NatsAddr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	return bus.New(bus.Config{Host: host, Port: port, StoreDir: c.NatsStoreDir}, gw)
}
