// Command ogdev runs the full local stack with no Docker: an embedded Postgres
// plus the server and operator console, in one process. It is a developer tool,
// run via `make dev`; the shipped binary (cmd/omniglass) never imports
// embedded-postgres, so that dependency stays out of production builds. Build
// with `-tags web` so the console is embedded.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/hyperscaleav/omniglass/internal/cli"
)

func main() {
	// Pick a free port so the embedded dev Postgres never collides with a
	// system Postgres (which may already hold 5432 or 5433).
	port, err := freePort()
	if err != nil {
		log.Fatalf("ogdev: pick port: %v", err)
	}
	dsn := fmt.Sprintf("postgres://omniglass:omniglass@localhost:%d/omniglass?sslmode=disable", port)

	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("omniglass").
		Password("omniglass").
		Database("omniglass").
		Port(uint32(port)).
		RuntimePath(".dev/pg").
		DataPath(".dev/pgdata").
		BinariesPath(".dev/pgbin").
		Logger(os.Stderr))

	if err := pg.Start(); err != nil {
		log.Fatalf("ogdev: start embedded postgres: %v", err)
	}
	defer func() { _ = pg.Stop() }()

	// cli.Dev mints the dev owner and serves until Ctrl-C (the server handles
	// SIGINT itself); the deferred Stop then shuts Postgres down.
	if err := cli.Dev(context.Background(), dsn, "dev"); err != nil {
		_ = pg.Stop()
		log.Fatalf("ogdev: %v", err)
	}
}

// freePort asks the OS for an unused TCP port and returns it. The listener is
// closed immediately; the brief window before Postgres binds is acceptable for a
// local dev tool.
func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
