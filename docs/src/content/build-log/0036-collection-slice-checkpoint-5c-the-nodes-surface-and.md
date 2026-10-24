---
title: "Collection slice (checkpoint 5c): the Nodes surface and enrollment"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5c): the Nodes surface and enrollment.** The first authoring surface for the
  [node](/architecture/nodes/) tier. The console `/nodes` stub becomes a live **Nodes** inventory (the flat
  sibling of the tree lists, a config over the shared `FlatList`): a row per collection daemon with a
  liveness **status pill** derived client-side from `last_heartbeat_at` against the server's down window
  (`OMNIGLASS_NODE_DOWN_AFTER`, 90s: `up` within it, `down` once stale, `never` before first heartbeat), the
  relative last-heartbeat time, and a description, with name and status facets on the same FilterBar the other
  lists use. A row opens a detail Drawer (facts plus an Enroll / Re-enroll action), and **New node** creates a
  node then enrolls it (day-one: create, then `POST /nodes/{name}:enroll`). The minted **enrollment token is a
  secret shown once**: a modal reveals it in a monospace field with a copy-to-clipboard button and a clear
  "shown once, cannot be retrieved again" warning, and the token lives only for the modal's lifetime (cleared
  on close, never written to the query cache, `localStorage`, or a log). The nav tab is gated on `node:read`,
  create on `node:create`, and enroll on `node:enroll` via the same `can()` the sidebar and route guard read;
  the page renders only real `NodeBody` fields. No API or engine change (the node routes already exist), so
  `make gen` shows no drift. Proven by vitest: the data layer (list-envelope unwrap, create body, the `:enroll`
  custom-method POST, the pure status derivation and its window boundary, the status facet catalog) and the
  page (a row per node with the derived pill, the create affordance hidden without `node:create`, and the
  enrollment modal revealing the token, copying it to a faked clipboard, and clearing it on close). The Nodes
  page stays `Partial`: placement, assigned-task worklists, and node health beyond the heartbeat pill are still
  Design. Interfaces and Tasks authoring is checkpoint 5d.
