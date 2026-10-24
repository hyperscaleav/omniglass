---
title: "The last stored function retires, and a principal's identifier becomes the gateway's answer (#564)"
date: 2026-08-14
refs: [564]
form: bullet
---

- **The last stored function retires, and a principal's identifier becomes the gateway's answer
  (#564).** `principal_label(uuid)` was a stored SQL function, `coalesce(human.username,
  service.label)`, and it was two defects wearing one name: it put the platform's answer to "what
  names this principal" in the database, which is the one place this repository says logic never
  lives, and it called the answer a **label** when both of its branches return an identifier. No
  test named it, so it could have returned anything and the suite would have agreed.

  The answer now lives in `internal/storage/principal_ident.go`, which names the two sources once
  and renders every shape a statement can need from it, so a caller picks a shape and never a
  column. A read over many rows LEFT JOINs the sources and folds them in Go. Two positions cannot
  join: `alarmCols` is read by an `UPDATE ... RETURNING` and `RETURNING` cannot left-join, and the
  audit insert runs inside the caller's transaction on every operator write, where a Go fold would
  cost a second round trip and the alarm write path pins its statement count as the exact equation
  `12 + 5*slots + 4*locations` (#674) that counts that insert. Both of those render the sources as
  correlated sub-selects, and both are bounded reads. Which shape is measured rather than assumed:
  on a 500-member group roster the sub-select shape projected AND sorted on costs 3011 shared buffer
  hits and 2000 index searches where the join costs 18. Moving the policy out of the database was
  the point; paying a round trip on every operator write, or two thousand index probes on a roster,
  was not part of it, and the pinned equation is unmoved.

  Only the ORDER is written twice, so `principal_ident_test.go` is the invariant between the two
  shapes: it drives a human, a service account, a node and an unknown id through both against a real
  database. A `node` stays out of the resolution exactly as the dropped function had it, so no audit
  row changes what it says. Recorded as
  [ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions),
  with `principal_label` appended to the vocabulary denylist.
