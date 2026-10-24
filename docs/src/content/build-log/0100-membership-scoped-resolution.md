---
title: "Membership-scoped resolution"
date: 2026-07-21
refs: []
form: bullet
---

- **Membership-scoped resolution** and the end of the component's system pointer: the tag and variable
  cascades seed their system band from `system_member` rather than `component.system_id`, and tag
  resolution takes the system to resolve against (`GET /components/{name}/effective-tags?system=`),
  falling back to the component's **primary** membership when none is given. A shared device therefore
  answers **differently for each system it serves**, which the single pointer could not express at
  all. **Secrets carry no system band**, on ownership grounds: a credential belongs to the device, not
  to the room it serves. `component.system_id` is **dropped**; the component body now reports `system`
  (the primary, by name) and `system_count`, and the console reads both instead of joining uuids
  client-side. Proven by an integration suite written before the change, since a mis-seeded chain is
  valid SQL that silently returns fewer rows: the same component resolving `prod` for one system and
  `lab` for another, the context-free read following the primary and moving when the primary moves, a
  component in no system resolving the other bands without error, a system the component is not a
  member of lending it nothing, and a system-owned secret never reaching a component. Slice 2 of epic
  [#324](https://github.com/hyperscaleav/omniglass/issues/324)
  ([ADR-0052](/architecture/decisions/#adr-0052-the-cascade-resolves-through-membership-and-secrets-carry-no-system-band));
  [cascade](/architecture/cascade/) and [core entities](/architecture/core-entities/) stay `Partial`.
