---
title: "A poll's CPU cost is its thread's kernel clock, cross-checked by the cgroup"
date: 2026-11-13
status: "Accepted"
summary: "Each driver poll runs locked to its OS thread and lands the CPU the kernel charged that thread as `poll-cpu-time`, a per-endpoint mean per report window. The lab cross-checks it against the node process's cgroup CPU slope (#1871), not against a labelled pprof profile as the issue specified: the node serves no profile"
refs: [1870, 1871, 1369, 1623]
---

- **Date:** 2026-11-13 | **Status:** Accepted | **Pages:** [nodes](/architecture/nodes/#self-monitoring-the-node-reports-itself), [collection](/architecture/collection/#collection-telemetry-every-endpoints-account-of-itself)
- **Decision:** (#1870.) A lane worker runs each driver poll, scheduled or run now, with its goroutine locked to its OS thread and reads `CLOCK_THREAD_CPUTIME_ID` before and after. The node lands the mean per run as `poll-cpu-time` (ms) beside `poll-duration`, a `node-probe` built-in key under ADR-1369's report window. The figure is a floor: it leaves out other goroutines (the network poller, a driver's helpers) and the GC. Its cross-check is the per-driver sweep of #1871, which compares the mean `poll-cpu-time` times the run rate against the slope of the node process's cgroup CPU, rather than a CPU profile with `pprof.Do` labels.
- **Consequences:** A platform with no thread clock (Windows, among the release targets) lands no `poll-cpu-time` rather than a false zero, and the lab's built-in tier expects it on every poller because the lab runs on Linux. Each lane worker holds its thread through a run, so the node holds at most one extra parked thread per worker.
- **Context:** The issue's spec cross-checked the clock against a labelled CPU profile. The node serves no pprof endpoint and takes no profile, so that check would add a profiling surface to the node for one comparison. The cgroup's CPU counter is the kernel's whole-process figure, already the basis of #1871's sweep, and bounds the per-run floor from above with no new surface.
- **Not chosen:**
  - **A labelled pprof profile in the lab.** It follows a run into its child goroutines, which the thread clock cannot, but it needs a profile endpoint or flag on the node and a profile pipeline in the lab, for a check the cgroup slope gives at the scale the cost table needs.
  - **Wall time (`poll-duration`).** It is mostly the device's latency and says nothing about what the node spent.
