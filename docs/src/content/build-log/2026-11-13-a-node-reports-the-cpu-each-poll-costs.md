---
title: "A node reports the CPU each poll costs (#1870)"
date: 2026-11-13
refs: [1870, 1871, 1623]
---

Every driver poll now lands `poll-cpu-time` beside `poll-duration`: the mean CPU time the kernel
charged each run, per endpoint per report window, in milliseconds. A lane worker locks its goroutine
to its OS thread for the run and reads the thread's CPU clock before and after, so the figure is the
run's own on-CPU time and none of its wait on the device. It is a floor (other goroutines and the GC
are charged to no poll), and the per-driver cost table of #1871 reads it against the process's
cgroup CPU. The lab's built-in tier now expects it on every poller (ADR-1870).
