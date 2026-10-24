---
title: "Node tags (N2)"
date: 2026-07-19
refs: []
form: bullet
---

- **Node tags (N2).** A node becomes a taggable owner kind, alongside the install-wide tier / component / system /
  location. The `tag_binding` owner arc gains a `node_id` leg (an additive migration re-adding the two
  CHECK constraints; `ON DELETE CASCADE` so a node purge drops its bindings), and the governed
  `applies_to` set can now include `node`. A node is estate-wide, not a scope tree, so tagging it needs
  an all scope (`node:update`, reusing the identity gate) and its effective tags are the install-wide layer
  plus its own direct bindings, no inheritance. The generic per-entity tag routes grow the
  `/nodes/{name}:listTags` / `:setTag` / `:removeTag` methods, and the node body carries `effective_tags`.
  The console node blade gains a **Tags** panel (the shared `TagAdder`: read pills, add / remove in edit),
  and the node list a **Tags** column plus a per-key filter facet, the same shape as the component list.
  Proven by a storage integration test (the applies_to gate, the all-scope requirement, effective = install-wide
  + direct, unbind), a real-binary API test (bind / list / effective / unbind and the `node:update` 403),
  and the web suite. Follows [node identity](/architecture/nodes/) (N1); the [tags](/architecture/tags/)
  page stays `Partial`. Deferred: setting tags **during** node create (an inline-create platform concern).
