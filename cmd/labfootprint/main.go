// Command labfootprint is `make lab-footprint` (#1277): it brings up a
// throwaway lab (the corporate scenario at 20 systems unless told otherwise,
// #1719) at a cadence profile, lets it warm up, measures each
// container's CPU and memory, the database's growth and the series read
// latency over N windows, prints the medians, optionally writes them as JSON,
// and tears the lab down, leaving nothing it created behind.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lab footprint:", err)
		os.Exit(1)
	}
}

// options is a run's flags: the measure's config (the lab's project, daemon
// and writers aside) and where its outputs go.
type options struct {
	cfg             labfootprint.Config
	jsonOut, labLog string
}

// parseArgs reads the command's flags. With none it measures the ladder's
// smoke rung, corporate at 20 systems (#1719).
func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("labfootprint", flag.ContinueOnError)
	var (
		bin      = fs.String("bin", "bin/omniglass", "the omniglass binary make built")
		scenario = fs.String("scenario", lab.MeasureScenario, "the fleet the lab seeds: a scale scenario, or a dev one with -systems 0")
		systems  = fs.Int("systems", lab.SmokeSystems, "the size a scale scenario is resolved at (0 for a dev scenario)")
		cadence  = fs.String("cadence", lab.CadenceLab, "the node's cadence profile ("+lab.CadenceNames()+")")
		warmup   = fs.Duration("warmup", 2*time.Minute, "how long the lab runs before the first window")
		window   = fs.Duration("window", time.Minute, "one window's length")
		windows  = fs.Int("windows", 5, "how many windows; each figure is their median")
		reads    = fs.Int("reads", 5, "how many times each picked series is read at each range")
		hours    = fs.String("read-hours", "24,720", "the series read ranges, in hours")
		cpus     = fs.Float64("read-cpus", 1, "the CPU the server and Postgres are held to while reads are timed")
		jsonOut  = fs.String("json", "", "also write the report here as JSON")
		labLog   = fs.String("lab-log", "", "write the lab's own output here (default: discarded)")
	)
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if _, err := lab.LookupCadence(*cadence, true); err != nil {
		return options{}, err
	}
	if *systems < 0 {
		return options{}, fmt.Errorf("-systems %d: a size, or 0 for a dev scenario", *systems)
	}
	if *windows < 1 {
		return options{}, fmt.Errorf("-windows %d: at least one window", *windows)
	}
	var readHours []int
	for _, f := range strings.Split(*hours, ",") {
		h, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || h <= 0 {
			return options{}, fmt.Errorf("-read-hours %q: %q is not a positive number of hours", *hours, f)
		}
		readHours = append(readHours, h)
	}
	return options{
		cfg: labfootprint.Config{
			Binary: *bin, Scenario: *scenario, Systems: *systems, Cadence: *cadence,
			Warmup: *warmup, Window: *window, Windows: *windows,
			ReadHours: readHours, ReadsEach: *reads, ReadCPUs: *cpus,
		},
		jsonOut: *jsonOut, labLog: *labLog,
	}, nil
}

func run() error {
	o, err := parseArgs(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	daemon, err := lab.ResolveDaemon(lab.Env{Getenv: config.Get, ReadFile: os.ReadFile, Home: home, GOOS: runtime.GOOS})
	if err != nil {
		return err
	}
	var logw io.Writer = io.Discard
	if o.labLog != "" {
		f, err := os.Create(o.labLog)
		if err != nil {
			return err
		}
		defer f.Close()
		logw = f
	}
	rnd := make([]byte, 3)
	_, _ = rand.Read(rnd)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := o.cfg
	cfg.DockerHost = daemon.Host
	cfg.Project = fmt.Sprintf("ogfoot-%d-%s", os.Getpid(), hex.EncodeToString(rnd))
	cfg.Out, cfg.LabLog = os.Stderr, logw
	rep, err := labfootprint.Measure(ctx, cfg)
	if err != nil {
		return err
	}
	fmt.Print("\n" + rep.Table())
	if o.jsonOut != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(o.jsonOut, append(b, '\n'), 0o644)
	}
	return nil
}
