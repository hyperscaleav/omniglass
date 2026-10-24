---
title: "The identity triad, on every surface"
date: 2026-08-04
refs: [545]
form: bullet
---

- **The identity triad, on every surface** ([#545](https://github.com/hyperscaleav/omniglass/issues/545)).
  The vocabulary above is reversed, and this entry records why rather than quietly replacing it. The
  columns have always been `id` / `name` / `display_name`; what drifted was the console, which had been
  taught to call the friendly string the Name and the identifier the Key. Prior art settled it: across
  35 systems in five families, no system names an entity's friendly string a `label`, and `display_name`
  is the blessed standard field in AIP-148, which this API already follows. So the schema does not move
  and every other surface agrees with it ([ADR-0076](/architecture/decisions/)).

  Proving the classification is what exposed how wrong it was. `tag`, `variable`, and `secret` were
  declared keyspace on the strength of the word "key" in their prose; none of them carries a dot, which
  is the only thing the keyspace rule adds, so all three are ordinary entity names. Behind the
  misclassification `tag` had its own private validator, a fourth name rule with its own ceiling and
  error text, and `variable` and `secret` had **no name validation at all**: a secret could be named a
  uuid. `CreateSecret` checked its crypto provider before the name, so an illegal name reported "no
  secret key provider configured". Only `property_type`, `event_type`, and `command_type` are keyspace.

  There is now one validator. `storage.ValidateName(table, name)` picks between the two rules from the
  table's declared identity shape, so a call site cannot pick the wrong rule or forget to pick at all,
  which is how `system_role` had reached production unvalidated. The four superseded validators are
  **deleted**, not renamed: leaving them exported would have let the next registry opt out of the
  primitive while passing review and `make test`. `principal_group`'s looser API-layer pattern folded in
  with it ([ADR-0077](/architecture/decisions/)).

  Renaming became an explicit act. `POST /<collection>/{ref}:rename` is gated by `<resource>:rename`,
  and `name` is removed from the four PATCH bodies, because two ways to rename would have made the
  permission decorative. `audit_log.resource_id` now always holds the primary key: it had held a name
  or the caller's reference depending on the route, so one entity accumulated two audit keys and a
  rename orphaned half its history.

  Three guards carry the invariants, and each reads the tree rather than a hand-written list, because a
  hand-written list is how the last three escapes happened. One checks every `ValidateName` call site
  names a declared table; one checks all 96 `writeAuditRes` sites key on a primary key, as a whitelist,
  because a blacklist waves through the commonest mistake of passing a dual-accept route's `id`; and one
  checks that a console label matches the field it labels, after eleven detail blades were found showing
  the identifier and the friendly string both under the word "Name" while 812 tests were green.
