---
title: "A scale lab measured end to end gates against a committed baseline"
date: 2026-11-10
status: "Accepted"
summary: "Amends ADR-0094: `make perf-check SIZE=ci|laptop` (`omniglass lab perf check`) measures N throwaway scale labs of a named size (corporate at 300 for ci, 1000 for laptop; 2 min warm-up, a 5 min window that is a whole multiple of every poll interval) and fails when a figure regressed past the size's committed, generated baseline. Exact figures (closed-loop gaps, ingest lag not measured, redeliveries, delivery loss per hop and kind) must be 0; host-independent counts gate on any host; host-dependent times and usage gate only on the baseline's host fingerprint and read reported elsewhere. A gated figure is the median of the runs and fails only past its tolerance and past the baseline's and the runs' combined spread; runs spread past the tolerance read noisy. CI runs it opt in on the run:perf label, never as a required check; micro-benchmarks stay diagnostic"
refs: [1640, 1639, 1641, 1632, 1695, 1719, 1662, 1749, 1750, 1751]
---

- **Date:** 2026-11-10 | **Status:** Accepted | **Pages:** [test-driven](/contributing/test-driven/), [lab](/guides/lab/#checking-against-the-baseline), [labels](/contributing/labels/)
- **Decision:** (#1640; amends
  [ADR-0094](#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing), and
  extends [ADR-1277](#adr-1277-the-lab-footprint-measures-a-running-lab-per-container-and-gates-nothing).)
  A whole scale lab, measured end to end, becomes a sixth performance instrument and the fourth that
  gates. `omniglass lab perf check` (`internal/cli/labperf.go`) brings up `--runs` throwaway labs of a
  named size one after another, measures each with `lab measure`'s own `MeasureScale`, and compares them
  with the size's baseline under `internal/labfootprint/baseline/` through one pure function,
  `ComparePerf` (`internal/labfootprint/perfgate.go`). Go micro-benchmarks (`make bench`, `bench-sim`,
  `compress-bench`) stay diagnostic under ADR-0094, unchanged.
  1. **ADR-0094's three refusals, answered in turn.**
     - *No CI job and no merge gate on a duration.* A CI job exists (`.github/workflows/perf.yml`), but it
       runs only on the `run:perf` label or a dispatch, skips fork PRs, and is never a required check.
       Whether it ever gates a merge is a later decision, once a record of its noise exists.
     - *No stored baseline: a number from a different machine on a different day.* A stored baseline
       exists, but it is generated (`lab perf baseline`, never by hand), per host class, and carries its
       host's fingerprint (CPU model, logical cores, memory). A time or a usage figure gates only on the
       host that fingerprint names; on any other it reads `reported` and gates nothing. The counts it
       holds do not depend on the machine, and gate everywhere.
     - *No timing assertion anywhere.* Still none in a test or a benchmark. The times the gate compares
       are medians of whole runs against the spread of those runs and of the baseline's, never one run
       against a threshold, and only on the host the baseline was measured on.
  2. **Three classes, one table each, extended by adding a row.** `exactFamilies` and `gatedFamilies`
     in `perfgate.go` are the tables; the docs render them (`docs/src/generated/perfgate.json`, written
     by `make gen`). A new figure (#1695's) is one row and its reader.
     - **Exact:** every closed-loop gap (#1639), `ingest_lag_not_measured`, `redeliveries`, each telemetry
       kind's `delivery_loss` per hop (#1641), and `delivery_unsettled_boots`. Each must be 0, whatever the
       baseline holds; a baseline whose runs read otherwise is refused.
     - **Host-independent:** metric and property rows a minute per system (2 percent, either way, since
       fewer rows is lost work), database bytes a day per system (5 percent), WAL bytes per sample
       (10 percent, provisional, below), samples per batch (5 percent, either way).
     - **Host-dependent:** endpoint latency p95 per protocol (20 percent), ingest lag p95 (20 percent),
       server millicores and working set (15 percent), fleet view p95 (20 percent). Each fails only on its
       worse side.
  3. **The noise rule.** Each gated figure is the median of the runs, with its spread (highest less
     lowest) beside it. It **fails** only when the median is past the baseline's by more than the
     tolerance *and* by more than the baseline's spread plus the runs' spread. When the runs' own spread
     exceeds the tolerance it reads **noisy**, never pass: those runs could not have seen a regression of
     that size, and the table prints it first. Noisy does not fail. A figure better than the baseline by
     more than its tolerance reads **improved**, which never fails and asks for a refreshed baseline.
     The default is three runs. With n = 3 a rank test such as Mann-Whitney cannot reach significance at
     any useful level (its smallest two-sided p for 3 against 3 is 0.1), so the spread rule stands in for
     it: it is the strictest statement three runs can support, and it errs toward not failing.
  4. **A run is clean before any figure is read.** The verdict is **invalid**, and the check exits
     nonzero, when the runs measured another size, window or warm-up than the baseline's; when this
     build's scenario places a different device set (the baseline records a digest of the planned fleet,
     so a catalog change says to regenerate rather than reading as a regression); when a node's endpoint
     report reads restarted; or when a device carried an active impairment. Validity names the run it
     failed in.
  5. **The fingerprint is read only where it describes the lab's host.** The runner reads its own CPU,
     cores and memory, so it records them only when the Docker daemon is local (`lab.IsLocal`); a run
     against a remote daemon records no host, and its host-dependent figures read reported. Two hosts
     match on CPU model, cores and memory; the Go version and the image build are recorded for the
     reader and do not decide a match.
  6. **The sizes, and the window.** `ci` is corporate at 300 systems, which a standard GitHub runner
     holds; `laptop` is corporate at 1000. 3000 belongs to #1695. Both warm up for 2 minutes and measure a
     5 minute window, a whole multiple of every interval the scenario's drivers poll at and of the lab
     cadence (2 s, 5 s, 10 s, 30 s, 1 min, 5 min), so every run counts the same whole rounds. A run sets
     no backlog (the retention probe restarts the server) and no system page clients. The issue's first
     guesses (50 and 500) were raised on the architect's ruling; refusing a run measured at another size
     carries #1719's wording, `the run measured dev at 0 systems; the baseline is corporate at 300`.
  7. **WAL per sample's 10 percent is provisional.** It moves with checkpoint timing, which a 5 minute
     window does not hold still. The value is set wider than the counts' until the committed baselines'
     spreads say what it should be; baseline generation refuses a figure whose spread passes its
     tolerance, so a value too tight shows up as a refused baseline, not a muted gate.
  8. **The baselines are generated on the host class they gate, and not on a shared host.** The `ci`
     baseline comes from the `perf` workflow's `workflow_dispatch` with `update` set, which uploads
     `corporate-300.ci.json` as an artifact; the `laptop` baseline from `make perf-baseline SIZE=laptop`
     on the architect's laptop. Committing either is the author's act, and a PR that changes a baseline
     file says why in its body (the ship-slice checklist holds it). The slice that built the gate ran on
     a shared build host whose fingerprint gates neither class, so it ships the generator and not the
     files; committing both, with three laptop passes and a labeled CI run, is #1751.
- **Context:** the architect's ask: measuring latency, queue depth and parallelism is how a feature
  push does not "nerf performance" unnoticed. `lab measure` already produced the whole report, but
  nothing compared one run with an earlier one. #1632's `--append` series gives the gate a second input:
  `--samples FILE` reads a sampling run's last N lines in place of fresh labs, with the same rules.
- **Not chosen:**
  - **A gate on one run.** One run's spread is zero, so a single run against a single-run baseline
    cannot tell a regression from the host's noise; the median and the spread are what make the gate
    believed rather than muted.
  - **A tolerance file.** The tolerances are Go next to the comparison, so changing one is a reviewed
    code change.
  - **Seeding the simulator's behaviors from a stable key.** Behaviors seed from each device's id, a
    database UUID, so two builds of a fleet place the same devices (the plan digest locks that) but not
    the same behavior seeds. The counted figures read the poll schedule, not the behaviors, so the gate
    holds without it; a stable seed key is #1749.
  - **Counting a moved impairment.** The runs are refused on an active impairment; a change counter that
    would show one set and cleared inside the window waits on #1662, and the gate reading it is #1750.
