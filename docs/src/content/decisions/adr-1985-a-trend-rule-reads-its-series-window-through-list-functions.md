---
title: "A trend rule reads its series window through list functions"
date: 2026-11-16
status: Accepted
summary: "An event rule on a metric may name a sample_window (up to a week); its criteria then bind the series' points over it as window, oldest first and averaged into at most 360 buckets, and the generic list functions slope, span and growth fold it; age (the series' run, a process key's from its uptime) gates a fill or a restart's refill. The store reads both, so the evaluator stays pure. Preview refuses them, and the reconcile scan rejudges their series after the drifted ones, capped. Settles the trend half of ADR-0050's design window."
refs: [1985, 1623, 957, 1142]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/#a-trend-rule-reads-a-window-not-a-sample), [expressions](/architecture/expressions/)
- **Decision:** (#1985, under the self-alarming epic #1623.)
  1. **The window is a rule field, the trend is an expression.** An `event_rule` on a metric may set
     `sample_window`, an interval up to a week. Its criteria (fire and clear alike) then bind
     **`window`**: the series' points over that window, oldest first, each a record of `value` and
     `at` (unix seconds). A rule on a property has no window, a `sample_window` the criteria never read
     is refused, and criteria that read `window` with no `sample_window` are refused.
  2. **The trend is three generic list functions.** `slope` (the least-squares rise per second),
     `span` (the seconds the points cover) and `growth` (the fitted rise across the span as a share of
     the points' mean) join the engine's list functions, admitted at every site that binds a list of
     timestamped points: the criteria's `window` and a calc rule's windowed input (#1142). A list of
     fewer than two points, or with no time between them, has no trend: 0.
  3. **The store reads the window; the evaluator stays pure.** The ingest, the sustain sweep, a rule
     write's prime and the reconcile scan read the window through the series' index for a rule whose
     compiled criteria read it, and hand it to the judge with the sample. A long window is averaged
     into at most 360 even buckets, so a run's cost is bounded by the site's budget, not by the key's
     cadence. Each bucket is the mean of its newest 4 samples, probed per series through the
     `(series_id, ts)` index, so a read touches at most 1,440 rows a series: the bound is in the probe,
     not a limit applied after the rows are grouped.
  4. **Not every rise is a trend: the criteria bind `age`.** A series that fills from empty (a fresh
     database) or refills after a restart (a process's heap) rises steadily with nothing wrong, so a
     metric's criteria also bind `age`, how long the series has run in seconds. On a process's own key
     it runs from the process's start: every process reports `process-uptime`, and the age is its newest
     plus the time since, since the series itself spans restarts. On any other key it runs from the
     series' oldest kept sample, so it is capped by the lane's retention.
  5. **A trend moves without a sample.** The reconcile scan rejudges the touched series of a rule whose
     criteria read `window` or `age` rather than trusting the state it last wrote, after the series whose
     state has drifted and at most 200 a pass, so a fleet of trend rules never starves the repair of a
     missed sample. A preview, which judges each sample alone, refuses criteria that read `window` or
     `age` (a 422) rather than answer that a trend never fires.
  6. **The shipped thresholds.** `process-heap-growing` and `process-goroutines-growing` read 6 hours,
     fire on `age >= 36000 && span(window) >= 18000 && growth(window) > 0.25` held 1 hour (ten hours of
     uptime: the window clear of a four-hour refill), and clear at `growth(window) <= 0.1`;
     `db-size-growing` reads 24 hours, gates on `age >= 604800` (a week, by when a day's fill is a
     seventh), needs 20 hours of span, holds 6 hours. All three raise `average`, the seed's middle
     level.
- **Context:** the self-alarming epic needs alarms on a slow leak (a heap or goroutine count, the
  database's size) that no level threshold catches without either firing on a healthy large install
  or missing a leak on a small one. A trend is relative to the series' own level. ADR-0050 left a
  `window: { reduce, over }` in design, which reduces a window to one value before the criteria and so
  cannot express a slope without a reducer per shape.
- **Consequences:**
  - A trend rule is unit-testable on a fixed series: the shipped rules' test judges a steady rise
    (fires), a flat noisy series (clears), a process's warm-up (holds) and a database at its retention
    window dropping a day's partition at any point (clears), and walks each through the growth it must
    not alarm on: a process's half day after a restart, a fresh database's first three weeks.
  - A trend fires on a sustained relative rise, not a step: a load step of over a quarter of the level
    reads as a slope for a while, which the sustain absorbs only partly.
  - `db-size-growing` cannot judge where the metric lane keeps under a week (`metric_days` < 7): its
    age never reaches the gate. A shorter gate there would read a day's fill as bloat.
  - A process that restarts more often than every ten hours is never judged for a leak; a crash loop
    is its own alarm.
  - The console's rule drawer does not yet edit `sample_window`, and a windowed rule has no preview.
  - `slope`, `growth` and `span` are now reserved, so a calc rule input may not take one as its alias; a
    one-time backfill disables a rule that did, as the first reserving backfill did, until it is renamed.
- **Not chosen:**
  - **Gating on the window's shape alone** (a span the series has filled): a restart's series spans
    the restart, so its window is full and its refill reads as growth; only the process knows it
    restarted.
  - **An age from the database's creation** (`pg_stat_database` or the first migration): it names one
    key's fill, not any series', and the series' own oldest sample says the same for every key.
  - **A trend operator or a `trend: { over, rise }` rule field:** one shape baked in, and a second
    trend (a fall, a rate, a share above a line) would need another field. The list functions compose.
  - **A function that reads history itself** (`history(key, 6h)` inside the criteria): the evaluator
    would do I/O, lose its purity and its budget's meaning, and each run would cost a query.
  - **`fire_window`** binding only fire: a clear needs the same series to say the trend has stopped,
    and the field names the sample's history, head noun last.
  - **A raw, unbucketed window:** a key written every second would bind 21,600 points over six hours
    and blow the criteria's budget; the fit over bucket means is the same line within noise.
