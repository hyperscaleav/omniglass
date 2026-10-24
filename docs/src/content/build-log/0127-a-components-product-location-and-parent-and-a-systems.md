---
title: "A component's product, location, and parent, and a system's location and parent, become patchable ([ADR-0064](/architecture/decisions/#adr-0064-placement-and-classification-are-mutable-after-create))"
date: 2026-07-26
refs: []
form: bullet
---

- **A component's product, location, and parent, and a system's location and parent, become patchable
  ([ADR-0064](/architecture/decisions/#adr-0064-placement-and-classification-are-mutable-after-create)).**
  These fields were accepted on create but had no update path, so a mis-classified or physically moved
  entity could only be deleted and recreated, losing its telemetry history. `PATCH /components/{name}`
  and `PATCH /systems/{name}` now take them on the house three-state (omitted unchanged, empty string
  clears, name sets); a reparent is cycle-guarded and scope-injected like a location move, and a product
  swap keeps hand-set property values while the new contract's defaults follow. No new UI: this opens the
  API and CLI and unblocks the CRUD form primitive's generated edit form. Closes
  [#342](https://github.com/hyperscaleav/omniglass/issues/342).
