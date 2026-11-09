package cli

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// The bus logs its lane tallies through the package-level slog functions, so
// the server's text handler must be the process default or the harness reads
// nothing: Go's default handler has no time= key and no msg="..." field.
func TestServerLoggerIsTheProcessDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	log := serverLogger(&buf)
	log.Info("migrations applied")
	slog.Info("ingest lane", "facility", "ingest", "batches", 3, "busy_ms", 120)

	samples, err := labfootprint.ParseLaneLog(&buf)
	if err != nil {
		t.Fatalf("parse the server log: %v\n%s", err, buf.String())
	}
	if len(samples) != 1 || samples[0].Batches != 3 || samples[0].Busy != 120*time.Millisecond {
		t.Fatalf("a package-level tally line did not reach the harness: %+v\n%s", samples, buf.String())
	}
	if time.Since(samples[0].At) > time.Minute {
		t.Errorf("the tally's time= is not the server clock: %s", samples[0].At)
	}
}
