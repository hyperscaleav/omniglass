---
title: "An operator forks a shipped registry row instead of the platform writing it"
date: 2026-08-09
status: "Accepted"
summary: "An operator's edit of a shipped (`official: true`) registry row does not write that row: it forks it into `registry_shadow`, one registry-agnostic table keyed `(registry, row_id)` on the shipped row's OWN uuid, and reads resolve the shadow over the official row; restore is deleting the shadow. One uuid and one name per logical row either way, so no foreign key, walk, audit row or URL learns about the fork. A fork captures the whole mutable row and never the structure, which makes inheritance on a nested registry resolvable per node. `component_type` is the first adopter. **Amended (#709):** a fourth fact adopters must keep, that the registry row's lock is taken in a statement of its OWN before the read that resolves the shadow, because `for update` on the resolving read serialises the transactions without refreshing what the waiter reads"
---

- **Date:** 2026-08-09 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [core entities](/architecture/core-entities/), [API](/architecture/api/)
- **Decision:** An operator's edit of a shipped (`official: true`) registry row never writes that
  row. It writes a **shadow**: the operator's version of the row's mutable columns, stored in one
  registry-agnostic `registry_shadow` table keyed `(registry, row_id)` where `row_id` is the shipped
  row's **own uuid**. Every read resolves the shadow over the official row, and restore-to-defaults
  (`POST /component-types/{id}:restore` for the first adopter) is deleting the shadow. The official row is neither updated nor
  deleted by any operator action, so a release can improve it without stomping anyone, and a delete
  of a shipped row stays refused whether or not it is forked: a fork is an overlay, not ownership.
  `component_type` is the first adopter; the other registries carrying `official` adopt as their
  slices land, needing no schema of their own.

  Three sub-decisions carry the shape, and an adopter must keep all three.

  **1. The shadow is keyed on the shipped row's uuid, in one generic table, not by a `namespace`
  column on the registry.** The rejected alternative was `namespace text` with the unique relaxed
  from `(name)` to `(namespace, name)`. It fails on addressing, which is the thing this repo has
  already settled twice. Relaxing the unique makes every name-keyed lookup on the registry return
  two rows, and `QueryRow` takes an arbitrary one; that is eight call sites on `component_type`
  alone, several of them in helpers **shared with twelve other registries**
  (`guardTypeMutable`, `registryAuditImage`, `deleteTypeRow`, `requireRegistryRow`), so a single
  missed site is a silent wrong-row bug rather than a failure. Worse, a same-table shadow needs its
  own uuid, giving one logical row two, while `product.component_type_id` and
  `role_component_type.component_type_id` are foreign keys to `component_type(id)` that would keep
  naming the official one: the fork would not take effect for the rows that matter, and the `id` a
  write echoes back would change on fork, which is the namespace leaking into the URL that
  [#655](https://github.com/hyperscaleav/omniglass/issues/655) forbids. The literal `(namespace, id)`
  composite primary key the issue sketched makes `id` non-unique and forces dropping all three
  inbound foreign keys, including the `ON DELETE RESTRICT` bought by
  [#507](https://github.com/hyperscaleav/omniglass/issues/507). Keying the shadow on the shipped
  row's uuid keeps `component_type_name_key` a global unique, keeps every foreign key and every
  existing lookup matching exactly one row, and reduces the change to *what a read resolves to*
  rather than *what anything addresses*. Generic rather than one shadow table per registry because
  twelve more registries adopt later and the per-type rule columns of
  [#657](https://github.com/hyperscaleav/omniglass/issues/657) add columns to them: a typed shadow
  table would double the DDL cost of every future column on every adopter, and this costs none. The
  polymorphic `(registry, row_id)` reference is the shape `audit_log`'s `(resource, resource_id)`
  already uses.

  **2. A fork captures the WHOLE mutable row, not only the edited column.** So a later release's
  improvement to a column the operator did not touch does **not** reach a forked row: the operator
  took the row over, and restore is how they hand it back. The alternative, a sparse overlay where
  unedited columns keep tracking the release, is not merely a different trade here, it is
  unrepresentable: `stem`, `icon` and `abbrev` are nullable and **null already means "inherit from
  the nearest ancestor"**, so a sparse overlay would need null to mean both "not overridden" and
  "inherit", i.e. a third state and a separate overridden-column set. That is the merge and
  three-way-diff machinery the issue's thin cuts rule out. The image is therefore stored as a
  whole-row jsonb of the mutable columns, each nullable column written as an explicit null rather
  than dropped, and the resolve overlays **the keys the image carries**. That last distinction is
  what makes a column added to a registry *after* a fork resolve to its official value instead of to
  nothing, which is the only sane answer for a column the fork could not have had an opinion about.

  **3. Inheritance across the shadow boundary resolves PER NODE, in official structure space.** The
  split that makes it decidable is **structure versus facts**. Structure is the uuid, the name, the
  `official` flag and `parent_id`; a shadow never carries any of them, so a fork restates what a
  node says and never moves, renames, or re-provenances it. Facts (`display_name`, `stem`, `icon`,
  `abbrev`, `default_tags`) resolve shadow-over-official at each node independently. The walk
  therefore visits the same node sequence it always did, and at each node reads that node's
  effective row before applying the existing first-non-null-wins rule. Forking an ancestor reaches
  every descendant that does not override the field; forking a leaf does not cut it off from the
  ancestors it inherits from; a chain with some nodes forked and some not is not a special case,
  because **there is no such thing as a forked chain, only forked nodes**. The rejected alternative,
  per chain ("a forked leaf walks only forked ancestors"), would make forking a leaf silently drop
  every inherited fact, and it names an object the schema does not have: `parent_id` is per row. The
  ruling survives `parent_id` becoming patchable later, which it is not today
  (`ComponentTypePatch` has no parent field and there is no reparent leg): the walk follows the
  *effective* row's parent, so a reparenting shadow would diverge the chain from that node up, which
  is still per node.

  **Amended ([#709](https://github.com/hyperscaleav/omniglass/issues/709)): a FOURTH fact, and the one
  an adopter is most likely to get subtly wrong. Every path that reads a row in order to write its
  shadow takes the registry row's lock in a statement of its OWN, before the read that resolves the
  shadow over it.** Sub-decision 2 is what makes the lock necessary: a fork writes the whole mutable
  row back, so two operators editing different fields of one shipped row at once both compute an image
  from the same starting point, and the second silently discards the first's field with no error and no
  audit anomaly, both writes in the log. What the amendment adds is that `for update` ON the resolving
  read does not do the job. At READ COMMITTED a statement's snapshot is taken when the statement
  begins, which is before it blocks; the waiter then unblocks on a row that was **locked** rather than
  updated, so there is no EvalPlanQual recheck, and the left join it had already evaluated still
  reports the shadow as its stale snapshot saw it. That serialises the two transactions without making
  the second read what the first wrote. It is not a hypothetical: `location_type` shipped exactly that
  lock in [#703](https://github.com/hyperscaleav/omniglass/issues/703), and a paired-fork test still
  lost an edit against it, which is how the shape was found. Locking FIRST makes the resolving read a
  new statement taking a new snapshot, after the lock is held, so it reads the shadow the previous
  holder committed. Cost: one round trip on the patch and restore paths of an adopting registry, the
  same trade [ADR-0108](#adr-0108-settlement-reads-one-clock-and-a-zero-window-is-a-statement-of-intent)
  states for the settle paths.
- **Context:** [#655](https://github.com/hyperscaleav/omniglass/issues/655), the first prerequisite
  of [#657](https://github.com/hyperscaleav/omniglass/issues/657). Thirteen tables carry `official`
  and enforcement was a flat refusal (`ErrTypeOfficial` on any patch), which is correct as far as it
  goes and leaves an operator nowhere to go. It also produced an inversion #657 runs into:
  `component_type` and `product` seed `official: true` and are closed, while `location_type` and
  `standard` seed `official: false` (example content an estate owns) and are open, so a per-type
  rule column would be writable on two registries and unwritable on the other two, backwards from
  where the machinery lives. The alternative on the table was a per-column carve-out from the
  official lock, which trades one inconsistency for a worse one: some columns of an official row
  writable and others not, with the rule living in code rather than in the model. `component_type`
  is the first adopter precisely because it is nested, so it forces sub-decision 3 rather than
  deferring it.
- **Open:** what happens to a shadow when a later release removes the official row it shadows. The
  emergent behaviour today is "orphan it, inertly": resolution is driven by the official row left
  joining the shadow, so an orphan is invisible rather than corrupting, and the polymorphic key
  carries no foreign key that would have blocked or cascaded the removal. Surfacing it as a conflict
  an operator resolves is the alternative, unbuilt.
