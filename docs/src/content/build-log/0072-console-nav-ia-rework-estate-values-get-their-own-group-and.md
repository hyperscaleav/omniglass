---
title: "Console: nav IA rework, estate values get their own group and Settings becomes Admin"
date: 2026-07-14
refs: []
form: bullet
---

- **Console: nav IA rework, estate values get their own group and Settings becomes Admin.** The
  sidebar's `nav.ts` gains a new **Values** top-level group (Variables, Secrets, Config), the
  estate-attached values that used to sit under Settings, standing beside **Inventory** (Components,
  Systems, Locations, and **Nodes**, the collection daemons) rather than nested inside it. Interfaces
  and Tasks are dropped from the nav: an interface is a panel on a component and a task is a panel on
  a node, both future work, not directories of their own. The Settings group is renamed **Admin**
  (Users, Roles, Groups, Audit) and gains an ungated **soon** Settings leaf reserving the
  platform-preferences page (severity scales, schedules, retention, defaults)
  ([#222](https://github.com/hyperscaleav/omniglass/issues/222)). Routes stay flat and unchanged
  (`/secrets`, `/variables`, `/config`, `/nodes`, the new `/settings` stub), so only the grouping and
  labels move; this supersedes the earlier same-day plan to move those values into Catalog
  ([decision log](/architecture/decisions/)). Proven by `nav.test.ts` (the Inventory and Values group
  order and labels, Admin's renamed label and its soon-stub surviving for both an owner and a bare
  `*:read` viewer, the moved entries keeping their existing gates) and a `Sidebar.test.tsx` render
  test (the Inventory and Values groups actually render in the expanded submenu).
