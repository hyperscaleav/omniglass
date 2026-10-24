---
title: "`location_type`, `secret_type`, `driver`, and `interface_type` take uuid primary keys"
date: 2026-07-22
refs: []
form: bullet
---

- **`location_type`, `secret_type`, `driver`, and `interface_type` take uuid primary keys** (slice 4 of
  [#262](https://github.com/hyperscaleav/omniglass/issues/262), [ADR-0062](/architecture/decisions/#adr-0062-a-registry-takes-a-uuid-primary-key-and-a-renameable-handle)):
  the last of the epic, closing the invariant that no foreign key anywhere points at a mutable string.
  `location_type`, `secret_type`, and `driver` are a **rename** of their kebab `id` to `name`;
  `interface_type` already called its slug `name` and only gains the uuid, as `property` did. Five inbound
  foreign keys move to the uuid: `location.location_type` and `location_type_property.location_type_id`,
  `secret.secret_type`, `product.driver_id`, and `interface.type`. Every read still projects the type's
  **name** (a scalar subquery aliased to the wire field), so the wire surface is unchanged while the
  storage key is a uuid, and the product body now carries a `driver` handle beside `driver_id` as it
  carries `vendor` beside `vendor_id`. `allowed_parent_types` stays a text array of type **names**, not a
  foreign key, since it is a self-reference the placement validator resolves by name. Proven by
  `TestRegistryHandleRenameKeepsReferences`, extended to rename all four under a live location, a
  location-type property contract, a secret, a product, and an interface, and assert every reference
  resolves and reads the new handle; **mutation-checked**. With this slice every registry is uuid-keyed;
  retiring the slug-key carve-out in the doctrine and the guard allow-lists is the epic's closing slice.
