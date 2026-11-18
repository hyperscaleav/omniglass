---
title: "The registry becomes the catalog, and three labels name what they list (#2070)"
date: 2026-11-17
refs: [2070, 2035, 2028]
---

The database rows the fleet is typed by are the catalog (ADR-2035), and now every surface says so.
One migration renames `registry_shadow` to `catalog_shadow` (its `registry` column and constraints
with it); `internal/storage/catalog_shadow_rename_migration_test.go` plants rows below the migration,
migrates forward over them, and rolls back. The Go follows (`typecatalog.go`, `catalogs.go`,
`catalog_rename.go`, `collection.Catalog`, the bus's catalog cache), the API's descriptions say "the
built-in transports" where they said "the code registry", and the generated OpenAPI, CLI help, MCP
catalog and console client regenerate over the new words. In the console, **Rules** becomes **Event
rules** and moves from `/rules` to `/event-rules`, **Severity** becomes **Severity levels**, and
**Targets** becomes **Notification targets**. The vocabulary denylist retires registry in the
catalog sense, allowing the compiled-in lookups that keep it (the transport registry, the expression
diagnostics, the simulator's sessions, the env and settings namespace registries, the console's
blade registry, container and npm registries), so a stray one fails the lint.
