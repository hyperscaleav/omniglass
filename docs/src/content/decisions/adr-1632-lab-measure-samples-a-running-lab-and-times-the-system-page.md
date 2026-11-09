---
title: "Lab measure samples a running lab into a JSONL series and times the system page as the page reads it"
date: 2026-11-09
status: Accepted
summary: "Accepted: lab measure gains a sampling mode (--every, --samples, --append) that measures one window per interval against a running lab and writes one ScaleSample line each, at the one-shot report's JSON paths, and a system page load (--page-clients) that issues every read a cold open of the console's system page makes. The read list is whatever the page makes today, held equal to it by a generated fixture and a vitest, which drops the alarms read and adds the catalog reads the issue's line references predated."
refs: [1632, 1623, 1625, 1626]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [the lab guide](/guides/lab/#sampling-a-scale-lab-over-time), [CLI reference](/reference/cli/#omniglass-lab-measure)
- **Decision:**
  1. **One window a sample, the lab found once.** `--every` finds the lab, signs in and warms up
     once, then measures one `--window` per interval with the same code the one-shot run uses
     (`measureWindow`), so a sample and a `--json` report cannot drift apart. A `ScaleSample` wraps
     the `ScaleReport` with `seq`, `at` and `uptime_seconds`; a failed sample writes its `error`
     with no figures and the run goes on. The first SIGINT or SIGTERM lets the sample in flight
     finish.
  2. **Sampling never restarts the server.** The retention probe restarts it, so a sampling run
     skips it: `--backlog` defaults to 0 under `--every`, and a `--backlog` given, `--systems`, an
     `--every` below `--window`, `--json`, and `--append` or `--samples` without `--every` are
     refused with the reason.
  3. **The absolutes are a sibling query.** Database bytes, the metric lane's partition count and
     oldest day, the rollup tables' bytes and the dead tuples of `property`, `log_line` and
     `node_log` are levels, not window deltas, so they are their own `AbsolutesQuery`, not columns of
     `DBCountersQuery`. Every sum is cast to `bigint` so the driver reads an integer, not a numeric.
  4. **The page load is the page's reads, guarded by generation.** `SystemPageReads` in
     `internal/labfootprint` is the list; `make gen` writes it to a TypeScript fixture, and
     `SystemZoom.test.tsx` mounts the page cold with a fetch spy and asserts the reads equal the
     fixture, while a Go test asserts the fixture equals the list. Neither side changes without a
     test failing. The chart reads come from the metrics answer, as the page derives them.
  5. **The token re-mints once on a 401.** A sampling run outlives a session token; the harness
     signs in again and retries the request once.
- **Divergences and follow-ups:**
  - The issue's route set cited `SystemZoom.tsx` line numbers from before the page moved its alarms
    read to the activity tab. The list follows the page as it is: `/systems/{name}/alarms` is out
    (activity tab only, with events and logs), and `/auth/me`, `/locations`, `/location-types` and
    `/severity-levels`, which a cold open makes for the shell and the labels, are in.
  - `uptime_seconds` is the server container's uptime at the window's close, not the sampler's: a
    falling value then shows a restart in the series, which a sampler clock cannot.
  - The report also carries `system_page_error_ratio` (0 when every load answered) beside the
    `system_page_errors` count, so the page's health reads as a loss ratio like every other.
  - Node schedule health and command latency stay out of the line until #1625 and #1626 land them.
- **Context:** goal #1623 needs to know whether a fleet's cost holds over hours (growth, retention,
  vacuum) and what an operator waits for when a system opens under load. One window answers neither:
  the first is a trend, and the fleet view read times one query, not the page.
- **Rejected:** a hand-kept read list checked by review (drifted from the page before this issue
  shipped, as the alarms read shows); restarting the lab for each sample (destroys the series being
  measured); appending the absolutes to the counters query (they are not deltas, and the window
  arithmetic would have to skip them).
