---
title: "The node becomes the outpost, everywhere (#2059)"
date: 2026-11-17
refs: [2059, 2031, 2028]
---

The edge process that collects and controls is an outpost (ADR-2031), and now every surface says so,
with no alias left behind. One migration renames the schema (`outpost`, `outpost_log`, every
`node_id` column, constraint and index, the `node` owner and principal kinds, role permission nouns,
and the seeded `omniglass-outpost` and `outpost-silent` rows, keeping their ids); `endpoint.node_name`,
which always held the outpost's principal id, becomes `endpoint.outpost_id`.
`internal/storage/outpost_rename_migration_test.go` plants a node and everything it owns below the
migration, migrates forward over it, and rolls it back. The Go, the API (`/outposts`, the
`outpost:*` permissions), the generated CLI (`omniglass outpost run`, `OMNIGLASS_OUTPOST_*`), the
console, the lab (`--no-outpost`) and the docs follow; the architecture page moves to
`/architecture/outposts/` with a redirect. Home's tile that counts them, which read Collectors (a
word ADR-2031 rejected), reads Outposts. The vocabulary denylist retires `node` as the collector
while allowing its tree, DOM and Node.js senses, so a stray one fails the lint.
