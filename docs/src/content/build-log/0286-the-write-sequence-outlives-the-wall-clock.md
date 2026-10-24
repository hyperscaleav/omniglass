---
title: "The write sequence outlives the wall clock"
date: 2026-08-26
refs: []
form: bullet
---

- **The write sequence outlives the wall clock**
  ([#780](https://github.com/hyperscaleav/omniglass/issues/780), ADR-0130). The
  screenshot gate's last two flaps (the audit page interleave, the users directory row
  swap) shared one root cause: on a loaded WSL2 host the wall clock steps several
  seconds mid-run correcting NTP drift, and both `created_at` and uuidv7 ids read that
  clock, so rows written in order could sort out of it. `audit_log` and `principal`
  gain a `bigint identity` the database assigns at insert; the audit trail orders by
  it, the principal directory groups humans before nodes and orders by it within a
  kind. Regression tests simulate the step by rewriting committed rows' clock keys and
  asserting the lists hold. The remaining clock-ordered reads are #801.
