package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// A sweep runs in a throwaway lab of its own and writes the table the docs
// render, from the repository's root, unless told otherwise.
func TestADriverSweepsFlagsAreItsConfig(t *testing.T) {
	cmd := newLabPerfDriversCmd()
	if err := cmd.ParseFlags([]string{"--only", "http-json,kestrel-relay-api", "--step-tasks", "100", "--window", "90s", "--image", "omniglass:x"}); err != nil {
		t.Fatal(err)
	}
	cfg, out := driverSweepConfig(cmd, "/bin/og", "unix:///d.sock")
	if cfg.Project != driverSweepProject || cfg.Binary != "/bin/og" || cfg.DockerHost != "unix:///d.sock" || cfg.Image != "omniglass:x" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.StepTasks != 100 || cfg.Window != 90*time.Second || cfg.Warmup != 2*time.Minute {
		t.Fatalf("steps = %d, window %s, warmup %s", cfg.StepTasks, cfg.Window, cfg.Warmup)
	}
	if !slices.Equal(cfg.Only, []string{"http-json", "kestrel-relay-api"}) {
		t.Fatalf("only = %v", cfg.Only)
	}
	if out != labfootprint.DriverCostPath {
		t.Fatalf("out = %s, want the committed table", out)
	}
}

// A sweep that stops partway (interrupted, or its teardown left something)
// still writes what it measured, and still fails.
func TestAStoppedSweepKeepsWhatItMeasured(t *testing.T) {
	out := filepath.Join(t.TempDir(), "d", "shipped.json")
	table := labfootprint.DriverCostTable{Date: "d", Rows: []labfootprint.DriverCost{{Driver: "a"}}}
	stopped := errors.New("interrupted")
	var log bytes.Buffer
	if err := saveDriverTable(&log, out, table, stopped); !errors.Is(err, stopped) {
		t.Fatalf("err = %v, want the sweep's own", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := labfootprint.ReadDriverCostTable(b); err != nil || len(got.Rows) != 1 {
		t.Fatalf("written %s, %v, want the measured row", b, err)
	}
	if !strings.Contains(log.String(), "1 drivers measured") || !strings.Contains(log.String(), "stopped") {
		t.Fatalf("log = %q", log.String())
	}
	// A sweep that never reached its first driver has nothing to keep.
	empty := filepath.Join(t.TempDir(), "none.json")
	if err := saveDriverTable(&log, empty, labfootprint.DriverCostTable{}, stopped); !errors.Is(err, stopped) {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Fatalf("a sweep that measured nothing wrote %s", empty)
	}
}
