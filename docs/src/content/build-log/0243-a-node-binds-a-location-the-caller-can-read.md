---
title: "A node binds a location the caller can read"
date: 2026-08-14
refs: []
form: bullet
---

- **A node binds a location the caller can read**
  ([#705](https://github.com/hyperscaleav/omniglass/issues/705)). `CreateNode` and `UpdateNode`
  resolved their optional `location` scope-blind, the shape
  [#700](https://github.com/hyperscaleav/omniglass/issues/700) closed on the component and system
  create and move paths and left here to keep that slice's diff honest to its own issue. They now
  take the caller's `location:read` set and resolve through `resolvePlacementRef`, the seam #700
  built, with the API injecting that scope exactly as the system and component routes already do.

  The node tier reaches the fix by a different argument than the other four, and saying which one
  matters. #700 was urgent because a create stamps a label rendered from the referenced row's, so an
  existence-only bind turned a create into a disclosure channel for a row the caller could not read.
  No node label rule exists, so nothing here is stamped and nothing handed back. What is left is the
  invariant this repository states as an invariant: ABAC scope is injected by the Storage Gateway on
  every applicable query, and a caller-supplied reference resolved without it is an exception to that
  rather than a carve-out anybody decided on. Out of scope answers the same non-disclosing
  `ErrLocationNotFound` an absent location gives, because a refusal that told them apart would
  confirm the row exists one level up.

  The regression test drives the gateway seam rather than the wire, and the reason is a fact about
  the shipped role set rather than a shortcut. `node:create` resolves only from an `all` grant
  (`applicableKinds` admits no tier for a node), and every seeded role carrying `node:*` inherits the
  viewer read floor, so no principal that can create a node today has a narrow `location:read`. The
  seam is where the two sets are separate arguments and where the invariant is written, and a custom
  role with `node:create` and no read floor is a shape the platform admits. The owner runs the
  identical body in the same test, so the refusal is proven to be a scope boundary rather than a
  broken write path, and the out-of-scope refusal is compared against a genuinely absent location's.
