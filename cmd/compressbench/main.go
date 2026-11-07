// Command compressbench is `make compress-bench` (#1516, #1518, #1522): it
// compresses the same seeded series under every candidate metric format, on
// throwaway Postgres 18 containers, at each product cadence, prints the bytes
// per sample, compression time, read latencies, late-merge cost and span rule
// as markdown, optionally writes them as JSON, and with -probes runs the fit probes for pg_deltax and
// TimescaleDB. With -plain it measures the summary-free block: at each
// cadence beside F2 and metric_trend, a week-block sweep for the week
// minimum, and a week of changing cadence. It is a diagnostic (ADR-0094): it exits non-zero only when a
// candidate does not give back exactly what it was given, or a probe cannot
// run.
package main

import (
	"context"
	"encoding/json"
	"errors"
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
		cadences = flag.String("cadences", "10s,30s,60s,5m,1h,24h", "the cadences to measure")
		series   = flag.String("series", "10s=10,30s=30,60s=60,5m=300,1h=3000,24h=3000", "series count at each cadence")
		days     = flag.Int("days", 30, "days of samples in each dataset")
		runs     = flag.Int("runs", 3, "runs each cell is the median of")
		reps     = flag.Int("reps", 5, "timed reads within a run, after one warmup")
		seed     = flag.Uint64("seed", 1516, "the generator's seed")
		only     = flag.String("only", "", "comma-separated candidate name prefixes to keep (raw,F0,F4)")
		jsonOut  = flag.String("json", "", "also write the report as JSON to this path")
		probes   = flag.Bool("probes", true, "run the pg_deltax and TimescaleDB fit probes")
		bench    = flag.Bool("bench", true, "run the format benchmark")
		plain    = flag.Bool("plain", false, "measure the summary-free block (#1522)")
		sweep    = flag.String("sweep", "1,2,3,4,7,14,24,48,168", "samples a week in the week-block sweep")
		sweepN   = flag.Int("sweep-series", 3000, "series in each week-block sweep dataset")
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
		Plain  *plainOut             `json:"plain,omitempty"`
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
		fmt.Printf("## Span rule\n\nThe smallest span whose block rows mostly cross the compression threshold; raw where no block is smaller than the narrow row.\n\n%s",
			out.Report.RuleTable())
	}
	if *plain {
		var perWeek []int
		for _, k := range strings.Split(*sweep, ",") {
			n, err := strconv.Atoi(k)
			if err != nil || n < 1 {
				return fmt.Errorf("-sweep: %q is not a count", k)
			}
			perWeek = append(perWeek, n)
		}
		p, err := runPlain(ctx, servers, specs, perWeek, *sweepN, *seed, *runs, *reps)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		out.Plain = &p
		benchErr = errors.Join(benchErr, err)
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

// plainOut is the -plain report: the cadence comparison, the week sweep and
// its minimum, and the mixed week at that minimum.
type plainOut struct {
	Cadences   compressbench.Report `json:"cadences"`
	Sweep      compressbench.Report `json:"sweep"`
	WeekMin    int                  `json:"week_min"`
	WeekMinSet bool                 `json:"week_min_set"`
	Mixed      compressbench.Report `json:"mixed"`
}

func runPlain(ctx context.Context, servers *compressbench.Servers, specs []compressbench.Spec, perWeek []int, sweepN int, seed uint64, runs, reps int) (plainOut, error) {
	var p plainOut
	var errs []error
	opt := func(specs []compressbench.Spec, cands func(compressbench.Spec) []compressbench.Candidate) compressbench.Options {
		return compressbench.Options{Specs: specs, Runs: runs, Reps: reps, Log: os.Stderr, BucketWidth: time.Hour, Candidates: cands}
	}
	// From a Monday, so week blocks are whole ISO weeks but the last.
	monday := make([]compressbench.Spec, len(specs))
	for i, s := range specs {
		s.Start = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
		monday[i] = s
	}
	var err error
	p.Cadences, err = compressbench.Bench(ctx, servers, opt(monday, compressbench.PlainCandidates))
	errs = append(errs, err)
	if ctx.Err() != nil {
		return p, ctx.Err()
	}
	fmt.Printf("## Summary-free blocks\n\n%d runs a cell, %d timed reads a run; each figure is the median. Reads are the 30 d hourly bucketed read of one series.\n\n%s\n%s\n",
		runs, reps, compressbench.PlainTable(p.Cadences), p.Cadences.Table())

	var sweep []compressbench.Spec
	for _, k := range perWeek {
		sweep = append(sweep, compressbench.SweepSpec(k, sweepN, seed))
	}
	p.Sweep, err = compressbench.Bench(ctx, servers, opt(sweep, compressbench.SweepCandidates))
	errs = append(errs, err)
	if ctx.Err() != nil {
		return p, ctx.Err()
	}
	rows := compressbench.Sweep(p.Sweep)
	fmt.Printf("## Week-block sweep\n\n%d series, %d ISO weeks, every block a week.\n\n%s\n", sweepN, compressbench.SweepWeeks, compressbench.SweepTable(rows))
	p.WeekMin, p.WeekMinSet = compressbench.WeekMinimum(rows)

	weekMin := p.WeekMin
	if !p.WeekMinSet {
		weekMin = 1 << 30
	}
	mixed := compressbench.MixedSpec(seed)
	p.Mixed, err = compressbench.Bench(ctx, servers, opt([]compressbench.Spec{mixed}, func(s compressbench.Spec) []compressbench.Candidate {
		return []compressbench.Candidate{compressbench.Plain{WeekMin: weekMin}.Candidate(s)}
	}))
	errs = append(errs, err)
	for _, c := range p.Mixed.Cells {
		fmt.Printf("## Mixed cadence\n\nOne ISO week at a week minimum of %d, every series-day held to the span rule and the exact round trip: %s.\n\n%s",
			weekMin, compressbench.Exact(c.Error), compressbench.MixedTable(c))
	}
	return p, errors.Join(errs...)
}
