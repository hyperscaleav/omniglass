---
title: "The history windows are bounded by the database's own clock"
date: 2026-08-13
refs: []
form: bullet
---

- **The history windows are bounded by the database's own clock**
  ([#719](https://github.com/hyperscaleav/omniglass/issues/719)). Six read paths built
  `time.Now().UTC().Add(-window)` in the server process and then filtered a `ts` column the database
  stamps with `default now()`: a system's and a location's health transitions, a component's property
  transitions, a component's events, and a component's and a node's logs. Two clocks on one
  comparison, which is the defect [ADR-0108](/architecture/decisions/#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)
  settled for settlement. Nothing is wrong at a 30-day window, where skew of a few hundred
  milliseconds moves the edge by an invisible amount; what is wrong is that the edge is decided by two
  clocks with nothing bounding the difference, so the same read answers differently against a database
  on another host than against a local one.

  ADR-0108's own answer, reading `select now()` in the transaction, costs one round trip, and it was
  weighed rather than copied. These are read paths, and the three slices before this one
  ([#653](https://github.com/hyperscaleav/omniglass/issues/653),
  [#725](https://github.com/hyperscaleav/omniglass/issues/725),
  [#726](https://github.com/hyperscaleav/omniglass/issues/726)) were spent taking round trips OFF
  them. It did not have to be paid: `Settle` compares in Go and needs `now` as a value, but a history
  read only hands its boundary to a `where`, and a bound can be computed where the data is. The window
  now travels as a **duration** and the query filters `ts >= now() - make_interval(secs => $n)`. One
  clock, six read paths, no extra statement anywhere. A window of zero stays the unwindowed read and
  binds nothing at all.

  The regression test says what it proves and what it does not. Two clocks that agree return the same
  rows as one, so nothing a fixture can build tells them apart on a box where the process and the
  database share a clock, which is every box the suite runs on. What is observable is whether an
  instant crosses the seam at all, so the test captures the statements each read really issued and
  asserts that none of them binds a `time.Time` and that exactly one bounds `ts` against `now()`. A
  process that sends no instant is not the one deciding the boundary, whatever its clock says.
