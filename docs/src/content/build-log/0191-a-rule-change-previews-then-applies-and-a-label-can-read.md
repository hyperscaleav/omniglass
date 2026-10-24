---
title: "A rule change previews then applies, and a label can read where it sits"
date: 2026-08-11
refs: []
form: bullet
---

- **A rule change previews then applies, and a label can read where it sits**
  ([#685](https://github.com/hyperscaleav/omniglass/issues/685), the fifth slice of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657),
  [ADR-0100](/architecture/decisions/#adr-0100-a-label-cascades-where-the-blast-radius-is-a-placement-and-waits-for-the-verb-where-it-is-the-estate)).
  The verb is what the sub-issue is named after and the smaller half of what the slice did. The
  larger half is that placement in the data map made slice 2's write-path completeness argument void,
  and the argument was the load-bearing part: it could enumerate five acts and call the set complete
  precisely because every fact its map held was the labelled row's own.

  So the acts were re-derived from the map rather than inherited, and the derivation turned up more
  than the sub-issue listed. `MoveSystem` had never stamped a label at all, correctly, while a
  system's map carried nothing a move could touch; `LocationLabel` made it a write path. Membership
  is a write path the epic never named: a component reads its PRIMARY system's type, and `AddMember`,
  `RemoveMember`, `SetPrimaryMember` and `AssignRole`'s implicit bind each move which system that is.
  And `CreateComponent` stamped BEFORE binding its membership, so every create naming a system
  rendered against no system and stored the answer.

  Two acts the sub-issue DID list turned out to be vacuous, which is worth recording as a finding
  rather than quietly satisfying. Moving a location restamps nothing: a location's own map carries
  `Name` and `TypeName`, and a component reads the label of the room it is in rather than that room's
  ancestors, so a campus rename is free. The test that pins it fails the day a location gains an
  ancestry fact, which is the honest way to hold a claim that is currently true by construction.

  The line the slice draws is BLAST RADIUS rather than ownership. Bounded by a placement, the estate
  is restamped inside the act's own transaction and is never observably stale. Bounded only by the
  estate (a rule at any tier, a `component_type`'s `display_name`, the acronym list) nothing is
  restamped and the operator applies it, which is the epic's own argument about rules applied
  consistently to everything else that can stale fifteen thousand rows at once.

  A preview is an APPLY THAT ROLLS BACK, and that was a correction the invariant forced. The
  read-only version could not list what the apply changes: recomputing locations moves their labels,
  which stales the components and systems placed at them, and the first implementation shipped an
  estate that a fresh preview immediately reported as stale. Simulating the second hop would have
  been a second implementation of the cascade for the two to drift apart on, so the preview runs the
  apply and rolls back. It is exact by construction, and the cost is stated rather than hidden: a
  preview takes the operation lock and `FOR UPDATE` on the rows it visits.

  D1 is ruled: one audit row for the operation, keyed on the rule (`label_rule`'s primary key is the
  entity kind, the one key a rename cannot orphan), carrying the count and its per-kind split. Per
  entity writes fifteen thousand rows for one click and buys a restatement, since a generated label
  is derived; the health recompute, which cascades across a whole ownership chain and audits nothing,
  is the precedent.

  Cost is measured rather than asserted: the recompute and the location cascade are both flat in row
  count on [#650](https://github.com/hyperscaleav/omniglass/issues/650)'s counting instrument, and a
  placed, system-bound, generated create is pinned at nineteen statements with every one of them
  named, of which exactly one is this slice's. Twenty-nine mutations, with three surviving the first
  pass: the create-ordering mutation and the unknown-kind refusal each survived because a second
  guard covered them, and the API's update-scope wiring survived outright, because every case in that
  file drove an owner whose read and update scopes were both `all` and a handler passing the read
  scope twice was indistinguishable from a correct one.

  The honest limit: the console has no rule editor, so the verb is reachable from the API and the
  generated CLI and nowhere an operator would find it. `location_type` and `system_type` gained
  `label_rule` on the wire here, because without them the routes this slice ships could never find a
  rule an operator had changed, but the global tier still has no route of its own.
