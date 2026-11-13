---
title: "Nodes sit under Admin"
date: 2026-11-11
status: "Accepted"
summary: "The Nodes sidebar entry moves from the top level into the Admin group, beside Status (Platform health), reversing the UI page's rule that a node keeps its own entry rather than sitting in Admin. Setting up a node is an admin's job; the gate stays node:read plus ABAC scope"
refs: [1767, 1644, 798]
---

- **Date:** 2026-11-11 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/)
- **Decision:** (#1767.) Nodes is a child of the Admin group, ordered Users, Roles, Groups, Audit, Nodes, Status, Settings. Its route (`/nodes`), its `node:read` gate and its page are unchanged; only the door moves.
- **Consequences:** The top level of the sidebar holds fleet work only. A `node:read` holder without the admin tier sees an Admin group holding Nodes, Status and Settings, as a `*:read` viewer did for Status alone before. Platform health's per-node links into Nodes are unchanged.
- **Context:** The UI page held that a node keeps its own sidebar entry because nodes are monitored and scope-controlled. Moving Platform health under Admin as Status (#1767) left Nodes the only infrastructure door on the top level, and the architect ruled that enrolling and tending nodes is administration.
- **Not chosen:**
  - **Keep Nodes top-level.** It is not fleet inventory (Explore holds that), and its daily reader is the admin who enrolls nodes, so a top-level door spends rail space on an admin task.
  - **Fold Nodes into Status.** Status reads whether the platform is well; Nodes is where a node is enrolled and edited. One page doing both blurs a read surface into a write surface.
