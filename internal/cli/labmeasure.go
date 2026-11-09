package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// labMeasureOpts are `lab measure`'s flags.
type labMeasureOpts struct {
	systems    int
	scenario   string
	warmup     time.Duration
	window     time.Duration
	fleetReads int
	backlog    int64
	drainWait  time.Duration
	jsonPath   string
}

// check refuses the flags a measurement cannot run on, before any container
// is touched.
func (o labMeasureOpts) check() error {
	switch {
	case o.systems < 0:
		return fmt.Errorf("--systems %d: want a count of systems, or 0 to measure the lab already running", o.systems)
	case o.window <= 0:
		return fmt.Errorf("--window %s: the window must be longer than zero", o.window)
	case o.warmup < 0:
		return fmt.Errorf("--warmup %s: want zero or more", o.warmup)
	case o.fleetReads < 1:
		return fmt.Errorf("--fleet-reads %d: want at least one timed read", o.fleetReads)
	case o.backlog < 0:
		return fmt.Errorf("--backlog %d: want a row count, or 0 to skip the retention probe", o.backlog)
	case o.backlog > 0 && o.drainWait <= 0:
		return errors.New("--drain-timeout: must be longer than zero")
	}
	return nil
}

// newLabMeasureCmd is `omniglass lab measure` (#1556): the scale lab's
// measurement, one table of what a fleet of this size costs.
func newLabMeasureCmd() *cobra.Command {
	o := labMeasureOpts{}
	cmd := &cobra.Command{
		Use:   "measure",
		Short: "Measure a running scale lab: rows a minute, growth, WAL a sample, CPU and memory, ingest lag, redeliveries, the consumer lane's busy fraction, fleet view times and the retention drain",
		Long: "Measures a scale lab (`omniglass lab run --scenario corporate --systems N`) over a stated window and prints " +
			"one table: rows a minute per lane, table and WAL growth a day extrapolated from the window, CPU and memory " +
			"per container, WAL bytes a sample, ingest lag from probe to row (p50, p95, p99), consumer redeliveries, the " +
			"ingest consumer lane's busy fraction and service time a batch, the console's fleet view response " +
			"times, and the retention pass's drain rate. It writes the same figures as JSON so two runs compare.\n\n" +
			"With no --systems it measures the scale lab already running. With --systems it brings up a throwaway scale " +
			"lab of that size, measures it, and removes it, so the one command is the whole run.\n\n" +
			"When no sample probed in the window had landed by its end, the ingest lag cell reads not measured, with the " +
			"reason and the age of the newest landed sample under the table, and every other figure still prints. The " +
			"consumer lane row is read from the server's periodic `ingest lane` log line and needs two tallies, 30 s " +
			"apart, inside the window; a window too short to hold them reads not measured, with the reason under the table.\n\n" +
			"The retention probe plants a backlog of aged property rows and restarts the server to run its pass; " +
			"--backlog 0 skips it and leaves the server alone. A lab started before this build's `lab run` does not " +
			"record commit timestamps, which the ingest lag reads: bring it up again.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.check(); err != nil {
				return err
			}
			ctx := cmd.Context()
			_, daemon, err := labEngine(ctx, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			cfg := labfootprint.ScaleConfig{
				Scenario: o.scenario, Systems: o.systems, DockerHost: daemon.Host,
				ReadyTimeout: defaultScaleTimeout, Warmup: o.warmup, Window: o.window,
				FleetReads: o.fleetReads, Backlog: o.backlog, DrainTimeout: o.drainWait,
				Out: cmd.ErrOrStderr(), LabLog: cmd.ErrOrStderr(),
			}
			if o.systems > 0 {
				if cfg.Binary, err = os.Executable(); err != nil {
					return err
				}
			}
			rep, err := labfootprint.MeasureScale(ctx, cfg)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), "\n"+rep.Table())
			if o.jsonPath == "" {
				return nil
			}
			b, err := json.MarshalIndent(rep, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(o.jsonPath, append(b, '\n'), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", o.jsonPath)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&o.systems, "systems", 0, "bring up a throwaway scale lab of this many systems, measure it and remove it; 0 measures the one already running")
	f.StringVar(&o.scenario, "scenario", "corporate", "the scale scenario a --systems lab runs")
	f.DurationVar(&o.warmup, "warmup", 2*time.Minute, "how long the lab runs before the window starts")
	f.DurationVar(&o.window, "window", 5*time.Minute, "the window the rates, growth and usage are read over")
	f.IntVar(&o.fleetReads, "fleet-reads", 20, "how many timed fleet view reads, spread over the window")
	f.Int64Var(&o.backlog, "backlog", 150000, "aged property rows the retention probe plants; 0 skips the probe and its server restart")
	f.DurationVar(&o.drainWait, "drain-timeout", 3*time.Minute, "how long the retention probe waits for the pass to clear the backlog")
	f.StringVar(&o.jsonPath, "json", "lab-measure.json", "also write the figures here as JSON; empty writes none")
	return cmd
}
