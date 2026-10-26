---
title: "System role staffing keeps its integrity (#504)"
date: 2026-10-26
refs: [504, 503, 505, 506, 508, 510]
---

Four defects in role staffing, each fixed test-first against a real Postgres. A membership or a role
assignment now costs a write on both of its ends: the five routes that bind, unbind, re-default or
staff a component need `component:update` over the component beside `system:update` over the
system, each end resolved in its own tier's scope, so a principal scoped to one system can no longer
bind a component it cannot read and inject system-band configuration onto it through the cascade
([ADR-0510](/architecture/decisions/#adr-0510-a-membership-or-a-role-assignment-costs-a-write-on-both-of-its-ends)).
A structural guard finds every write whose path names an entity of another tier and requires it to
publish that tier's permission and be driven by a scoped principal in the staffing test, so the next
cross-tier route cannot ship unfenced.

A role name means one role in a system: a system role that would shadow its standard's, a standard
role a conforming system already declares, and a move onto such a standard are 409s naming both
owners, and a collision written before the rule is refused rather than resolved to whichever row
came back first
([ADR-0503](/architecture/decisions/#adr-0503-a-role-name-addresses-one-role-per-system)). Changing a
system's standard carries its staffing onto the new standard's roles of the same names, under the
typed-slot guard and the new capacity, and releases the rest as members with no role, so no
component is ever wedged; a backfill releases the strands earlier releases left
([ADR-0506](/architecture/decisions/#adr-0506-changing-a-systems-standard-carries-its-staffing-onto-the-roles-that-still-exist)).
The role seed lands whole and repairs a set an interrupted install left missing
([ADR-0508](/architecture/decisions/#adr-0508-the-boot-seed-installs-a-shipped-role-whole-and-repairs-a-set-an-unrevised-role-is-missing)).
The epic's fifth defect, an unknown standard landing an orphan role (#505), was already closed by
#647's owner-arc check; its read-side residue goes with this slice, so listing an unknown standard's
roles is a 404 rather than an empty list. The `role-staffing` demo moves a staffed room between
standards and refuses a scoped technician.
