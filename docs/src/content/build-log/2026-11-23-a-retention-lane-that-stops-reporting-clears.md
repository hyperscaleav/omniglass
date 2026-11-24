---
title: "A retention lane that stops reporting clears its warning (#1673)"
date: 2026-11-23
refs: [1673, 1629]
---

A lane that read `retention-behind` = 1 and then dropped out of the pass's report (a type override
removed, a window out of range) kept a pending fire streak, so the sweep opened a
`retention-behind` alarm with no sample to ever clear it. The retention loop now remembers the lanes
the last pass flagged behind and writes 0 for any the next pass does not report. Tests:
`TestRetentionSamplesClearALaneThatStoppedReporting` (including the compactions when the metric
window goes out of range) and `TestARetentionLaneThatStopsReportingOpensNoAlarm`.
