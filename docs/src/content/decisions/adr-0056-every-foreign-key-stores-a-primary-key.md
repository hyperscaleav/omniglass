---
title: "Every foreign key stores a primary key"
date: 2026-07-22
status: "Accepted; the slug-keyed carve-out is retired by [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle), the health carve-out amended (#717)"
summary: "Every foreign key stores a primary key. **Amended (#717):** the health carve-out is gone: the advisory lock hashes `health/<kind>/<id>` and has since the identity epic (#627) landed its addressing slice, because a name-keyed lock partitions the estate only while names do and that epic scoped a location's name uniqueness to its placement"
---

- **Date:** 2026-07-22 | **Status:** Accepted; the slug-keyed carve-out below is retired by
  [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle) | **Pages:** [storage](/architecture/storage/),
  [api-first](/contributing/api-first/)
- **Decision:** every foreign key stores the target's **primary key**: a uuid for an estate entity,
  `principal_id` for a node, and the slug itself for a slug-keyed catalog (`product`, `standard`,
  `property`, `interface_type`) where the name already *is* the key. No column references a `name`.
  In exchange, the API accepts **either form** wherever a reference is written (a path segment or a
  join field in a body), trying the uuid first, and every response carries **both**: the name an
  operator reads and the id it resolves to.
- **Context: this reverses a direction set two ADRs ago.** [ADR-0053](#adr-0053-a-name-is-the-address-a-uuid-is-identity)
  found eleven tables keyed by name and six by id, and resolved the split by declaring the majority
  correct. [ADR-0055](#adr-0055-the-tag-variable-and-secret-owner-arcs-key-by-name) then converted the
  six to match, and the follow-on work was converting the rest. The premise was never examined: a
  friendly, renameable key is valuable *because* it can change, which is the one thing a foreign key
  must not do.
- **The tell was `on update cascade`.** ADR-0055 called it "the load-bearing clause" and was pleased
  that removing it made the test fail. That machinery exists only to fund the wrong choice: it is
  write amplification across every referencing row, on a rename, to protect a key that did not need
  to be a name. A uuid arc needs no clause, and the equivalent test passes because there is nothing
  to rewrite.
- **A rename was not merely inefficient, it was refused.** With `interface.component` referencing
  `component (name)` and no `on update` clause, renaming a component that owned any interface failed
  outright with a foreign-key violation. The name-keyed convention had spread past the point where
  its own cascade covered it, and the operator-facing symptom was a rename that returned an error.
- **Scope:** all 30 name-keyed foreign keys, converted across five slices grouped by subsystem rather
  than by table, so each file changed once: the estate arcs, then health and roles, then the
  collection tier (`metric_datapoint`, `interface`, `node`) and every node reference.
- **What stays a name.** The columns whose target is slug-keyed are already conformant and were not
  touched. Health passes names internally on purpose: its advisory lock hashes `health/<kind>/<name>`,
  and a mixed currency would hash two keys for one owner and silently stop serializing. (**That
  sentence stopped being true at #627; see the amendment below.** It is left standing because the log
  is append-only and because its argument is the one that reversed it.) (The
  slug-keyed targets themselves later took uuid keys too, so those columns moved to the uuid; see
  [ADR-0062](#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle).)
- **Amended (#717): the health carve-out above is no longer true, and what unmade it is the reason it
  was a carve-out at all.** The lock hashes **`health/<kind>/<id>`**, and has since the identity epic
  ([#627](https://github.com/hyperscaleav/omniglass/issues/627)) landed its addressing slice
  ([#647](https://github.com/hyperscaleav/omniglass/issues/647)); health resolves a reference to the
  row's id once, before any lock is taken. A name-keyed lock partitions an estate only while names
  partition it, and that epic scoped a location's name uniqueness to its **placement**, so two rooms
  under different buildings may both be `415a`. One key for two unrelated owners is a silent loss of
  concurrency; the mixed currency this bullet warned about is a silent loss of the serialization the
  compare-then-act recompute needs for correctness. Both halves of the argument survive, and only the
  conclusion moved. The same fact moved the lock ORDER a slice later, because a comparison that leaves
  two owners tied is not an order at all
  ([#670](https://github.com/hyperscaleav/omniglass/issues/670)); the [health](/architecture/health/)
  page has said `id` since. This entry was the last page in the corpus describing a keying scheme that
  would be a live defect if implemented from it, so the key now lives in one named function
  (`healthLockKey`) with a unit test asserting that two same-named rooms do not share a lock.
- **Guarded both ways.** `TestResponsesAddressEntitiesByName` fails on a response that names an entity
  by uuid alone; the per-tier rename tests fail if an arc stops following a rename. Each conversion was
  **mutation-checked** rather than trusted: breaking the projection had to turn the suite red.
- **Tracked as** [#343](https://github.com/hyperscaleav/omniglass/issues/343).
