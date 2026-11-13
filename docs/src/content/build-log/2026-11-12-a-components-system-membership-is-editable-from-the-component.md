---
title: "A component's system membership is editable from the component (#767)"
date: 2026-11-12
refs: [767, 1203]
---

The component create form offers a System; its edit face did not, so a binding made at create had no
path back for the operator even though the member routes existed. The edit face now carries a
**Memberships** field: the primary system as a picker over the systems the caller's `system:update`
reaches, and the other memberships as removable chips. Save runs the add, `:setPrimary` and remove
calls in that order (`planMembershipChanges` in `web/src/lib/members.ts`), refreshes the component's
memberships and every system's member list, and shows a refused write inline. The set of bindable
systems is one function now (`bindableSystemNodes`), shared with the create form. No API or schema
change; membership stays its own verb rather than a field on the component's update or move.
