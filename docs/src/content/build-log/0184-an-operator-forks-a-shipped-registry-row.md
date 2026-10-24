---
title: "An operator forks a shipped registry row"
date: 2026-08-09
refs: [655]
form: bullet
---

- **An operator forks a shipped registry row** ([#655](https://github.com/hyperscaleav/omniglass/issues/655),
  [ADR-0095](/architecture/decisions/#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it),
  the first prerequisite of [#657](https://github.com/hyperscaleav/omniglass/issues/657)). Thirteen
  tables carry `official`, and the enforcement was a flat refusal: `UpdateComponentType` returned
  `ErrTypeOfficial` for any patch to a shipped row. Correct as far as it went, and it left an operator
  nowhere to go. An edit now **forks**: the operator's version of the row lands in `registry_shadow`,
  reads resolve it over the official row, the official row is never written, and restore
  (`POST /component-types/{id}:restore`) discards the fork so later releases reach the row again.

  The key was the decision worth the time, because twelve more registries copy whatever shape this
  slice picks. A `namespace` column with the unique relaxed to `(namespace, name)` was rejected on
  addressing: it makes every name-keyed lookup return two rows with `QueryRow` taking an arbitrary
  one, across helpers shared with the other registries, and it gives one logical row two uuids while
  `product.component_type_id` keeps naming the official one, so the fork would not take effect for the
  rows that matter. Keying the shadow on the shipped row's **own uuid**, in one registry-agnostic
  table, changes only what a read resolves to and never what anything addresses: the name stays
  globally unique, every foreign key and `ON DELETE RESTRICT` survives, and the namespace cannot reach
  a URL. Generic rather than a shadow table per registry because #657 adds columns to the adopters,
  and a typed shadow would double the DDL cost of every future column thirteen times over.

  `component_type` is the first adopter because it is nested, which forces the inheritance question
  rather than deferring it. The answer is **per node, in official structure space**: a shadow carries
  facts (`display_name`, `stem`, `icon`, `abbrev`, `default_tags`) and never structure (the uuid, the
  name, the `official` flag, `parent_id`), so the walk visits the same chain it always did and reads
  each node's effective row on the way. Forking an ancestor reaches every descendant that does not
  override the field; forking a leaf does not cut it off from what it inherits. There is no such
  thing as a forked chain, only forked nodes.

  A fork captures the **whole** mutable row, so a later release's change to an unedited column does
  not reach a forked row, which is the price of taking it over and what restore undoes. Sparse was not
  a live option: null on `stem`/`icon`/`abbrev` already means "inherit from the parent", so a sparse
  overlay would need null to mean two things at once. The resolve still overlays only the keys the
  image carries, which is what makes a column added *after* a fork resolve to its official value.

  Mutation is what made the tests worth having, and it found a live bug on the way: dropping the
  overlay from the inheritance walk left the assertions passing, because `encoding/json` unmarshals
  into an existing non-nil pointer by writing **through** it, so decoding a shadow's stem into the
  official row's `*string` had already rewritten the string that row points at. The overlay now
  decodes into a fresh value and assigns, and each overlay case gets its own row so the test cannot
  hide the same aliasing again. The re-seed acceptance carries the same suspicion: the fixture scuffs
  the official row in SQL before each re-seed and asserts the seed put the shipped value back, so
  "the fork survived" cannot pass by the seed having skipped the row.

  The console reads three origins now, not two: **official**, **custom**, and **overridden**. Edit is
  live on a shipped row for a caller holding `component_type:update` (a viewer still cannot fork what
  they cannot write), the destructive slot carries **Restore shipped** on a forked row and a greyed
  Delete with the official sentence on a pristine one, and the shared registry lock keeps the flat
  read-only verdict for the twelve registries that have not adopted.
