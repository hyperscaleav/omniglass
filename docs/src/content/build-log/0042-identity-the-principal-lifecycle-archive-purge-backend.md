---
title: "Identity: the principal lifecycle (archive + purge), backend"
date: 2026-07-09
refs: []
form: bullet
---

- **Identity: the principal lifecycle (archive + purge), backend.** Beyond the reversible disable, a principal
  can be **archived** (a soft delete, `archived_at`: hidden from the directory, cannot authenticate,
  reversible) and then **purged** (an irreversible hard delete, gated on prior archival and on the
  admin-sensitive `principal:purge:admin`, so admin and owner can purge but a two-token `principal:*` cannot
  reach it). The purge preserves the audit trail: every `audit_log` row now denormalizes the actor's label at
  write time, the audit foreign keys are `ON DELETE SET NULL`, and the read coalesces the live join to the
  snapshot, so a purged actor's history stays legible ([ADR-0016](/architecture/decisions/#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it),
  retiring the "never hard-deleted" rule). The last-active-owner guard extends to archive. Gateway
  (`ArchivePrincipal` / `RestorePrincipal` / `PurgePrincipal`), the `:archive` / `:restore` /
  `:purge` API custom methods, and the generated CLI / client ship; proven against real Postgres by a storage
  integration test (lifecycle + audit-actor survival + owner guard) and an HTTP end-to-end test (the capability
  split, the archive-before-purge gate, soft-delete visibility). The console UI for the lifecycle is a
  follow-up ([#143](https://github.com/hyperscaleav/omniglass/issues/143) backend).
