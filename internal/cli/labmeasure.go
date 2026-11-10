package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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
	settleWait time.Duration
	jsonPath   string
	// jsonSet is whether --json was given, not left at its default.
	jsonSet bool
	// every, appendPath and samples make a sampling run (#1632).
	every      time.Duration
	appendPath string
	samples    int
	// pageClients, pageLoads and pageSystem are the system page load.
	pageClients int
	pageLoads   int
	pageSystem  string
}

// labMeasureOptsFrom reads the parsed flags. A sampling run defaults the
// retention probe off, since the probe restarts the server; a --backlog
// given on the command line stays, so check refuses it by name.
func labMeasureOptsFrom(cmd *cobra.Command) labMeasureOpts {
	fs := cmd.Flags()
	o := labMeasureOpts{}
	o.systems, _ = fs.GetInt("systems")
	o.scenario, _ = fs.GetString("scenario")
	o.warmup, _ = fs.GetDuration("warmup")
	o.window, _ = fs.GetDuration("window")
	o.fleetReads, _ = fs.GetInt("fleet-reads")
	o.backlog, _ = fs.GetInt64("backlog")
	o.drainWait, _ = fs.GetDuration("drain-timeout")
	o.settleWait, _ = fs.GetDuration("settle-timeout")
	o.jsonPath, _ = fs.GetString("json")
	o.jsonSet = fs.Changed("json")
	o.every, _ = fs.GetDuration("every")
	o.appendPath, _ = fs.GetString("append")
	o.samples, _ = fs.GetInt("samples")
	o.pageClients, _ = fs.GetInt("page-clients")
	o.pageLoads, _ = fs.GetInt("page-loads")
	o.pageSystem, _ = fs.GetString("page-system")
	if o.every > 0 && !fs.Changed("backlog") {
		o.backlog = 0
	}
	return o
}

// check refuses the flags a measurement cannot run on, before any container
// is touched.
func (o labMeasureOpts) check() error {
	switch {
	case o.systems < 0:
		return fmt.Errorf("--systems %d: want a count of systems, or 0 to measure the lab already running", o.systems)
	case o.window <= 0:
		return fmt.Errorf("--window %s: the window must be longer than zero", o.window)
	case o.window > labfootprint.MaxDeliveryWindow:
		return fmt.Errorf("--window over 23h (%s): receipts are pruned at a day, so a longer window would read pruned batches as lost", o.window)
	case o.warmup < 0:
		return fmt.Errorf("--warmup %s: want zero or more", o.warmup)
	case o.fleetReads < 1:
		return fmt.Errorf("--fleet-reads %d: want at least one timed read", o.fleetReads)
	case o.backlog < 0:
		return fmt.Errorf("--backlog %d: want a row count, or 0 to skip the retention probe", o.backlog)
	case o.backlog > 0 && o.drainWait <= 0:
		return errors.New("--drain-timeout: must be longer than zero")
	case o.settleWait <= 0:
		return fmt.Errorf("--settle-timeout %s: the delivery read needs a bound longer than zero", o.settleWait)
	case o.every < 0:
		return fmt.Errorf("--every %s: want 0 for one measurement, or the time between samples", o.every)
	case o.samples < 0:
		return fmt.Errorf("--samples %d: want a count, or 0 to sample until interrupted", o.samples)
	case o.pageClients < 0:
		return fmt.Errorf("--page-clients %d: want a count of concurrent clients, or 0 to skip the system page load", o.pageClients)
	case o.pageClients > 0 && o.pageLoads < 1:
		return fmt.Errorf("--page-loads %d: want at least one timed load a client", o.pageLoads)
	case o.every == 0 && o.appendPath != "":
		return errors.New("--append writes one line a sample: add --every to sample on an interval, or use --json for one measurement")
	case o.every == 0 && o.samples > 0:
		return fmt.Errorf("--samples %d counts a sampling run's samples: add --every to sample on an interval", o.samples)
	case o.every == 0:
		return nil
	case o.systems > 0:
		return errors.New("--every samples the lab already running: drop --systems, bring the lab up with `omniglass lab run --scenario corporate --systems N`, then sample it")
	case o.backlog > 0:
		return fmt.Errorf("--backlog %d: the retention probe restarts the server, which a time series must not do; drop --backlog when sampling with --every", o.backlog)
	case o.every < o.window:
		return fmt.Errorf("--every %s is shorter than --window %s: each sample measures one whole window, so give --every at least the window", o.every, o.window)
	case o.jsonSet && o.jsonPath != "":
		return errors.New("--json writes one measurement: a sampling run writes JSON lines, so use --append with --every")
	}
	return nil
}

// newLabMeasureCmd is `omniglass lab measure` (#1556): the scale lab's
// measurement, one table of what a fleet of this size costs.
func newLabMeasureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "measure",
		Short: "Measure a running scale lab: rows a minute, growth, WAL a sample, CPU and memory, ingest lag, redeliveries, the consumer lane's busy fraction, fleet view and system page times, the retention drain and each kind's delivery loss, once or sampled on an interval",
		Long: "Measures a scale lab (`omniglass lab run --scenario corporate --systems N`) over a stated window and prints " +
			"one table: rows a minute per lane, table and WAL growth a day extrapolated from the window, CPU and memory " +
			"per container, WAL bytes a sample, ingest lag from probe to row (p50, p95, p99), consumer redeliveries, the " +
			"ingest consumer lane's busy fraction and service time a batch, the console's fleet view response " +
			"times, the retention pass's drain rate, and each telemetry kind's delivery loss across the ingest hop (in, out, " +
			"loss, deduplicated and the cause of any loss), with the end-to-end loss a kind. It writes the same figures as JSON so two runs compare.\n\n" +
			"With no --systems it measures the scale lab already running. With --systems it brings up a throwaway scale " +
			"lab of that size, measures it, and removes it, so the one command is the whole run.\n\n" +
			"When no sample probed in the window had landed by its end, the ingest lag cell reads not measured, with the " +
			"reason and the age of the newest landed sample under the table, and every other figure still prints. The " +
			"consumer lane row is read from the server's periodic `ingest lane` log line and needs two tallies, 30 s " +
			"apart, inside the window; a window too short to hold them reads not measured, with the reason under the table.\n\n" +
			"The delivery read waits, up to --settle-timeout, until every node boot that minted a batch in the window has " +
			"landed one minted after it and the telemetry consumer holds nothing pending, then reconciles each batch's " +
			"ledger against what landed. A healthy lab reads loss 0 for every kind; a boot that did not settle prints " +
			"unsettled with its node, and no ratio is read from it.\n\n" +
			"The retention probe plants a backlog of aged property rows and restarts the server to run its pass; " +
			"--backlog 0 skips it and leaves the server alone. A lab started before this build's `lab run` does not " +
			"record commit timestamps, which the ingest lag reads: bring it up again.\n\n" +
			"--page-clients N times the console's system page as an operator feels it: N clients each load the page " +
			"--page-loads times across the window, making every read a cold open of the page makes, timed from the " +
			"first request to the last answer. A load with any read that does not answer 200 counts as an error and " +
			"stays out of the times.\n\n" +
			"--every samples the lab already running on an interval instead of measuring it once. The lab is found and " +
			"warmed up once; then every --every one window is measured and written as one JSON line holding the same " +
			"fields at the same paths as --json, plus the sample's sequence, end time and the server's uptime. Lines go " +
			"to --append (opened for append, so a series resumes into the same file) or to standard output. --samples " +
			"stops after that many; 0 samples until interrupted, finishing the sample in flight. A sample that fails " +
			"writes a line with its error and no figures, and the run goes on. Sampling skips the retention probe, " +
			"which restarts the server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := labMeasureOptsFrom(cmd)
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
				PageClients: o.pageClients, PageLoads: o.pageLoads, PageSystem: o.pageSystem,
				Every: o.every, Samples: o.samples, SettleTimeout: o.settleWait,
				Out: cmd.ErrOrStderr(), LabLog: cmd.ErrOrStderr(),
			}
			if o.every > 0 {
				return sampleScale(ctx, cfg, o.appendPath, cmd.OutOrStdout())
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
	f.Int("systems", 0, "bring up a throwaway scale lab of this many systems, measure it and remove it; 0 measures the one already running")
	f.String("scenario", "corporate", "the scale scenario a --systems lab runs")
	f.Duration("warmup", 2*time.Minute, "how long the lab runs before the window starts (once, before the first sample)")
	f.Duration("window", 5*time.Minute, "the window the rates, growth and usage are read over")
	f.Int("fleet-reads", 20, "how many timed fleet view reads, spread over the window")
	f.Int64("backlog", 150000, "aged property rows the retention probe plants; 0 skips the probe and its server restart (0 when sampling)")
	f.Duration("drain-timeout", 3*time.Minute, "how long the retention probe waits for the pass to clear the backlog")
	f.Duration("settle-timeout", 3*time.Minute, "how long the delivery read waits for every node's batches from the window to land before naming a boot unsettled")
	f.String("json", "lab-measure.json", "also write the figures here as JSON; empty writes none")
	f.Int("page-clients", 0, "how many clients load the console's system page at once across the window; 0 skips the load")
	f.Int("page-loads", 20, "how many timed system page loads each client makes, spread over the window")
	f.String("page-system", "", "the system whose page the clients load, by name; empty loads the fleet view's first")
	f.Duration("every", 0, "sample the running lab on this interval, one JSON line a sample; 0 measures once")
	f.String("append", "", "append each sample's line to this file, created if missing; empty writes to standard output")
	f.Int("samples", 0, "stop after this many samples; 0 samples until interrupted")
	return cmd
}

// sampleScale is a sampling run: SampleScale until it has its samples or
// the first SIGINT or SIGTERM, which lets the sample in flight finish; a
// second signal ends the process as usual.
func sampleScale(ctx context.Context, cfg labfootprint.ScaleConfig, appendPath string, stdout io.Writer) error {
	out := stdout
	if appendPath != "" {
		f, err := labfootprint.OpenSampleLog(appendPath)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	stop, release := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer release()
	go func() {
		<-stop.Done()
		release()
	}()
	if err := labfootprint.SampleScale(stop, cfg, out); err != nil {
		return err
	}
	if appendPath != "" {
		fmt.Fprintf(cfg.Out, "appended to %s\n", appendPath)
	}
	return nil
}
