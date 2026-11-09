package cli

import (
	"io"
	"log/slog"
)

// serverLogger builds the server's logger and makes it the process default.
// The bus, the storage gateway and every other package that logs through the
// package-level slog functions would otherwise write Go's default handler
// format (a date prefix, no time= or msg= keys), which nothing downstream
// parses: the scale harness reads the server's "ingest lane" tallies and its
// redelivery warnings from the container log by their slog text fields.
func serverLogger(w io.Writer) *slog.Logger {
	log := slog.New(slog.NewTextHandler(w, nil))
	slog.SetDefault(log)
	return log
}
