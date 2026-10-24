---
title: "Dev experience: an example estate on `make dev`"
date: 2026-07-07
refs: []
form: bullet
---

- **Dev experience: an example estate on `make dev`.** Distinct from the three production seeding concerns
  the [storage](/architecture/storage/) page names (schema migrations, boot seed, one-time backfills), a
  **dev-only example seed**, applied by `make dev` and never in production. `internal/devseed` embeds a
  fixture (a multi-site estate of three campuses, `hq` / `east` / `airport`, three sign-in-able users, and
  their grants spanning `operator@all`, `viewer@hq` subtree, and `deploy@east` subtree-excl-root) and
  installs it through the [Storage Gateway](/architecture/storage/) on the same trusted direct-DB lane as
  `bootstrap`, exposed as `omniglass seed-dev`. It is idempotent (a re-run of `make dev` changes nothing:
  locations are skipped by name, users by their taken-username error), so a fresh console comes up populated
  instead of empty and nobody hand-creates rows to demo a feature. Proven by a pure fixture-shape unit test
  and a testcontainer integration test (run twice: thirteen locations across three campuses, three users with
  password credentials, three grants of the expected scope, all stable across the second run), plus the
  `seed-dev` command driven end-to-end against a real Postgres. The `/ship-slice` gate now requires any slice that adds a new operator entity to seed an
  example of it here.
