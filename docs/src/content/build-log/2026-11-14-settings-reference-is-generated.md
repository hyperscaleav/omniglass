---
title: "The settings reference is generated from the settings schema (#1342)"
date: 2026-11-14
refs: [1342, 417]
---

Every settings default, range and doc string used to live twice: once as a struct tag in
`internal/settings/schema.go` and again, typed by hand, in the admin guide and the storage page (the
retention windows). `settings.FactsJSON()` now walks the struct and `make gen` writes
`docs/src/generated/settings.json` through `cmd/settingsgen`, one row per key with its namespace,
JSON type, default, enum, pattern, range, doc, domain and client visibility, in declaration order.
`SettingsTable.astro` renders it: the settings page carries every namespace, the admin guide the
retention table, and the storage page links instead of restating the numbers. A unit test pins the row
shape per tag kind (enum, pattern, range, list default, `nullable:"false"`, no default), and
`TestSettingsFactsMatchesCommitted` fails when a tag changes without a regenerate.
