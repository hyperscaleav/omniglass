---
title: "The criteria deadline cannot refuse a small run (#956)"
date: 2026-10-21
refs: [956]
---

Re-running the lowering oracle on a loaded machine refused one trivial criterion (`value >= 1000000000000000000000.0`) for its wall-clock deadline: the meter read the clock at a run's first charge, so a goroutine preempted between `Run` and that charge read as an overrun and would have held a rule for no reason. The clock is now read every 256 charges and never before the first 256, so a run that small is bounded by its iteration budget alone and the deadline stays a backstop for long charged work; `TestDeadlineSparesSmallRuns` pins it.
