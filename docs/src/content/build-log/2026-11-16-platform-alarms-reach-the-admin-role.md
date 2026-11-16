---
title: "Platform alarms reach the admin role by default (#1987)"
date: 2026-11-16
refs: [1987, 2016, 2017]
---

A platform alarm used to open on the console and tell nobody until an operator wrote an action rule for
it. A notification target can now name a **role**: everyone holding it, directly or through a group
grant, read at each transition and filtered by each person's own scope, told once however many ways they
hold it. The boot seed ships `platform-admins` (the `admin` role, inbox and email) and
`platform-alarms-to-admins` (`when: platform`, a new action-site binding true on Omniglass's own parts).
The seed puts its rows back on every boot but leaves `enabled` alone, so a disable survives a restart, and
targets an operator adds to the rule stay. Integration tests grant and revoke `admin` around two platform
alarms, reach a person holding it through a group once, check a scope-limited holder hears nothing outside
their scope, and boot the seed twice around a disable. The health page's email line is #2016 and the
console create form's role picker is #2017 ([ADR-1987](/architecture/decisions/)).
