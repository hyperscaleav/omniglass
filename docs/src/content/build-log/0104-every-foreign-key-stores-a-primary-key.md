---
title: "Every foreign key stores a primary key"
date: 2026-07-22
refs: []
form: bullet
---

- **Every foreign key stores a primary key** ([ADR-0056](/architecture/decisions/#adr-0056-every-foreign-key-stores-a-primary-key)):
  all 30 name-keyed foreign keys convert to the target's primary key (a uuid, `principal_id` for a node),
  reversing the direction ADR-0053 set and ADR-0055 completed. `on update cascade` is gone with them: it
  was write amplification across every referencing row to protect a key that did not need to be a name.
  The conversion also fixed a refusal, not just an inefficiency: `interface.component` referenced
  `component (name)` with **no** `on update` clause, so renaming a component that owned any interface
  failed outright with a foreign-key violation. Verified against the restored pre-conversion schema
  rather than argued. In exchange the API accepts **either** form wherever a reference is written (uuid
  tried first) and every response carries both the name and the id. Shipped in five slices grouped by
  subsystem so each file changed once; the last converts the collection tier (`metric_datapoint`'s four
  arcs, `interface`'s component and node arcs, `node.location_name`) and every remaining node reference.
  Proven by `TestCollectionReferencesSurviveARename`, which renames a component and a location under a
  live interface, task, and datapoint and asserts the node's resolved worklist and the component's
  interface list still come back; **mutation-checked** in both directions. Health keeps names internally
  on purpose, since its advisory lock hashes `health/<kind>/<name>` and a mixed currency would silently
  stop serializing.
