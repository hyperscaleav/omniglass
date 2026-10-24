---
title: "Settlement reads one clock, and a zero window is a statement of intent"
date: 2026-08-12
status: "Accepted"
summary: "Settlement's two timestamps come from **one clock, the database's**: a sample's `ts` is `default now()`, so the `now` a settle-check judges against is read with `select now()` inside the same transaction rather than from `time.Now()` in the server process. Two clocks on one comparison made the verdict a function of skew, and at `settle_window_seconds: 0` there is no margin to absorb it, so a command that genuinely failed could be reported `pending` and never settle on any deployment whose database is on another host. Separately, a **zero window is terminal by construction**, checked before any arithmetic: it is the documented way to say \"settle immediately\", a claim about intent rather than elapsed time, so no timestamp may make it pending. Stamping samples from Go was refused as the larger ripple (every telemetry `ts` defaults to `now()` and other readers rely on database ordering), and a tolerance was refused outright as the move that stops a test failing without stopping the behavior depending on skew. `Settle` stays pure and still takes `now`; what changed is who supplies it, at the cost of one round trip on each of the two settle paths. **Amended (#718):** \"a check in a later transaction reads a strictly later timestamp\" over-claims, since READ COMMITTED admits a concurrent issue committing after a settle-check began, so the delta can be negative; the verdict is unaffected (a negative delta is `pending`, and the zero case reads no timestamp), and the claim rather than the behaviour is what was corrected. **Extended (#719):** the same principle reaches the six history reads by a different mechanism and no round trip, since a read needs a BOUND rather than a value: the window travels as a duration and the query filters `ts >= now() - make_interval(...)`, so the instant is never named in Go"
---

- **Date:** 2026-08-12 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/)
- **Context:** `Settle` compares a sample's `ts` against a `now` its caller supplies. Every `ts` on
  the telemetry tables is `timestamp with time zone default now()`, so that end of the comparison is
  written by **Postgres**, while `now` was `time.Now()` in the **server** process. Two clocks, one
  comparison. At `settle_window_seconds: 0` the test reduces to `now.Sub(intended.TS) < 0`, true
  exactly when the sample is stamped ahead of the host, so the verdict for the setting that most
  wants to be decisive was decided by skew. `TestCommandIssueAPI` failed intermittently on precisely
  that branch, and the operator-facing version is worse than a flake: a deployment whose database sits
  on another host gets the same coin flip on every zero-window command, and a command that genuinely
  failed can be reported `pending` and never settle.
- **Decision (the clock):** the comparison takes `now` from the **database**, read with `select now()`
  inside the caller's transaction (`dbNow`), at both settle sites. `now()` is
  `transaction_timestamp`, which is the point rather than an incidental detail: a settle-check running
  in the transaction that just opened the intended value reads exactly the timestamp that row was
  stamped with, so the two ends of the comparison are one reading of one clock rather than two
  readings that happen to be close. A check in a later transaction reads a strictly later timestamp
  from the same server, so elapsed time is elapsed time and never skew.

  **Amended (#718): that last sentence over-claims, and the code comment repeating it has been
  corrected.** `now()` is `transaction_timestamp` and both settle paths run at READ COMMITTED, so an
  `IssueCommand` that BEGAN after a settle-check's transaction did can still commit before one of its
  statements takes a snapshot; the intended row it then reads carries a `ts` later than that
  transaction's `now()`, and the delta is negative. No verdict is wrong today, and not by luck: a
  negative delta is less than any positive window, which is `pending`, the right answer for a command
  issued a moment ago, and the zero case never consults a timestamp at all. What is corrected is the
  claim, not the behaviour, because an invariant an isolation level does not provide is one a future
  change can lean on. `settled_at` is stamped with
  the same value, which also puts it in the currency the insert path's own `now()` already uses.
- **Decision (the zero window):** a window of zero is **terminal by construction**, checked before any
  arithmetic. `settle_window_seconds: 0` is the documented way to say "settle immediately", which is a
  claim about **intent** rather than about elapsed time, so no timestamp of any provenance may make it
  `pending`. Taking the clock from the database is what makes every other window honest; this is what
  makes the zero case independent of the clock rather than merely agreeing with it, and it is the half
  that survives a future caller that supplies `now` from somewhere else.
- **What was refused:** **stamping the sample from Go** on the write path, which is the other way to
  get one clock. It inverts the ripple: `ts` defaults to `now()` across every telemetry table and other
  readers rely on database time ordering, so moving the authority to the process would touch far more
  than settlement to fix a defect that lives in settlement. Also refused: **widening the comparison
  with a tolerance**. A tolerance makes the test stop failing without making the behavior stop
  depending on skew, which is the failure this repo has paid for twice.
- **Consequence (cost, stated):** one extra round trip on each of the two settle paths, `IssueCommand`
  and `CommandSettlement`. Both already run several statements inside one transaction, so it is a small
  fraction of either, and it is the price of the comparison being about time rather than about which
  host answered. `Settle` stays **pure** and still takes `now` as an argument; what changed is who
  supplies it, which is the repo's own rule about the clock being an edge concern pushed out of the
  core.
- **Consequence (a shape that was already true becomes stated):** nothing in the catalog ships a zero
  window with a target today (`reboot` is fire-and-forget), so the change moves no seeded behavior. It
  moves what an operator's own zero-window `command_type` does, from "terminal if the clocks happen to
  cooperate" to "terminal".
- **Extended ([#719](https://github.com/hyperscaleav/omniglass/issues/719)): the same principle, a
  different mechanism, and no round trip.** The mixed comparison survived on the history reads: a
  system's and a location's health transitions, a component's property transitions, a component's
  events, and a component's and a node's logs each built `time.Now().UTC().Add(-window)` in the server
  process and filtered a `ts` the database stamped. Less urgent than settlement, because a
  multi-minute window absorbs ordinary skew where a zero window had no margin at all, and still a
  boundary decided by two clocks with nothing bounding the difference, which is why the same read
  answers differently against a database on another host.

  The round trip `dbNow` costs was weighed rather than assumed, and refused: these are READ paths, and
  the arc that precedes this one
  ([#653](https://github.com/hyperscaleav/omniglass/issues/653),
  [#725](https://github.com/hyperscaleav/omniglass/issues/725),
  [#726](https://github.com/hyperscaleav/omniglass/issues/726)) spent itself taking round trips OFF
  them. It did not have to be paid, because the dilemma the issue poses (pay a round trip, or document
  the mixed comparison) has a third answer the settle path did not have. `Settle` compares in Go, so
  it needs `now` as a VALUE and something has to fetch it. A history read only ever hands its boundary
  to a `where`, so it needs a BOUND, and a bound can be computed where the data is: the window travels
  as a **duration** and the query filters `ts >= now() - make_interval(secs => $n)`. One clock, the
  database's, and the instant is never named in Go at all. Six read paths, zero extra statements.
  Neither horn of the issue's choice was taken and nothing was left implicit.

  What this costs instead is that the boundary is now per statement rather than per request, so the
  reachability panel's per-interface strips are each bounded by their own statement's
  `transaction_timestamp` instead of by one instant shared across the loop. Sub-millisecond, against a
  24-hour window, and the instant they used to share was skewed against the data anyway.
- **Tracked under** [#667](https://github.com/hyperscaleav/omniglass/issues/667) and
  [#719](https://github.com/hyperscaleav/omniglass/issues/719).
