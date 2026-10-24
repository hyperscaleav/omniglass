---
title: "A shipped location type is platform-owned, and a name rule can be cleared"
date: 2026-08-12
refs: []
form: bullet
---

- **A shipped location type is platform-owned, and a name rule can be cleared**
  ([#703](https://github.com/hyperscaleav/omniglass/issues/703),
  [#692](https://github.com/hyperscaleav/omniglass/issues/692), ADR-0106). The two halves of one
  question: how does a shipped rule stop applying? A release withdraws it, and an operator clears
  their own.

  The seed's contract said authoritative upsert and `SeedLocationType` said
  `on conflict (name) do nothing`, which made a shipped default a one-way ratchet: adding one
  reached every estate, removing one reached only new installs. That was not an oversight. Shipped
  location types seeded `official: false`, so the row belonged to the operator and an authoritative
  re-seed would have stomped their edits. So the ownership model is the fix rather than the seed
  statement: `location_type` becomes the second adopter of the registry fork (ADR-0095), shipped rows
  seed official, an operator's edit stores their whole version of the mutable columns in
  `registry_shadow` under the row's own uuid, and `POST /location-types/{id}:restore` discards it.
  Every read resolves the shadow over the row, the placement check and the label and name generators
  included, so a fork reaches the surfaces it exists to change.

  The wire half is the mask (ADR-0091): `{"update_mask": ["name_rule"], "name_rule": null}` clears a
  rule, because an object has no empty value to overload the way a string has `""` and an explicit
  null is indistinguishable from an omitted key after decoding. It composes with the fork rather than
  arguing with it: clearing on a shipped type forks the row with an image carrying no rule, clearing
  on an operator's own type writes null, and an operator sees one behavior.

  The migration is one statement, and the interesting part is what it stopped doing. It first
  captured operator edits into `registry_shadow` ahead of the flip, telling an edit from a shipped
  value by the **audit trail**, since a row holding a value a previous release shipped and this one
  withdrew looks exactly like an edit and preserving it would defeat the withdrawal. The architect
  ruled that premise away: no release is cut and no operator data exists, so there is no upgraded
  estate and nothing to preserve, and a review had already found the discriminator wrong in two ways
  that both lose data silently with its `create` leg exercised by nothing. So the backfill is
  `set official = true` on the four shipped names, its test asserts it writes no shadow at all, and
  ADR-0106 records that the removal rides on the premise: the first real release has estates, and
  owes them a migration that reasons about their rows. Still proven against the old shape, standing
  the database one migration back, writing the rows an installed estate holds, migrating forward with
  the real file, and driving the down leg as a round trip.

  One capability had to be defended rather than shipped: flipping the rows to official would have
  activated a guard dormant on this registry and silently taken away the contract editor for the four
  types an estate actually uses. A contract line is a row in its own table, nothing seeds one, so
  those writes no longer consult the official flag. Two expectations inverted deliberately, the seed
  test's "official location_types = 0" and the wire test's twin; the console's origin column now
  reads three ways and its blade offers **Restore shipped**.
