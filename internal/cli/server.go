package cli

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/spf13/cobra"
)

func newServerCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the control-plane server (HTTP API)",
		Long: "Runs the control plane: applies pending migrations and the boot seed, then serves the HTTP " +
			"API on OMNIGLASS_ADDR and hosts the embedded NATS bus nodes connect to on OMNIGLASS_NATS_ADDR. " +
			"Either address may name port 0, which binds whatever free port the kernel hands out; set " +
			"OMNIGLASS_READY_FILE to a path and, once both listeners are bound, the server writes the " +
			"addresses it got there as JSON (addr, nats_addr, and the nats_url it advertises to nodes), " +
			"removing the file at shutdown. A supervisor or a test reads the ports from that file rather " +
			"than picking one and hoping it stays free. Configured by environment; see the deployment guides.",
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

	kek, source, err := secret.LoadKEK(config.Get, c.DataDir, func(msg string) { log.Warn(msg) })
	if err != nil {
		return err
	}
	log.Info("secret key loaded", "source", source)

	// Settings engine: the operator file layer is captured once at boot, the
	// declared defaults are reflected from the type, and the platform DB override
	// is read live per call. The gateway builds the resolver (it IS the override
	// reader) and the API is handed the same one, so the labels the gateway
	// renders and the settings the console reads cannot come from two documents.
	// The file is loaded BEFORE the gateway because the gateway captures it.
	settingsFile, err := settings.LoadFile(c.SettingsFile)
	if err != nil {
		return fmt.Errorf("server: load settings file: %w", err)
	}

	gw, err := storage.NewPG(ctx, c.DSN,
		storage.WithSecretProvider(secret.NewStaticProvider(kek)),
		storage.WithSettingsFile(settingsFile))
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
	log.Info("embedded nats-server listening", "addr", busSrv.Addr().String())
	// Unset, the advertised URL is the bus's own bound address, so a bus
	// asked for port 0 tells nodes the port it got.
	natsURL := c.NatsURL
	if natsURL == "" {
		natsURL = busSrv.LocalURL()
	}

	// Bind before serving, so the address the listener got (the kernel's
	// pick, for port 0) is known and can be reported.
	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           api.NewHandler(gw, api.WithSecureCookies(c.SecureCookies), api.WithNatsURL(natsURL), api.WithSettingsService(gw.Settings()), api.WithTelemetryPublisher(busSrv), api.WithSimHub(busSrv)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("http api listening", "addr", ln.Addr().String())

	// Catch the stop signals before reporting ready: a supervisor that stops
	// the server the moment the ready file lands gets a graceful shutdown.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	removeReady, err := writeReadyFile(c.ReadyFile, readyReport{
		Addr: ln.Addr().String(), NatsAddr: busSrv.Addr().String(), NatsURL: natsURL,
	})
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer removeReady()

	errc := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

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
