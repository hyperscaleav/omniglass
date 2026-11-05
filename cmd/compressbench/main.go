// Command compressbench is `make compress-bench` (#1516): it compresses the
// same seeded series under every candidate metric format, on throwaway
// Postgres 18 containers, at each product cadence, prints the bytes per
// sample, compression time and read latencies as markdown, optionally writes
// them as JSON, and with -probes runs the fit probes for pg_deltax and
// TimescaleDB. It is a diagnostic (ADR-0094): it exits non-zero only when a
// candidate does not give back exactly what it was given, or a probe cannot
// run.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hyperscaleav/omniglass/internal/compressbench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "compress bench:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		cadences = flag.String("cadences", "2s,30s,60s,24h", "the cadences to measure")
		series   = flag.String("series", "2s=6,30s=30,60s=60,24h=3000", "series count at each cadence")
		days     = flag.Int("days", 30, "days of samples in each dataset")
		runs     = flag.Int("runs", 3, "runs each cell is the median of")
		reps     = flag.Int("reps", 5, "timed reads within a run, after one warmup")
		seed     = flag.Uint64("seed", 1516, "the generator's seed")
		only     = flag.String("only", "", "comma-separated candidate name prefixes to keep (raw,F0,F4)")
		jsonOut  = flag.String("json", "", "also write the report as JSON to this path")
		probes   = flag.Bool("probes", true, "run the pg_deltax and TimescaleDB fit probes")
		bench    = flag.Bool("bench", true, "run the format benchmark")
	)
	flag.Parse()

	counts := map[string]int{}
	for _, kv := range strings.Split(*series, ",") {
		k, v, ok := strings.Cut(kv, "=")
		n, err := strconv.Atoi(v)
		if !ok || err != nil || n < 1 {
			return fmt.Errorf("-series: %q is not cadence=count", kv)
		}
		counts[k] = n
	}
	var specs []compressbench.Spec
	for _, c := range strings.Split(*cadences, ",") {
		d, err := time.ParseDuration(c)
		if err != nil {
			return fmt.Errorf("-cadences: %w", err)
		}
		n, ok := counts[compressbench.CadenceLabel(d)]
		if !ok {
			n, ok = counts[c]
		}
		if !ok {
			return fmt.Errorf("-series has no count for %s", c)
		}
		specs = append(specs, compressbench.Spec{Cadence: d, Series: n, Span: time.Duration(*days) * 24 * time.Hour, Seed: *seed})
	}
	var keep []string
	if *only != "" {
		keep = strings.Split(*only, ",")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	servers := &compressbench.Servers{}
	defer servers.Stop(context.WithoutCancel(ctx))

	var out struct {
		Report compressbench.Report  `json:"report"`
		Probes []compressbench.Probe `json:"probes,omitempty"`
	}
	var benchErr error
	if *bench {
		out.Report, benchErr = compressbench.Bench(ctx, servers, compressbench.Options{
			Specs: specs, Runs: *runs, Reps: *reps, Only: keep, Log: os.Stderr,
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Printf("## Formats\n\n%d runs a cell, %d timed reads a run; each figure is the median, its range in parentheses.\n\n%s\n",
			*runs, *reps, out.Report.Table())
	}
	if *probes {
		ps, err := compressbench.RunProbes(ctx, servers, os.Stderr)
		if err != nil {
			return err
		}
		out.Probes = ps
		fmt.Printf("## Fit probes\n\n%s", compressbench.ProbeTable(ps))
	}
	if *jsonOut != "" {
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	return benchErr
}
