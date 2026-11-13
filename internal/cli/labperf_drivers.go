package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// driverSweepProject is the sweep's throwaway lab, never the dev or scale
// lab's project.
const driverSweepProject = "omniglass-drivers"

// driverSweepConfig reads `lab perf drivers`'s flags into the sweep's
// config and where its table goes.
func driverSweepConfig(cmd *cobra.Command, binary, dockerHost string) (labfootprint.DriverConfig, string) {
	f := cmd.Flags()
	cfg := labfootprint.DriverConfig{Binary: binary, DockerHost: dockerHost, Project: driverSweepProject}
	cfg.Image, _ = f.GetString("image")
	cfg.Only, _ = f.GetStringSlice("only")
	cfg.StepTasks, _ = f.GetInt("step-tasks")
	cfg.Warmup, _ = f.GetDuration("warmup")
	cfg.Window, _ = f.GetDuration("window")
	out, _ := f.GetString("out")
	return cfg, out
}

func newLabPerfDriversCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drivers",
		Short: "Measure what each shipped driver's polls cost a node, and write the table the docs render",
		Long: "Brings up a throwaway lab with no fleet and, for each shipped driver in turn, a node of its own with the " +
			"simulator beside it in its network namespace, so the node's cgroup holds only the node's work. Each driver " +
			"is measured idle, then at --step-tasks and twice that many poll tasks; the table is the slope of the node's " +
			"memory and CPU over its task count, with the threads' own CPU per run from poll-cpu-time beside it. A " +
			"driver whose steps do not fit a line is listed as not measured, with why. The sweep takes hours: each " +
			"step's window holds the driver's slowest poll.\n\n" +
			"The table is a host's: run it on the host class it describes, then run `go run ./cmd/labgen` and commit " +
			"both files in a reviewed change.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			log := cmd.ErrOrStderr()
			_, daemon, err := labEngine(cmd.Context(), log)
			if err != nil {
				return err
			}
			binary, err := os.Executable()
			if err != nil {
				return err
			}
			cfg, out := driverSweepConfig(cmd, binary, daemon.Host)
			cfg.Out, cfg.LabLog = log, log
			table, err := labfootprint.MeasureDrivers(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			b, err := table.JSON()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(out, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(log, "wrote %s: %d drivers measured, %d not\n", out, len(table.Rows), len(table.NotMeasured))
			return nil
		},
	}
	f := cmd.Flags()
	f.String("image", "", "the omniglass image the lab and the sweep's nodes run (lab run's --image); empty is this binary's own build")
	f.StringSlice("only", nil, "sweep only these drivers; empty sweeps every shipped one")
	f.Int("step-tasks", 400, "about how many poll tasks each step adds")
	f.Duration("warmup", 2*time.Minute, "how long each step runs before it is measured")
	f.Duration("window", 2*time.Minute, "each step's measured window; a driver's slowest poll lengthens it")
	f.String("out", labfootprint.DriverCostPath, "where to write the table, from the repository's root")
	return cmd
}
