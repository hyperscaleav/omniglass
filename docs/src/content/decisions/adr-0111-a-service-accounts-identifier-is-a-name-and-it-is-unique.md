---
title: "A service account's identifier is a name, and it is unique"
date: 2026-08-13
status: "Accepted"
summary: "`service.label` becomes **`service.name`**: it is the username analogue for `kind=service`, the only handle the row has, so under the identity triad it is a name and it was the one place in the schema where `label` meant an identifier. The uniqueness question is answered rather than inherited: **unique**, matching `human_username_key` and `node_name_key`, because the string is denormalized as bare text into `audit_log.actor_username` and into an alarm's acknowledgement, where a duplicate is unresolvable after the fact. The table's declared identity shape moves from `ShapeIDOnly` to `ShapeHumanNotAKey`, and a new guard refuses any `ShapeIDOnly` table that carries a `name`. **Breaking wire change:** `svcBody.label` becomes `name`, and the group roster's mixed `coalesce(h.display_name, s.label, '')` splits into `name` and `display_name`, two fields each meaning one thing"
---

- **Date:** 2026-08-13 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [audit](/architecture/audit/)
- **Context:** `service` has exactly two columns, `principal_id` and `label`, and the second one is
  what IDENTIFIES a service principal: it is the username analogue for `kind=service`, the only
  operator-visible handle the row has. It was also on the wire as `svcBody.Label`, three lines from
  the human body's `display_name`, so two different concepts read as one. Under the identity triad
  an identifier is a `name`, and this was the only place in the schema where `label` meant one,
  which matters beyond tidiness because `label` is about to become the schema's word for the
  friendly string on eighteen tables ([#613](https://github.com/hyperscaleav/omniglass/issues/613)).
- **Decision:** the column is `service.name`, on the wire as `name`, in the console as **Name**.
- **Uniqueness: yes, answered rather than inherited.** The column carried no index and no
  constraint. It is now `service_name_key`, matching `human_username_key` and `node_name_key`, so
  all three principal-kind identifiers behave the same way. The reason is not symmetry, it is that
  the string is DENORMALIZED as bare text where nothing survives beside it: into
  `audit_log.actor_username` at write time so the trail outlives a purge, and into an alarm's
  acknowledgement on every read. Two service accounts sharing a name make both unresolvable after
  the fact, which is exactly why a username is unique. It is also free today and expensive later:
  there is no create path for a service principal on the gateway or the API, so nothing can be
  holding a duplicate an operator typed, and the estate has no releases and no operator data. The
  migration still copes: a dedupe backfill runs first, keeping the name on the oldest row of each
  duplicated set (`principal.id` is uuidv7, so it sorts by creation time) and suffixing every other
  with its own principal id, which is unique by construction rather than by luck.
- **The declaration moved with it, and grew a guard.** `service` was declared `ShapeIDOnly`, whose
  published sentence is "nobody names it". That was believable only while the identifier was called
  `label`, and renaming the column would have left the declaration saying the opposite with a green
  suite, because nothing checked it. `service` is now `ShapeHumanNotAKey` (a username analogue, on
  its own rule, not an address), and a new guard reads the generated schema facts and fails any
  `ShapeIDOnly` table carrying a `name` column.
- **The mixed fallback is what the rename was for.** The group roster read
  `coalesce(h.display_name, s.label, '')` into one field: a human's friendly string falling through
  to a service account's identifier. Written out after the rename it reads
  `coalesce(h.display_name, s.name, '')`, and the mistake is on the page. The fix is not a better
  chain but two fields, `name` (the identifier, resolved by the gateway's `principalIdent`) and
  `display_name` (the friendly string, which only a human has a column for), with the renderer
  choosing. A service member's identifier used to arrive in a field called `display_name`, which an
  API test asserted verbatim.
- **Breaking wire change**, on two read shapes: `svcBody.label` is `svcBody.name`, and the roster's
  `username` is `name` with `display_name` narrowed to humans. No CLI flag moves, because neither
  field is a request field.
- **Tracked under** [#563](https://github.com/hyperscaleav/omniglass/issues/563), under
  [#613](https://github.com/hyperscaleav/omniglass/issues/613).
