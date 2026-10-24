---
title: "A service account's identifier is a name, and it is unique (#563)"
date: 2026-08-14
refs: [563]
form: bullet
---

- **A service account's identifier is a name, and it is unique (#563).** `service` has exactly two
  columns and the second one is what identifies the row: the username analogue for `kind=service`,
  the only operator-visible handle it has. It was called `label`, and it was on the wire three lines
  from the human body's `display_name`, so two different concepts read as one. It is now
  `service.name`, `name` on the wire, and **Name** in the console.

  **The uniqueness question was answered rather than inherited.** The column carried no index and no
  constraint; it now carries `service_name_key`, matching `human_username_key` and `node_name_key`.
  Not for symmetry: the string is denormalized as bare text into `audit_log.actor_username` at write
  time and into an alarm's acknowledgement on every read, and a duplicate makes both unresolvable
  after the fact with no uuid left beside the text. The migration copes with duplicates rather than
  assuming there are none, in the foundation / backfill / floor shape: the oldest row of each
  duplicated set keeps the name (`principal.id` is uuidv7, so it sorts by creation time) and every
  other is suffixed with its own principal id, unique by construction. The harness migrates an empty
  database, so running the chain forward can never exercise that sweep; a test stands the schema
  between the rename and the floor, writes three accounts under one name, and runs the extracted SQL
  twice, then lands the constraint on the result.

  **The identity declaration moved with the column, and grew the guard that would have caught it.**
  `service` was declared `ShapeIDOnly`, which publishes "nobody names it", and that was believable
  only while the identifier was spelled `label`. It is now `ShapeHumanNotAKey` with its reason, and
  a new guard reads the generated schema facts and fails any `ShapeIDOnly` table that carries a
  `name`.

  **The clearest illustration of why the word had to move is the group roster.** It read
  `coalesce(h.display_name, s.label, '')` into a single field, a human's friendly string falling
  through to a service account's identifier; after the rename the same chain reads
  `coalesce(h.display_name, s.name, '')` and the mistake is on the page. It is now two fields, the
  identifier (`name`, resolved by the gateway's `principalIdent`) and the label (`display_name`,
  which only a human has a column for). An API test had been asserting a service account's
  identifier out of a field called `display_name`; it now asserts it out of `name` and asserts the
  display name is empty. Recorded as
  [ADR-0111](/architecture/decisions/#adr-0111-a-service-accounts-identifier-is-a-name-and-it-is-unique).

  **Breaking wire change** on two read shapes (`svcBody.label` to `name`, and the roster's
  `username` to `name`). No CLI flag moves: neither is a request field.
