---
title: "A principal's identifier is the gateway's answer, not a stored function's"
date: 2026-08-13
status: "Accepted"
summary: "`principal_label(uuid)` is dropped. What names a principal (a human's username, else a service account's own identifying column) is declared once in the gateway (`internal/storage/principal_ident.go`) and rendered into the statements it binds. A READ projects both sources and folds them in Go; the audit insert binds the fold as one expression, because it runs inside the CALLER's transaction on every operator write and a Go resolution there would cost a second round trip the alarm write path pins as an exact equation. Two shapes of one policy are held together by an invariant test over every principal kind, not by care. A `node` stayed out of the resolution, exactly as the dropped function had it. **Amended (#738):** it is the third source, `node.name`, because preserving the omission preserved a blank actor on an audit row; a node's identifier is its unique, operator-visible `name`, never its optional `display_name`"
---

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [audit](/architecture/audit/), [identity and access](/architecture/identity-access/)
- **Context:** `principal_label(uuid)` was a stored SQL function,
  `coalesce(human.username, service.label)`, and it was wrong on both counts. It put the platform's
  answer to "what names this principal" in the database, which is the one place this repository says
  logic never lives, and it called the answer a LABEL when both branches return an identifier. It
  was also unpinned: no test named it, so it could have returned anything and the suite would have
  agreed. The word matters beyond tidiness, because `label` is about to become the schema's name for
  the friendly string on eighteen tables ([#613](https://github.com/hyperscaleav/omniglass/issues/613)),
  and a word cannot be renamed onto a column while it still means two other things somewhere else.
- **Decision:** the function is dropped and the answer moves to
  `internal/storage/principal_ident.go`, which names the tables and columns ONCE
  (`principalIdentSources`) and renders every shape a statement can need from that one list, so a
  caller picks a shape and never a column.
- **Which shape, measured rather than assumed.** A read over many rows LEFT JOINs the sources and
  folds them in Go. Two positions cannot join, and both are bounded reads: `alarmCols` is read by an
  `UPDATE ... RETURNING` (`RETURNING` cannot left-join, and giving the `UPDATE` a `FROM` clause
  would change which rows it updates), and the audit insert runs inside the CALLER's transaction on
  every operator write, where a Go fold means a second round trip and the alarm write path pins its
  statement count as the exact equation `12 + 5*slots + 4*locations` (`alarm_cost_test.go`,
  ADR-0094) that counts that insert. Both render the sources as correlated sub-selects instead. The
  first draft of this decision used sub-selects everywhere, and a review measured it: on a
  500-member group roster, projecting them AND sorting on them costs **3011 shared buffer hits and
  2000 index searches** where the join costs **18**, because Postgres does not common up two
  identical scalar sub-selects. Moving the policy out of the database was the point; paying a round
  trip on every operator write, or two thousand index probes on an unbounded roster, was not.
- **What holds the shapes together.** The columns are named once, so only the ORDER is written
  twice (as Go control flow and as a coalesce), and `principal_ident_test.go` drives a human, a
  service account, a node and an unknown id through EVERY shape against a real database, asserts
  they agree and asserts what they agree ON, and fails on the first disagreement. Asserting only
  the agreement would pass when all shapes are wrong the same way, which is exactly what naming the
  wrong column in the one source list would do. That is the recompute-and-compare shape the gateway
  already uses wherever one fact has two readers, applied to a policy rather than to a derived
  column.
- **The audit READ moved with them.** `ListAuditLog` kept its own hand-written
  `coalesce(ah.username, a.actor_username, '')`, which resolved a HUMAN actor live and a SERVICE
  actor only from the row's snapshot: a fourth statement of the policy, asymmetric between two kinds
  for no recorded reason. It now resolves both live through the same sources and falls back to the
  snapshot, which is what makes the trail survive a purge (ADR-0016).
- **A node is still not a source.** `principal_label` never read `node.name`, so a node principal
  resolved to null, and this change preserves that rather than quietly widening what an audit row
  says. Nothing seeds a node grant today, so no path reaches it; widening the resolution is its own
  decision with its own test.

  **Amended ([#738](https://github.com/hyperscaleav/omniglass/issues/738)): a node's identifier is
  its `name`, and it is the third source.** The preservation above was right for a refactor about
  WHERE the answer lives and wrong to leave standing, because what it preserved was a blank actor on
  an audit row, the kind of defect found much later from a trail that cannot say who. The identifier
  is `node.name` for the same reasons the other two columns are identifiers: it is the only
  operator-visible handle the row has, it is unique (`node_name_key`, beside `human_username_key`
  and `service_name_key`, ADR-0111), and it is already the estate address and the NATS subject
  token, so it is the string an operator would recognise. `node.display_name` cannot be one: it is
  optional and not unique, and this string is denormalized as bare text into `audit_log` where
  nothing survives beside it. Reachability is unchanged (nothing seeds a node principal with a
  grant), so this is what a node acting WOULD read as rather than a fix to something an estate sees
  today. The cost is one more `LEFT JOIN` per resolution on the read shapes and one more correlated
  sub-select on the two positions a join cannot reach, the same arithmetic the measurement above
  covers; the invariant test now drives the node arm to a real answer instead of to the empty
  string, and a second test drives all three kinds through the audit write, the audit read, the
  alarm read and the group roster, because a fold reading one column fewer than its query returned
  is a defect only a surface can show.
- **Tracked under** [#564](https://github.com/hyperscaleav/omniglass/issues/564), under
  [#613](https://github.com/hyperscaleav/omniglass/issues/613).
