---
title: "A component's Vitals carry each metric's unit (#1550)"
date: 2026-11-15
refs: [1550]
---

The effective-metrics read (components, systems and locations alike) now carries the metric type's `unit`,
omitted when the type names none, and a component's Vitals card shows it after the value: Uptime reads
`306 s` and Poll Duration `0.15 ms`, never a bare `0.1527895`. A float keeps at most two decimals (a small
one keeps two significant digits rather than rounding to zero), the exact value stays on the row's title and
in the API, and the rule is one pure helper, `vitalValueText`. The read is the one unit source for the
card; the rules page keeps its own metric-type lookup because a rule names a metric but has no
effective-metrics read. `TestEffectiveMetricsCarryTheirUnit` failed before the change (no `unit` on any
line) and passes after, as do the `vitalRows`, `vitalValueText` and Vitals render tests.
