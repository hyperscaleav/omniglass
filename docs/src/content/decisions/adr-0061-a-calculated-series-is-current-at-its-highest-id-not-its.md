---
title: "A calculated series is current at its highest id, not its newest timestamp"
date: 2026-07-22
status: "Accepted"
summary: "A calculated series is current at its highest id, not its newest timestamp"
---

- **Date:** 2026-07-22 | **Status:** Accepted | **Pages:** [datapoints](/architecture/properties/)
- **Decision:** for a **calculated** series (health, and anything else the engine derives), the current
  value is the row with the **highest id**. `ts` records when the value was computed and is for display
  and history; it does not decide which row is current. For an **observed** series, `ts` still orders,
  because it is the observation time and a late arrival must not displace a newer reading, but `id`
  breaks a tie.
- **Context:** `recordHealth` writes `select clock_timestamp(), ...`, so the timestamp is evaluated in
  the SELECT list while the id comes from the identity sequence applied when the row is inserted: the
  clock is read **before** the id is assigned. Two concurrent inserts can therefore commit with `ts`
  inverted relative to `id`, and a reader ordering by `ts` then disagrees with the writer about which
  row is current.
- **Production was already right, the test was not.** Every production reader of a recorded verdict
  (`recordHealth`'s own transition check, `subtreeSystemHealth`) orders by `id`, so the writer and the
  readers agreed. The health test helper ordered by `ts`, which is why it reported verdicts the engine
  never produced. The intermittent failure was in the harness, not the product.
- **`LatestState` is a real exposure and is fixed here too.** It backs the ingest transition guard and
  ordered by `ts` alone, so a poll cycle stamping several rows in one instant resolved to an arbitrary
  one and the guard could compare against a row that is not current. It now tie-breaks on `id`.
- **Reproduced deliberately rather than waited for.** The failure needs contention: it never appeared in
  nine consecutive full-suite runs on an idle machine, and appeared within one or two attempts when six
  copies of the storage package ran at once. Under that same load the fix held for 24 runs.
- **The regression test writes the inversion directly** rather than racing it into existence, and asserts
  the two orderings genuinely disagree before asserting the outcome, so it cannot pass vacuously. It
  reads through `LocationHealth`, which reports the **recorded** verdict; `SystemHealth` recomputes live
  and cannot witness the defect.
- **Tracked as** [#356](https://github.com/hyperscaleav/omniglass/issues/356).
