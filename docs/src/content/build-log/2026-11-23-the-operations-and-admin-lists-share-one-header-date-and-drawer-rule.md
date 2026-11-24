---
title: "The operations and admin lists share one header, date format and drawer rule (#2447)"
date: 2026-11-23
refs: [2447, 2511, 2506]
---

Alarms, outposts, variables, secrets, users, roles, user groups, audit and files now open with the
`Page` header (title and one-line hint from the nav metadata). One formatter module owns every date:
a list shows the relative age with the absolute time and exact instant on hover (the `Time`
component), and a guard test forbids the `toLocale*` calls anywhere else. The audit change table
humanizes field names ("LastOrphanedAt" reads "Last orphaned at", the recorded key on hover) and
shows instants as dates. Filter placeholders share one casing. The drawer width rule (wide when the
body carries a table or a timeline) and the date rule are stated in the design system. Roles says in
its hint that the roles are built in and keeps the filter bar's height; an empty user group list
teaches and offers New user group; secrets show the type by label with the name beneath it in the
list, the drawer and the form, and the drawer shows the secret's own name; the profile lists role
grants and groups as rows and spells the `>` wildcard as "Every permission"; file rows carry the
open chevron. The simulation drawer (item 9) was rebuilt by #2510 and is not part of this change.
