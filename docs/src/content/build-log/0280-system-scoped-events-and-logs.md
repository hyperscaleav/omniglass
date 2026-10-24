---
title: "System-scoped events and logs"
date: 2026-08-26
refs: []
form: bullet
---

- **System-scoped events and logs**
  ([#793](https://github.com/hyperscaleav/omniglass/issues/793)). Two new reads:
  `GET /systems/{name}/events` (the system's own events and its members', newest first,
  each row labeled by owner) and `GET /systems/{name}/logs` (the members' raw lines
  merged, each naming its writer), both windowed and capped like the component reads,
  gated by `system:read` with the non-disclosing resolve. The workspace grows its Events
  and Logs tabs over them. A wire lesson worth keeping: Huma does not flatten an embedded
  struct into the schema, so the system bodies spell their fields out rather than
  embedding the component bodies.
