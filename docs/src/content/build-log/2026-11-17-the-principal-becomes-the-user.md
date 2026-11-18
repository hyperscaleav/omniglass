---
title: "The principal becomes the user, a person or a machine (#2075)"
date: 2026-11-17
refs: [2075, 2036, 2028]
---

The IAM subject is a user on every surface (ADR-2036). One migration renames `principal` to `users`
(plural, since `user` is reserved in Postgres), `human` to `person`, `service` to `machine`, the
group, grant and member tables to `user_group`, `user_grant` and `user_group_member`, and every
column, constraint, index and trigger that spelled principal; the kind check narrows to `person`
and `machine`, and an outpost's user is a machine user its `outpost` row points at.
`internal/storage/user_rename_migration_test.go` plants rows below the migration,
migrates forward over them, and rolls back. The API follows (`/users`, `/user-groups`,
`/user-groups/{group}/members/{user}`, the `user:*` and `user_group:*` permissions, and an
`/auth/me` that answers `user`, `person` and `machine`), and the generated OpenAPI, CLI
(`omniglass user`, `omniglass user-group`, `--user-id`, `--user-group`), MCP catalog and console
client regenerate over the new words. Notification targets name `user` and `user_group`, and the
audit trail records `user` and `user_group` from here on. The vocabulary denylist retires
principal, principal group, service account and human as a kind, so a stray one fails the lint.
