package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// labPerfOpts are `lab perf`'s flags (#1640).
type labPerfOpts struct {
	size string
	runs int
	// systems, warmup, window and fleetReads override the size's inputs
	// when set; a check takes them from its baseline instead.
	systems    int
	warmup     time.Duration
	window     time.Duration
	fleetReads int
	image      string
	settleWait time.Duration
	// baseline is the baseline file, empty the size's committed one.
	baseline string
	// samples and last read a sampling run's series (#1632) in place of
	// fresh labs.
	samples string
	last    int
	// appendPath keeps each fresh run's report as a sample line, so runs a
	// baseline refused can be read again with --samples.
	appendPath string
	jsonPath   string
}

func labPerfOptsFrom(cmd *cobra.Command) labPerfOpts {
	fs := cmd.Flags()
	o := labPerfOpts{}
	o.size, _ = fs.GetString("size")
	o.runs, _ = fs.GetInt("runs")
	o.systems, _ = fs.GetInt("systems")
	o.warmup, _ = fs.GetDuration("warmup")
	o.window, _ = fs.GetDuration("window")
	o.fleetReads, _ = fs.GetInt("fleet-reads")
	o.image, _ = fs.GetString("image")
	o.settleWait, _ = fs.GetDuration("settle-timeout")
	o.baseline, _ = fs.GetString("baseline")
	o.samples, _ = fs.GetString("samples")
	o.last, _ = fs.GetInt("last")
	o.appendPath, _ = fs.GetString("append")
	o.jsonPath, _ = fs.GetString("json")
	return o
}

// check refuses the flags a gate cannot run on, before any container is
// touched.
func (o labPerfOpts) check() error {
	if _, err := lab.PerfSizeNamed(o.size); err != nil {
		return fmt.Errorf("--size: %w", err)
	}
	switch {
	case o.runs < 1:
		return fmt.Errorf("--runs %d: want at least one run", o.runs)
	case o.systems < 0:
		return fmt.Errorf("--systems %d: want a count of systems, or 0 for the size's", o.systems)
	case o.warmup < 0:
		return fmt.Errorf("--warmup %s: want zero or more, or 0 for the size's", o.warmup)
	case o.window < 0 || o.window > labfootprint.MaxDeliveryWindow:
		return fmt.Errorf("--window %s: want a window up to 23h, or 0 for the size's", o.window)
	case o.fleetReads < 0:
		return fmt.Errorf("--fleet-reads %d: want a count, or 0 for the size's", o.fleetReads)
	case o.settleWait <= 0:
		return fmt.Errorf("--settle-timeout %s: the delivery read needs a bound longer than zero", o.settleWait)
	case o.last < 0:
		return fmt.Errorf("--last %d: want a count of samples, or 0 for --runs", o.last)
	case o.last > 0 && o.samples == "":
		return fmt.Errorf("--last %d reads the end of a series: add --samples with the series' file", o.last)
	case o.samples != "" && o.image != "":
		return errors.New("--image brings up fresh labs: --samples reads a series already measured, so drop one")
	case o.samples != "" && o.appendPath != "":
		return errors.New("--append keeps fresh runs: --samples reads a series already measured, so drop one")
	}
	return nil
}

// inputs is what the size's runs measure with, overrides applied, with the
// device set its scenario places at that size.
func (o labPerfOpts) inputs() (labfootprint.PerfInputs, error) {
	s, err := lab.PerfSizeNamed(o.size)
	if err != nil {
		return labfootprint.PerfInputs{}, err
	}
	if o.systems > 0 {
		s.Systems = o.systems
	}
	if o.warmup > 0 {
		s.Warmup = o.warmup
	}
	if o.window > 0 {
		s.Window = o.window
	}
	if o.fleetReads > 0 {
		s.FleetReads = o.fleetReads
	}
	return labfootprint.PerfInputsOf(s)
}

// baselinePath is --baseline, or the size's committed baseline from the
// repository's root.
func (o labPerfOpts) baselinePath() string {
	if o.baseline != "" {
		return o.baseline
	}
	s, err := lab.PerfSizeNamed(o.size)
	if err != nil {
		return ""
	}
	return filepath.Join("internal", "labfootprint", "baseline", s.BaselineFile())
}

// scaleConfig is one run's measurement: a throwaway lab of the inputs'
// fleet, with the retention probe and the system page load off, since
// neither is gated and the probe restarts the server.
func (o labPerfOpts) scaleConfig(in labfootprint.PerfInputs, binary, dockerHost string) labfootprint.ScaleConfig {
	return labfootprint.ScaleConfig{
		Binary: binary, Image: o.image, DockerHost: dockerHost,
		Scenario: in.Scenario, Systems: in.Systems, ReadyTimeout: defaultScaleTimeout,
		Warmup: time.Duration(in.WarmupS) * time.Second, Window: time.Duration(in.WindowS) * time.Second,
		FleetReads: in.FleetReads, SettleTimeout: o.settleWait,
	}
}

// newLabPerfCmd is `omniglass lab perf` (#1640): the scale lab measured
// against a committed baseline.
func newLabPerfCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "perf",
		Short: "Check a fresh scale lab against the committed performance baseline, or regenerate it",
	}
	group.AddCommand(newLabPerfCheckCmd(), newLabPerfBaselineCmd())
	return group
}

func labPerfFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("size", "ci", "the gated size: ci (corporate at 300 systems) or laptop (corporate at 1000)")
	f.Int("runs", 3, "how many throwaway labs to measure, one after another; every gated figure is their median")
	f.String("image", "", "the omniglass image each lab runs (lab run's --image); empty is this binary's own build")
	f.Duration("settle-timeout", 3*time.Minute, "how long each run's delivery read waits for its window's batches to land")
	f.String("samples", "", "read a sampling run's series (lab measure --every --append) in place of fresh labs")
	f.Int("last", 0, "with --samples: how many samples from the series' end to read; 0 reads --runs")
	f.String("append", "", "keep each fresh run's report as a line in this file, which --samples reads again")
}

func newLabPerfCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Measure fresh scale labs and fail when a figure regressed past the baseline",
		Long: "Brings up --runs throwaway scale labs one after another, each measured with the baseline's own inputs " +
			"(scenario, systems, warmup, window and fleet reads, with the retention probe off), and compares the runs " +
			"with the baseline. It prints one row a figure and exits nonzero when any reads fail, or when the runs are " +
			"invalid.\n\n" +
			"The exact figures (every closed-loop gap, an ingest lag not measured, redeliveries, each kind's delivery " +
			"loss) fail at any value but 0. The host-independent figures (rows a minute, growth and WAL per system, " +
			"samples a batch) gate on any host. The host-dependent figures (latency, CPU, memory, fleet view time) gate " +
			"only on the host the baseline was generated on; on another they read reported. A gated figure is the " +
			"median of the runs, and fails only past its tolerance and past the runs' combined spread. Runs that spread " +
			"past the tolerance read noisy, which does not fail. A figure better than its tolerance reads improved: " +
			"refresh the baseline.\n\n" +
			"Runs are invalid, and no figure is compared, when they measured another fleet, window or device set than " +
			"the baseline's, or a node restarted or carried an impairment during the window.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := labPerfOptsFrom(cmd)
			if err := o.check(); err != nil {
				return err
			}
			base, err := readBaselineFile(o.baselinePath())
			if err != nil {
				return err
			}
			runs, err := o.collect(cmd.Context(), base.Inputs, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			v := labfootprint.ComparePerf(base, runs, nil)
			fmt.Fprintln(cmd.OutOrStdout())
			v.WriteTable(cmd.OutOrStdout())
			if o.jsonPath != "" {
				b, err := json.MarshalIndent(v, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(o.jsonPath, append(b, '\n'), 0o644); err != nil {
					return err
				}
			}
			if v.Failed() {
				return fmt.Errorf("perf gate: %s against %s", v.Status, o.baselinePath())
			}
			return nil
		},
	}
	labPerfFlags(cmd)
	cmd.Flags().String("baseline", "", "the baseline to check against; empty is the size's committed one under internal/labfootprint/baseline")
	cmd.Flags().String("json", "", "also write the verdict here as JSON")
	return cmd
}

func newLabPerfBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Measure fresh scale labs and write the size's baseline from them",
		Long: "Brings up --runs throwaway scale labs of the size, one after another, and writes the baseline the check " +
			"compares with: each gated figure's median, spread and values, the inputs the runs measured with and the " +
			"host they ran on. It refuses, naming each, when an exact figure is not 0, a figure was not measured in " +
			"every run, or a figure's runs spread past its tolerance, which a check could never gate. The baseline is " +
			"a host's: generate it on the host class it gates, and commit the file in a reviewed change.\n\n" +
			"--systems, --warmup, --window and --fleet-reads override the size's inputs, for a throwaway baseline a " +
			"quick check can run against; the committed baselines use the size's own.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := labPerfOptsFrom(cmd)
			if err := o.check(); err != nil {
				return err
			}
			in, err := o.inputs()
			if err != nil {
				return err
			}
			runs, err := o.collect(cmd.Context(), in, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			base, err := labfootprint.GenerateBaseline(in, runs, nil)
			if err != nil {
				return err
			}
			out, _ := cmd.Flags().GetString("out")
			if out == "" {
				out = o.baselinePath()
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			f, err := os.Create(out)
			if err != nil {
				return err
			}
			if err := base.Write(f); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s: %d figures over %d runs on %s\n", out, len(base.Figures), base.Runs, base.Host)
			return nil
		},
	}
	labPerfFlags(cmd)
	f := cmd.Flags()
	f.String("out", "", "where to write the baseline; empty is the size's committed file under internal/labfootprint/baseline")
	f.Int("systems", 0, "override the size's systems; 0 is the size's")
	f.Duration("warmup", 0, "override the size's warmup; 0 is the size's")
	f.Duration("window", 0, "override the size's window; 0 is the size's")
	f.Int("fleet-reads", 0, "override the size's timed fleet view reads; 0 is the size's")
	return cmd
}

func readBaselineFile(path string) (labfootprint.Baseline, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return labfootprint.Baseline{}, fmt.Errorf("no baseline at %s: generate it with `make perf-baseline` on the host class it gates, or name one with --baseline", path)
	}
	if err != nil {
		return labfootprint.Baseline{}, err
	}
	defer f.Close()
	return labfootprint.ReadBaseline(f)
}

// collect is the runs the gate reads: the series' last samples, or fresh
// labs measured one after another with in.
func (o labPerfOpts) collect(ctx context.Context, in labfootprint.PerfInputs, log io.Writer) ([]labfootprint.ScaleReport, error) {
	if o.samples != "" {
		n := o.last
		if n == 0 {
			n = o.runs
		}
		f, err := os.Open(o.samples)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return labfootprint.ReadSampleReports(f, n)
	}
	_, daemon, err := labEngine(ctx, log)
	if err != nil {
		return nil, err
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var keep io.Writer
	if o.appendPath != "" {
		f, err := labfootprint.OpenSampleLog(o.appendPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		keep = f
	}
	var runs []labfootprint.ScaleReport
	for i := 1; i <= o.runs; i++ {
		fmt.Fprintf(log, "perf run %d of %d: %s at %d systems\n", i, o.runs, in.Scenario, in.Systems)
		cfg := o.scaleConfig(in, binary, daemon.Host)
		cfg.Out, cfg.LabLog = log, log
		rep, err := labfootprint.MeasureScale(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("perf run %d: %w", i, err)
		}
		if keep != nil {
			line, err := labfootprint.ScaleSample{Seq: i, At: time.Now().UTC(), ScaleReport: &rep}.Line()
			if err != nil {
				return nil, err
			}
			if _, err := keep.Write(line); err != nil {
				return nil, err
			}
		}
		runs = append(runs, rep)
	}
	return runs, nil
}
