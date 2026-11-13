---
title: "The perf gate reads the working set across the window and the fleet view 100 times (#1751)"
date: 2026-11-11
refs: [1751, 1640]
---

The second `ci` baseline dispatch refused two host-dependent figures as noisy: the server's working set
spread 23 percent against a 15 percent tolerance, and the fleet view p95 spread 37 percent against 20.
An earlier dispatch had read both at 14 percent, so the tolerances were not the problem; the
measurements were.

The working set was one read of each container's cgroup at the window's end, so a run read the Go heap
wherever its collection cycle had reached: just after a collection, or at the peak before one. A scale
window now reads every container's usage about 30 times across it (a read every 10 s of the 5 minute
window, besides its two ends) and reports the median, so one read at a peak does not set the figure.
CPU is still the window's delta. The median is a small pure function (`MedianWorkingSetMiB`) applied by
`MedianWorkingSets`, and the reads are taken by a sampler the window stops when it ends; `lab scale`,
`lab perf` and a `lab measure --every` sampling run all measure through the same window, so they read
the working set the same way. The lab footprint's idle and loaded reads are unchanged.

The fleet view p95 was the nearest-rank p95 of 20 timed reads, which is the second slowest, so a single
slow read moved it. Both sizes now time 100 reads (`lab.FleetViewReads`), which puts five above the p95,
and `lab measure --fleet-reads` defaults to the same. A test holds every size to at least five reads
above its p95.

Both change what a run measures, so a baseline taken before this build does not compare with one taken
after: the `laptop` baseline is generated from a build with this change.
