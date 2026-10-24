---
title: "The migration chain is collapsed to a single init"
date: 2026-07-23
refs: []
form: bullet
---

- **The migration chain is collapsed to a single init.** The 69 per-slice migrations that carried the
  schema from the first commit through the uuid-key epic are squashed into one `db/migrations` file that
  reproduces the current schema exactly. Verified by round trip: a fresh apply of the full chain and a fresh
  apply of the lone init produce a **byte-identical** `pg_dump`. It is pure DDL (no seed rows, which a squash
  drops), reuses the original initial migration's version so an existing database treats it as already
  applied, and carries a real `down` that drops every table. The workflow is unchanged going forward: the
  next schema change is still a new migration on top, never an edit to this one. One migration-history test
  that rolled the database back to a now-absent version was removed; the resolution behaviour it checked is
  covered by the cascade suite. (The stale constraint names the renames left, `component_make_*` on `vendor`
  and so on, are preserved faithfully by the squash; renaming them is a separate cosmetic pass.)
