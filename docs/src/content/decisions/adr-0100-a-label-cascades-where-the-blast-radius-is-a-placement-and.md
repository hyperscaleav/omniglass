---
title: "A label cascades where the blast radius is a placement, and waits for the verb where it is the estate"
date: 2026-08-10
status: "Accepted"
summary: "A label rule reads its entity's PLACEMENT (a component's location label and its primary system's type label, a system's location label), reversing ADR-0098's exclusion, and the write paths that keep those honest are derived from the map rather than enumerated, a derivation the epic's review pass then caught missing the one the DATABASE performs (an `ON DELETE CASCADE` is a write path with no Go on it, so a system's delete now releases its memberships explicitly). The line is BLAST RADIUS, not ownership: bounded by a placement (the rows at one location, one system's members, one component's membership) it cascades inside the act's own transaction; bounded only by the estate (a rule at any tier, a classification row's display_name, the acronym list) it waits for the preview-then-apply verb. A preview is an apply that rolls back, so it lists exactly what the apply changes including the second hop. One audit row per operation, keyed on the rule, never one per changed entity"
---

- **Date:** 2026-08-10 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [identity and access](/architecture/identity-access/),
  [glossary](/architecture/glossary/)
- **Decision (placement is in the data map):** A component's label rule reads `LocationLabel` (the
  label of the location it sits at) and `SystemTypeLabel` (the label of its primary system's type); a
  system's reads `LocationLabel`. This **reverses the narrowing in
  [ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)**, whose title is now
  false of the code and is left standing as the record of what was believed. The worked example is
  `{{.SystemTypeLabel}} {{.LocationLabel}} {{.TypeName}}` rendering `Boardroom 204B Display`.
- **The keys are flat, never dotted,** because the map is a closed `map[string]string` and that
  flatness is half the sandbox argument ADR-0098 rests on: a string cannot be traversed through, so
  `{{.Location.Label}}` would be a handle on another row where `{{.LocationLabel}}` is a fact copied
  out of one.
- **Which location, precisely:** the row's OWN `location_id`. Not an ancestor of it, and not the
  location its plane root sits at. A component's label says where that component is, so it reads the
  column that says so; a nested component with no location of its own reads placement as absent. The
  alternative would make a label depend on an ANCESTOR COMPONENT's placement, so moving a parent
  would stale every descendant, and it costs a recursive walk per row on a path that runs on every
  create. `LocationLabel` is the location's READ LADDER (the label an operator typed, else the
  location's own name), not the raw column, because a shipped estate has no location labels at all
  and reading the column alone would render placement as blank for every row in it.
- **Consequence, and the reason this is one slice rather than two: ADR-0098's completeness argument
  is void.** It could enumerate five write paths and call the set complete precisely because every
  fact its map held was the labelled row's own. That argument was already thinner than it read
  (`TypeName`, `ProductName`, `VendorName`, `Stem`, `TypeAbbrev` and the rule itself are columns on
  registry rows), and placement ends it: the set of acts that stale a label is no longer the set of
  acts on the labelled row. The write paths are therefore **derived from the map** and, more
  importantly, the invariant that guards them stopped being an enumeration at all: it is now an
  estate-wide question the gateway answers (`PreviewLabelRecompute` returning nothing), so a write
  path nobody thought of fails it.
- **Decision (the line is blast radius, not ownership):** An act whose blast radius is bounded by a
  PLACEMENT cascades eagerly, inside its own transaction, so the estate is never observably stale: a
  location's rename, relabel or reclassify restamps what is placed at it; a system's reclassify
  restamps its member components; every act that moves a component's primary membership (`AddMember`,
  `RemoveMember`, `SetPrimaryMember`, `AssignRole`'s implicit bind, a create naming a system, and a
  system's DELETE, which releases every membership it holds) restamps that component; a system's
  `:move` stamps its own label, which it never did before.
  An act whose blast radius is bounded only by the ESTATE restamps nothing and waits for the verb:
  a rule changing at any tier, a `component_type`'s or `product`'s or `vendor`'s `display_name`
  changing, and the acronym list changing. That is the epic's own argument applied consistently:
  editing a shared classification must not silently rewrite fifteen thousand rows any more than
  editing a rule may.
- **Consequence, added by the epic's review pass: a write path can be one the DATABASE performs.**
  Deriving the write paths from the map caught every explicit mover of a primary membership and
  missed the implicit one. `system_member_system_id_fkey` is `ON DELETE CASCADE`, so deleting a
  system has always taken its memberships with it, in the parent's own `DELETE` where the gateway
  can attach nothing to the loss. A component in a boardroom under `[{{.SystemTypeLabel}}]` went on
  reading `[Boardroom]` after the boardroom was gone, and the invariant this decision rests on
  (`PreviewLabelRecompute` returning nothing) said so, which is the property it was built for. The
  fix is not another restamp bolted to the delete: the memberships are now **released explicitly**,
  one step ahead of the row's own delete, so a delete is the same act `RemoveMember` performs and
  gets the same two consequences (the sole survivor is promoted to default, the released components
  restamp in one recompute rather than one apiece). The promotion half was a pre-existing divergence
  between the two doors into `system_member`, and it is fixed here rather than filed because the
  label the delete leaves behind depends on which membership answers afterwards. The generic
  scoped-CRUD delete grew a `beforeDelete` hook to hold it, since the rows a hook needs are
  unreadable by the time an `afterDelete` runs. The completeness lesson generalizes past labels:
  **an `ON DELETE CASCADE` is a write path with no Go on it**, and the estate's other two
  (`system_role_assignment` on both `system_id` and `role_id`) feed nothing into a rule today.
- **Consequence:** a location rename is bounded by what is placed AT that location, not by its
  subtree, because a component reads the label of the room it is in and never that room's building.
  A campus rename is free. That is a property of the data map, so if a later slice gives a location
  an ancestry fact (a positional type's generated name is the obvious candidate) the cascade owes it
  a subtree arm, and the test that pins the current answer is the one that fails first.
- **Decision (a preview is an apply that rolls back):** The read-only implementation of a preview
  cannot list exactly what the apply changes. Recomputing locations moves their labels, which stales
  the components and systems placed at them, so the honest answer includes rows of two other kinds
  that exist only as a consequence of writes a read-only pass never made. Simulating those writes
  would be a second implementation of the cascade for the two to drift apart on. So a preview runs
  the apply, collects the change set, and rolls back: exact by construction rather than by argument.
  The cost is stated rather than hidden: a preview is not a pure read. It takes the operation lock
  and `FOR UPDATE` on the rows it visits and produces WAL that is discarded, so it is an operator
  gesture, not something to put behind a keystroke. It still does not promise atomicity ACROSS the
  pair, and deliberately: holding a lock between two HTTP requests would let an operator who opened a
  preview and wandered off block every write on the tier. The apply's own returned set is what closes
  that gap.
- **Decision (epic D1, one audit row for the operation):** A bulk recompute writes **one** audit row,
  verb `recompute`, resource `label_rule`, `resource_id` the entity kind, carrying the affected count
  and its per-kind split. Per changed entity was the alternative and it loses on both halves of the
  question it claims to answer better: it writes fifteen thousand rows for one click, in one
  transaction, on a table every other write shares, and the per-entity trail it buys is a
  restatement, since a generated label is DERIVED and "why does this row read what it reads" is
  already answered by the rule and the row's own facts. "Who changed the estate's labels and when" is
  answered by exactly this row and by nothing in a per-entity trail. The nearest precedent agrees: a
  health recompute cascades across a whole ownership chain and audits nothing, because it is a
  consequence of an act that is itself audited. Which is also why a CASCADE writes no row of its own:
  the rename or reclassify that triggered it already has one. The row is keyed on the rule rather
  than on the entities because `label_rule`'s primary key genuinely is the entity kind, so it is the
  one key a rename cannot orphan.
- **Decision (a cascade is not scope-filtered; the verb is):** The verb selects on the caller's read
  scope AND their update scope, both injected into the one query, so an operator can neither preview
  nor apply outside what they may already see and change. A cascade is not scope-filtered, because it
  is not a query anyone asked for: it is the rest of a write the operator already made, and leaving a
  row stale because it sits outside the grant that let them rename the location would break the
  invariant the stored label rests on, silently. The health recompute crosses scope boundaries for
  the same reason. No new permission either way: a recompute is gated by the entity's own `:update`,
  and so is the preview, because a preview is half of an edit rather than a report.
- **Decision (lock order, written down rather than discovered):** membership, then label, then
  health. A bulk recompute takes one coarse advisory lock for the whole operation plus `FOR UPDATE`
  on the rows it visits; the single-row stamps take neither, because each already holds its row's
  lock from the `UPDATE ... RETURNING` it rides behind.
- **Consequence, measured:** the recompute and the location cascade are both flat in row count, held
  there by [#650](https://github.com/hyperscaleav/omniglass/issues/650)'s counting instrument rather
  than by intent, with the classification resolved once per distinct product or classifier pair and
  the placement facts, the global rule and the acronym dictionary each read once per operation. A
  placed, system-bound, generated component create costs nineteen statements, of which exactly one is
  this decision's.
- **Tracked under** [#685](https://github.com/hyperscaleav/omniglass/issues/685), the fifth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657).
