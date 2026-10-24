---
title: "A name is the address, a uuid is identity"
date: 2026-07-21
status: "Superseded in part by [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key)"
summary: "A name is the address, a uuid is identity"
---

- **Date:** 2026-07-21 | **Status:** Superseded in part by
  [ADR-0056](#adr-0056-every-foreign-key-stores-a-primary-key). Its API half stands: a reference is
  addressed by name. Its schema half ("a new table references an estate entity by `name` with `on
  update cascade`") is reversed, and responses now carry the id **beside** the name rather than
  instead of it.
- **Context:** the pattern was real and dominant but never applied to the original entities.
  Eleven tables keyed their estate references by `name`, six by `id`, split by age. Worse, the API
  **accepted names on write and returned uuids on read**: a component created with
  `{"parent": "rack", "location": "hq-b1"}` read back as `{"parent_id": "0198f...", "location_id":
  "0198f..."}`. The body did not round-trip, so every client fetched a second collection and joined by
  uuid to render one label. The console carried exactly that map until it was deleted in
  [#329](https://github.com/hyperscaleav/omniglass/issues/329).
- **Decision:** every request **and response** addresses another entity by its **name**. A uuid appears
  only as an entity's own `id`. Two exceptions: an entity with **no name** (an interface, a stored
  value, an audit row, a grant, a principal) and a **slug-keyed catalog**, whose id already is a name.
  A new table references an estate entity by `name` with `on update cascade`.
- **Normalized:** `parent_id` and `location_id` on component and system, `parent_id` on location, and
  the redundant `owner_id` on tag bindings, variables, and secrets, which already carried `owner_name`
  beside it. Nine fields, seven of them found by survey and **two by the guard test**, which caught a
  `SecretBody.owner_id` the survey missed.
- **Enforced by contract, not prose.** `TestResponsesAddressEntitiesByName` walks the generated
  OpenAPI and fails on any field naming another entity by uuid. The failure it prevents is invisible
  otherwise: a body emitting `parent_id` still serves 200s, and the cost only appears in the clients.
- **Deliberately not in scope:** `secret`, `variable`, and `tag_binding` still key their owner arcs by
  uuid in the schema, and the cascade resolvers compare those uuids directly. Converting them is a data
  migration plus a rewrite of resolution SQL reworked in
  [ADR-0052](#adr-0052-the-cascade-resolves-through-membership-and-secrets-carry-no-system-band).
  The API contradiction is what operators saw and is fixable without touching resolution. The rule binds
  new tables; the stragglers convert when something else needs to touch them.
- **Breaking.** Response shapes change. At v0.0.0 this is the right moment, since the cost only grows.
- **Tracked as** [#334](https://github.com/hyperscaleav/omniglass/issues/334), following
  [#328](https://github.com/hyperscaleav/omniglass/issues/328).
