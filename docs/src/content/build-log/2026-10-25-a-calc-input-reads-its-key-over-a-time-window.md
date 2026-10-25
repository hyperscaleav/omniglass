---
title: "a calc input reads its key over a time window (#1142)"
date: 2026-10-25
refs: [1142, 1065]
---

The first cut of #1065, the rolling figures a calc rule could not compute. A component rule's input
gains `window_seconds` (1 to 3600, on `calc_rule_input`): its alias binds as the key's samples in the
window, oldest first, each a record with `value`, `at` (unix seconds; an expression environment holds
no `time.Time`) and `instance` (`internal/rules/calc.go`). The list functions fold a list of records
with a `value` field by that field, one rule in `internal/expression/lists.go` (`foldElem`, `asList`),
so `avg(rtt)` over a 300s window is a 5-minute mean with no new function; the predicate forms and `len`
read the records as they are.

The read walks the key's in-window samples on the component through the series index and keeps the
newest 1000 (`readCalcWindow`, `internal/storage/calc_eval.go`). A window moves with no new sample, so each
computation records when its oldest sample leaves the window (`calc_rule_state.window_due_at`), and the
calc tick marks the owners whose due time has passed since, one statement a mark so it joins no lock
cycle, and passes over a rule a write holds (`markCalcWindowsDue`; the review found its foreign key
check could otherwise deadlock with a rule edit's reset). A window outside its bounds, or on a system rule, is refused at write on
the input's field.

Deferred to #1143: a sample's outcome and `rate` (they need #1063's observation model), system-rule
windows, the console's window field and completion, a cadence-derived cap, naming a truncated window,
an arrival-time window, and the backtest.
