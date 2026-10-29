---
title: "A node's own logs can keep their own window (#1350)"
date: 2026-10-29
refs: [1350, 417, 1265, 1351]
---

A node's own log lines, until now kept by the same four severity windows as a component's, can be
kept for windows of their own: `node_log_debug_days`, `node_log_info_days`, `node_log_warning_days`
and `node_log_error_days` in the retention settings. Each is unset by default, and while it is unset
nothing changes: a node's lines follow the shared window. Set one and the retention pass prunes that
band of the node's lines by it, and a component's still by the shared window.

These are the first settings with no default. The Settings page used to list only the keys the
effective document resolved, so a setting with no default would never have appeared there; it now
lists every key the schema declares, and an unset one reads **not set** until an admin saves a value.

A per-node window, so one node under investigation keeps its logs longer, waits for the cascade to
gain a node leaf (#1351).
