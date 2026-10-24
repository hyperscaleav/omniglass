---
title: "`role` (IAM) takes a uuid primary key"
date: 2026-07-23
refs: []
form: bullet
---

- **`role` (IAM) takes a uuid primary key**, the last named entity still keyed by its slug. It gains a uuid
  `id` and demotes the slug to a unique, renameable `name`, and its one inbound reference,
  `principal_grant.role_id`, moves to the uuid. The RBAC engine is unchanged: it keys roles by name and
  expands `inherits` by name, so the well-known handles (`owner` / `viewer` / `operator` / `deploy` /
  `admin`) stay stable and the storage layer resolves `role_id` to a name on read and a name (or uuid) to
  the uuid on write. The owner invariant, previously holding the literal slug `'owner'` in its trigger,
  now resolves owner by name (`(select id from role where name = 'owner')`), and the roles view and grant
  bodies carry the role's name beside the uuid. With this every named entity in the schema is uuid-keyed;
  the only non-uuid identities left are deliberate (`node.principal_id`, the polymorphic `scope_id` /
  `resource_id`, content-addressed `blob` / `task`).
