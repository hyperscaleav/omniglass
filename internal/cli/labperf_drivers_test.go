package cli

import (
	"slices"
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
