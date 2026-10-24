---
title: "Node identity and edit (N1)"
date: 2026-07-19
refs: []
form: bullet
---

- **Node identity and edit (N1).** A node reaches parity with the component/system/location
  primitives: it gains a nullable `display_name` and an optional `location` (a descriptive
  placement referencing `location(name)`, `ON DELETE SET NULL`, not a scope; a node stays
  estate-wide), added by an additive migration. `name` stays the immutable key and estate
  address. `UpdateNode` and `PATCH /nodes/{name}` (gated a new `node:update`, covered by the
  owner `node:*`) patch display_name, description, and location, with an unknown location a 422
  through the FK. The console blade becomes read-edit-save: the title is the display_name
  (falling back to the name), **Edit** is the primary action and Enroll / Re-enroll moves to the
  secondary kebab, the list row labels by display_name with the key and location as its subtitle,
  and the create Drawer carries the two identity fields (its enroll flow is unchanged). Proven by
  the storage round-trip (each patched field, name immutable, location set / clear, the
  `ON DELETE SET NULL`), the real-binary API test (the PATCH round-trip, the `node:update` 403,
  the unknown-location 422, create mints no token in the body), and the web suite (the Edit
  action gated on `node:update`, the editable fields, Re-enroll from the kebab, the labelled
  rows). Deferred to their own issues: the token-after-create flow (#287), node tags (#285),
  decommission (#286), and inline create for flat lists (a platform list-primitive concern). The
  [nodes](/architecture/nodes/) page stays `Partial`.
