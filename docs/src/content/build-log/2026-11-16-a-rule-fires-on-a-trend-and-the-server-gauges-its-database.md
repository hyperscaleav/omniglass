---
title: "A rule fires on a trend, and the server gauges its database (#1985)"
date: 2026-11-16
refs: [1985, 1623]
---

An event rule on a metric can now judge where its series is heading. A rule's `sample_window` (up to a
week) binds the series' points over it to its criteria as `window`, read by the store through the
series' index and averaged into at most 360 buckets, and the new list functions `slope`, `span` and
`growth` fold it; calc rules' windowed inputs admit them too. The ingest, the sweep, a rule write's prime
and the reconcile scan all read the window, the reconcile rejudges a windowed rule's series since a trend
moves with no sample (after the drifted series, at most 200 a pass), and a preview refuses windowed
criteria. Each bucket is the mean of its newest 4 samples, probed per series, so a window reads at most
1,440 rows a series. Criteria also bind `age`, how long the series has run: from the process's start on
a process's own key (every process now reports `process-uptime`), else from the oldest kept sample. The server now gauges `db-size`
(`pg_database_size`) every report window on its platform component, shown on the platform health page,
replacing the retention pass's hourly `database-bytes`. Three official rules ship at `average`:
`process-heap-growing` and `process-goroutines-growing` (a quarter's fitted rise over 6 hours, held an
hour, after a day of uptime) and `db-size-growing` (a quarter over a day, held 6 hours, once its series
is a week old), which ships disabled: a fleet onboarded on days 4 to 6 grows the database past the
quarter after day 7, which `db-size` alone cannot tell from bloat, so an operator authors it when their
fleet has settled. A seeded rule can now say `enabled: false`. `TestTheShippedTrendRulesFireOnASteadyRise`
judges each shipped rule on a fixed rising series (fires), a flat noisy one (clears), a warm-up (holds) and
the retention sawtooth (clears), then walks a process's restart refilling over 4 to 18 hours and a fresh
database's first three weeks, its fleet there from day one or onboarded on days 4 to 6: a rule ships on
only if no walk fires it, off only if one does; `TestWindowedRuleFiresOnATrend` proves the same against Postgres through
the prime, the ingest and the reconcile. The decision is ADR-1985. A week of corporate soak opening none
of the three is pending.
