---
title: "A location type is platform-owned, and a nullable object clears under the mask"
date: 2026-08-12
status: "Accepted"
summary: "`location_type` adopts the **registry fork** (ADR-0095) rather than growing a third ownership model: the shipped rows seed `official: true`, the boot seed writes them authoritatively, and an operator's edit forks into `registry_shadow` with `:restore` discarding it. That is what makes a shipped value **withdrawable**, which insert-if-absent could never be, since it can add a default to every estate and remove one from none. The one-time backfill moves the edits estates already hold ON those rows into shadows first, telling an edit from a shipped value by the **audit trail** rather than by comparing columns against what this release ships, because a row holding a WITHDRAWN shipped value is indistinguishable from an edit by inspection and preserving it would defeat the withdrawal. A location type's property and metric **contracts stay writable** on a shipped row: a contract line is a row in its own table, nothing seeds one, and the official guard was dormant on this registry until the flip would have activated it. On the wire, a nullable OBJECT field clears by being **named in `update_mask`** with no value, since an object has no empty value to overload and an explicit null is indistinguishable from an omitted key after decoding; `name_rule` is the first and the convention is now the API's, not that field's. **Amended (#703):** the discriminator is REMOVED and the backfill is only the `official` flip, because with no release cut and no operator data there is no edit to preserve; the argument for it stands unchanged for the first release that has estates, which owes them a new migration rather than this one"
---

- **Date:** 2026-08-12 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [core entities](/architecture/core-entities/), [API](/architecture/api/),
  [location types](/guides/admin/location-types/)
- **Context:** the boot seed's contract is documented as an authoritative upsert, and
  `SeedLocationType` was `on conflict (name) do nothing`. A shipped default was therefore a one-way
  ratchet: adding one reached every estate, removing one reached only new installs.
  [#657](https://github.com/hyperscaleav/omniglass/issues/657) hit it live, shipping a `name_rule` on
  `floor` and then reversing that in
  [ADR-0103](#adr-0103-a-positional-name-is-allocation-order-and-the-real-world-designation-is-a-label),
  at which point the withdrawn rule could not be taken back out of an estate that had booted in
  between, and no `PATCH` could clear one either. The do-nothing seed was not an oversight: shipped
  location types seeded `official: false`, so the rows belonged to the operator and an authoritative
  re-seed would have stomped their edits. The ratchet was the symptom; the ownership model was the
  cause.
- **Decision (ownership):** `location_type` adopts the registry-fork primitive
  ([ADR-0095](#adr-0095-an-operator-forks-a-shipped-registry-row-instead-of-the-platform-writing-it)),
  the second registry to do so after `component_type`, rather than growing a third mechanism beside
  it. Shipped rows seed `official: true` and the platform owns them, so the seed writes them with
  `ON CONFLICT DO UPDATE` and a withdrawal is just another write. An operator still shapes their own
  place vocabulary: an edit of a shipped type stores their whole version of its mutable columns as a
  shadow keyed on the row's own uuid, every read resolves the shadow over the row, and
  `POST /location-types/{id}:restore` discards it. One uuid and one name per logical row either way,
  so no foreign key, no URL and no audit row learns about the fork. The rejected alternative was the
  `default_template` / `template` column pair the global label rules use, which is right for a table
  of three rows with no operator-created siblings and wrong here: it doubles every future column on a
  registry that already has five mutable ones, and it answers only the columns somebody thought to
  split.
- **Decision (what the image carries):** the fork's whole-row rule is per registry, not per column
  name. `allowed_parent_types` is IN the image although it names other types, because a
  `location_type` is flat: it has no parent link, and the placement constraint is a fact the row
  states about itself, one an operator legitimately reshapes. Holding it official would leave a fork
  unable to say the thing the registry exists to say.
- **Decision (the migration's discriminator):** estates already carry operator edits on the shipped
  rows themselves, so a one-time backfill moves them into shadows before the flip. Telling an edit
  from a shipped value is the whole difficulty, and it is NOT done by comparing the row against what
  this release ships. That comparison cannot see the case this ADR exists for: a row holding a value a
  previous release shipped and this one withdrew differs from today's YAML and reads exactly like an
  edit, so preserving it would defeat the withdrawal, and a row edited to a value a later release
  happens to ship has the mirror problem. The **audit trail** decides instead, because it is a record
  rather than an inference: every operator write of a registry row writes its audit row in the same
  transaction, so a shipped row with no `create` and no `update` of its own has never been touched and
  whatever it holds is what some release shipped. `create` counts as well as `update` for a case that
  is reachable today, an operator deleting a shipped type and creating their own under the same name.
  The known imprecision is biased toward preserving: a contract write audits against `location_type`
  too, so one addressed by uuid looks like an edit here and leaves the row holding a shadow of the
  values it already had, which reads identically and costs a `:restore`. The reverse mistake,
  reverting an edit, is not recoverable from the console at all.

  **Amended (#703), and the discriminator is REMOVED.** Everything above answers "how do you tell an
  operator's edit from a shipped value", and the architect's ruling of 2026-08-12 is that there is no
  such edit to tell apart: Omniglass has cut no release and holds no operator data, so no upgraded
  estate exists and the capture was preserving rows that do not. A review then found the
  discriminator wrong in two ways that both lose data silently, and its `create` leg exercised by
  nothing, which is what a premise that never occurs does to the code written for it. The backfill is
  now one statement, `set official = true` on the four shipped names, and its test asserts the
  migration writes **no** shadow, so the machinery cannot grow back quietly as a fix for a problem
  that was ruled out of existence. **This is correct only while that premise holds.** The first real
  release creates estates carrying operator data, and from then on a change of ownership on a shipped
  row owes those estates an answer to the question this bullet asks, in a NEW migration reasoning
  about their rows: this one has already run, and it reasons about nothing. Whoever ships that
  release should read the paragraphs above as the standing argument, still sound; only the machinery
  went away.
- **Decision (the wire):** a nullable OBJECT field clears by being named in `update_mask` with no
  value, reusing the primitive from
  [ADR-0091](#adr-0091-an-update_mask-says-which-fields-a-patch-writes) rather than inventing a
  per-type spelling. The house three-state string sentinel does not generalize: `""` clears a string
  because a string has an empty value to overload, and an object has none (`{}` is a rule with default
  fields, not the absence of one). An explicit `null` cannot carry the intent either, since it decodes
  to the same nil as an omitted key. **This is the convention for every nullable object field that
  follows**, so it is recorded on the API page and not only in the handler.
- **Consequence (a capability that had to be defended):** flipping the shipped rows to `official`
  activates a guard that had been dormant on this registry, since no `location_type` row was official
  before. `SetLocationTypeProperty` and its three siblings would have started refusing the four types
  an estate actually uses, silently withdrawing the contract editor for them. They no longer consult
  the official flag: a contract line is a row in its own table rather than a column of the registry
  row the fork covers, and **nothing seeds one**, so every line in those tables is an operator's
  already. An ownership change may not take a working capability away as a side effect.
- **Consequence (an expectation inverts):** `internal/seed/seed_test.go` asserted "official
  location_types = 0, a shipped location type is operator-owned". It now asserts 4 and guards the
  opposite claim. The same inversion lands on the wire test and on the console, where origin reads
  three ways (shipped, yours, overridden) exactly as it does on component types, and the blade's
  destructive slot offers **Restore shipped** on a forked row.
- **What this does NOT change:** the `label_rule` table's `default_template` / `template` pair stays
  as it is ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)). It is one
  row per labelled entity kind with no operator-created rows, where a per-column split is the natural
  fit and an overlay table would be more machinery than three rows are worth. A shipped row still
  cannot be DELETED, forked or not, and an operator-created location type is untouched by any of
  this: it is written in place, it never carries a shadow, and `:restore` has nothing to give it back.
- **Tracked under** [#703](https://github.com/hyperscaleav/omniglass/issues/703) and
  [#692](https://github.com/hyperscaleav/omniglass/issues/692).
