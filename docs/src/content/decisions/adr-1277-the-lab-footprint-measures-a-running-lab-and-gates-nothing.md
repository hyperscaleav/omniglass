---
title: "The lab footprint measures a running lab per container, and gates nothing"
date: 2026-10-29
status: "Accepted"
summary: "A fifth performance instrument, diagnostic only: make lab-footprint brings up a throwaway lab at a named cadence profile (lab, walkthrough, default), reads each container's CPU and working set (anon plus shmem) from its cgroup through the Engine, rows a minute from the cumulative statistics, growth from relation sizes and the WAL position, and times the 24 h and 30 d series reads with the server and Postgres held to one vCPU, as the median of N windows with the range beside it"
refs: [1277, 1272, 1265]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [test-driven](/contributing/test-driven/), [lab](/guides/lab/)
- **Decision:** (#1277, the half of #1272 that needs Docker; extends
  [ADR-1272](#adr-1272-the-ingest-footprint-is-counted-and-a-stored-rows-heap-bytes-are-a-count)
  and [ADR-0094](#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing).)
  1. **A running lab is measured, never gated.** `make lab-footprint` (`cmd/labfootprint`,
     `internal/labfootprint`) brings up a throwaway lab of its own project, warms it up, and
     measures N back-to-back windows. Each figure is the median, printed with the lowest and the
     highest window, because on a shared host the spread is part of the reading.
  2. **The lab's cadence is a named profile.** `lab` (a learner's: check in every 2 s, probe
     every 10 s), `walkthrough` (a script's throwaway lab: probe on every check-in) and
     `default` (the node's and the simulator's own defaults, read from their constants so the
     profile cannot drift from them). A lab given none runs as before. The flag is hidden: it is
     for the harness and scripts.
  3. **What is read, and from where.** CPU and memory from each container's cgroup v2 counters
     as the Engine's stats report them; the working set is anonymous plus shared memory, never
     RSS or the cgroup total, which count reclaimable file pages. Rows a minute per lane from
     `pg_stat_user_tables.n_tup_ins`, never a `count(*)`, so measuring scans nothing. Each lane
     is summed over its partitions, so a partitioned `metric` reads the same.
  4. **The one-vCPU limit holds only while reads are timed.** Ingest is measured unthrottled;
     then the server and Postgres are updated to one CPU live and the reads run through the API
     the console uses.
  5. **It cleans up after itself, provably.** The lab is stopped with one SIGTERM and its own
     teardown. The harness then fails if any container, network or volume carrying the
     project's label is left.
- **Context:** #1265's sizing needs what a deployment costs per container, which the ingest
  footprint (one batch through the handler) cannot see. #1273 named cadence profiles but
  shipped only `--probe-every`, so the harness had no way to run a lab at the product's cadence.
  The profiles land here, at the lab, rather than as a product setting; #1373 owns where a
  deployment's cadence resolves from.
- **Not chosen:**
  - **A gate or a stored budget.** The figures move with the host's other load by more than the
    changes they exist to show (ADR-0094's reason). The counted instrument stays the gate.
  - **Reading cgroup files with an exec.** The omniglass image is distroless and has no shell.
    The Engine's stats are the same counters, read the same way on every platform Docker runs.
  - **Holding the lab to one vCPU throughout.** That would make the ingest figures a measure of
    the limit rather than of the work.
