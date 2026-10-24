---
title: "Node decommission (N3)"
date: 2026-07-19
refs: []
form: bullet
---

- **Node decommission (N3).** The node blade gets its destructive action: `DeleteNode` +
  `DELETE /nodes/{name}` (gated a new `node:delete`, covered by the owner `node:*`). It is a
  hard delete of the node's `kind='node'` principal, which cascades the node detail and, through
  it, its interfaces and their derived tasks, its node-owned tags and self-telemetry, and its
  enrollment credential, every referencing FK is already `ON DELETE CASCADE`, so no migration. A
  node is estate-wide, so it needs an all scope; the delete is audited, and the component
  telemetry the node collected (owner arc = component, `node_id` null) survives untouched. The
  console blade adds a **Delete** action (confirm, then close and refresh), completing its
  cred-action set (Edit primary, Enroll / Re-enroll kebab, Delete destructive). Proven by a
  storage test (the cascade of interfaces / tasks / tags, the component datapoint surviving, the
  all-scope gate) and a real-binary API test (the DELETE and the `node:delete` 403). Closes the
  node-lifecycle arc (N1 identity, N2 tags, N3 delete). The [nodes](/architecture/nodes/) page
  stays `Partial`.
