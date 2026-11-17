---
title: "A cascade value is a binding, and a declared value is device state (#2079)"
date: 2026-11-17
refs: [2079, 2037, 2028]
---

The values words follow ADR-2037. A value set at one cascade level is a **binding**, and a deeper
binding **shadows** a shallower one; override stays only for the catalog's sparse edit (#1435). One
migration renames the settings table `setting_override` to `setting_binding` with its constraints and
indexes, and `internal/storage/setting_binding_migration_test.go` plants a binding below it, migrates
forward over it, and rolls back. The settings engine, Storage Gateway and API descriptions speak of
bindings, the Admin settings page names the console binding, and the tag combinator reads union on
key, shadow on value. Desired device state is a **declared value**: the variables page is retitled
"Variables, secrets, and declared values", the console's Values group names its stub Declared
values, and the vocabulary table gains the term, retiring declared config, desired state, desired
value, set the config, config drift and the cascade sense of override, so a stray one fails the lint.
