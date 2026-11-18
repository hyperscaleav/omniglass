---
title: "Role retires from the system sense; system slot everywhere (#2061)"
date: 2026-11-17
refs: [2061, 2032, 2028]
---

ADR-2032 gave role back to IAM alone, and this sweep carries the other sense, what a system needs filled,
to system slot on every surface. One migration renames the schema (`system_slot`, `system_slot_assignment`,
`system_slot_type`, `system_slot_product`, `slot_choice`, and every `role_id` column, constraint and index
with them); the IAM `role` table is untouched. The routes are `/standards/{id}/slots/{slot}` and
`/systems/{name}/slots/{slot}` (assignments and `:swapPositions` included), so the CLI groups are
`omniglass standard slot` and `omniglass system slot`, the expression accessor is `$slot`, and the template,
health and simulation bodies carry `slots`. The storage methods are `ListSystemSlots`, `SetSystemSlot`,
`DeleteSystemSlot` and `AssignSlot` beside RBAC's `ListRoles` and `UpsertRole`. The console's Slots panel,
its "Ad hoc slots" group and the "no slot" member row follow, while the Admin Roles page, the operator and
viewer roles, `--role` grants and the agent role policy keep the word. The `declare-a-first-role`
walkthrough is `declare-a-first-slot`, the `role-staffing` tape is `slot-staffing`, and the docs denylist
bans system role, `$role`, the system and standard role routes, role choices and assignments, and effective
roles, with IAM role left alone. A test that replays the one-time stranded-assignment backfill reads it
against today's table names, since that migration ran before the rename.
