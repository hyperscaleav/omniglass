// Command labfootprint is `make lab-footprint` (#1277): it brings up a
// throwaway lab at a cadence profile, lets it warm up, measures each
// container's CPU and memory, the database's growth and the series read
// latency over N windows, prints the medians, optionally writes them as JSON,
// and tears the lab down, leaving nothing it created behind.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

func run() error {
	var (
		bin      = flag.String("bin", "bin/omniglass", "the omniglass binary make built")
		scenario = flag.String("scenario", lab.DefaultScenario, "the fleet the lab seeds")
		cadence  = flag.String("cadence", lab.CadenceLab, "the node's cadence profile: lab, walkthrough or default")
		warmup   = flag.Duration("warmup", 2*time.Minute, "how long the lab runs before the first window")
		window   = flag.Duration("window", time.Minute, "one window's length")
		windows  = flag.Int("windows", 5, "how many windows; each figure is their median")
		reads    = flag.Int("reads", 5, "how many times each picked series is read at each range")
		hours    = flag.String("read-hours", "24,720", "the series read ranges, in hours")
		cpus     = flag.Float64("read-cpus", 1, "the CPU the server and Postgres are held to while reads are timed")
		jsonOut  = flag.String("json", "", "also write the report here as JSON")
		labLog   = flag.String("lab-log", "", "write the lab's own output here (default: discarded)")
	)
	flag.Parse()
	if _, err := lab.LookupCadence(*cadence, true); err != nil {
		return err
	}
	if *windows < 1 {
		return fmt.Errorf("-windows %d: at least one window", *windows)
	}
	var readHours []int
	for _, f := range strings.Split(*hours, ",") {
		h, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || h <= 0 {
			return fmt.Errorf("-read-hours %q: %q is not a positive number of hours", *hours, f)
		}
		readHours = append(readHours, h)
	}
	home, _ := os.UserHomeDir()
	daemon, err := lab.ResolveDaemon(lab.Env{Getenv: config.Get, ReadFile: os.ReadFile, Home: home, GOOS: runtime.GOOS})
	if err != nil {
		return err
	}
	var logw io.Writer = io.Discard
	if *labLog != "" {
		f, err := os.Create(*labLog)
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
	rep, err := labfootprint.Measure(ctx, labfootprint.Config{
		Binary: *bin, DockerHost: daemon.Host,
		Project:  fmt.Sprintf("ogfoot-%d-%s", os.Getpid(), hex.EncodeToString(rnd)),
		Scenario: *scenario, Cadence: *cadence,
		Warmup: *warmup, Window: *window, Windows: *windows,
		ReadHours: readHours, ReadsEach: *reads, ReadCPUs: *cpus,
		Out: os.Stderr, LabLog: logw,
	})
	if err != nil {
		return err
	}
	fmt.Print("\n" + rep.Table())
	if *jsonOut != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(*jsonOut, append(b, '\n'), 0o644)
	}
	return nil
}
