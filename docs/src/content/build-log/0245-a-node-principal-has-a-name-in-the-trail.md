---
title: "A node principal has a name in the trail"
date: 2026-08-14
refs: []
form: bullet
---

- **A node principal has a name in the trail**
  ([#738](https://github.com/hyperscaleav/omniglass/issues/738)). The gateway's identifier
  resolution had a human arm and a service arm and no node arm, so a `kind=node` principal resolved
  to the empty string everywhere a principal is named: the audit actor column, the alarm
  acknowledgement, the group roster. Preserved behaviour rather than introduced, and deliberately
  so: the dropped `principal_label(uuid)` was `coalesce(human.username, service.label)` with no node
  arm either, and #564 kept the shape so a refactor about WHERE the answer lives changed nothing
  about what it says. What that preserved was a blank actor on an audit row.

  **The node's identifier is its `name`**, on the same reasoning that made the other two columns
  identifiers: it is the only operator-visible handle the row has, it is unique (`node_name_key`,
  beside `human_username_key` and `service_name_key`), and it is already the estate address and the
  NATS subject token, so it is the string an operator would recognise in a trail.
  `node.display_name` cannot be one, being optional and not unique, and this string is denormalized
  as bare text into `audit_log` where nothing survives beside it to disambiguate a duplicate.

  Adding it is one row in `principalIdentSources`, because that list is the only place in the tree
  that names these tables and columns and every shape is rendered from it. The three folds that read
  those shapes each grew a column, which is the one thing the variadic resolution cannot catch for
  them, so the test grew to match: the invariant test drives the node arm to a real answer instead
  of to the empty string, and a new test drives all three kinds through the audit write, the audit
  read, the alarm read and the group roster, since a fold reading one column fewer than its query
  returned is a defect only a surface can show. Still not reachable in a shipped estate (nothing
  seeds a node principal with a grant), which is why it was worth fixing before something did.
