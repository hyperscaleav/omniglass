---
title: "Create forms lead with the display name and derive the key as you type"
date: 2026-07-22
refs: []
form: bullet
---

- **Create forms lead with the display name and derive the key as you type.** An operator types
  "Conf Room 301" and the key fills in as `conf-room-301`, editable underneath, instead of having to
  invent a kebab identifier and get the character class right. `deriveKey` folds diacritics (so "Café"
  is `cafe`, not `caf`), collapses punctuation runs, trims leading and trailing separators, and
  respects the 100 character ceiling without leaving a trailing `-`; it only ever produces the empty
  string or a key matching the `^[a-z0-9][a-z0-9-]*$` the API enforces, asserted against that pattern
  directly. **The moment the operator edits the key it becomes theirs and stops following**, which is
  the rule that makes the pattern usable rather than infuriating, and the field says which state it is
  in. An existing entity's key is owned from the start, so relabelling can never rename: the API takes
  a rename explicitly and it stays a deliberate act. The coupling is one primitive (`createIdentity`)
  rather than three copies of a signal pair, on Components, Systems, and Locations.
