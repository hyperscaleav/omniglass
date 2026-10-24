---
title: "A principal can be purged, and the audit trail is denormalized to survive it"
date: 2026-07-09
status: "Accepted"
summary: "A principal can be hard-deleted (purge, gated on archival); the audit trail survives via a denormalized actor label and `ON DELETE SET NULL`, retiring the \"never hard-deleted\" rule (soft-delete verb: archive)"
---

- **Date:** 2026-07-09 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** A principal gains a full **lifecycle**: **disable** (reversible, the `active` flag),
  **archive** (a soft delete, `archived_at`, hidden from the directory and unable to authenticate,
  reversible), and **purge** (an irreversible hard delete of the row). Purge is **gated on prior archival**
  (archive-before-delete) and on the admin-sensitive `principal:purge:admin`, so `admin` (which carries it
  explicitly) and `owner` (`>`) can purge but a two-token `principal:*` cannot reach it
  ([ADR-0015](#adr-0015-permissions-are-topic-patterns-single-token-and-tail-wildcards)). To keep the audit
  trail through a hard delete, the actor's human-readable label is **denormalized** into every `audit_log` row
  at write time, and the audit foreign keys become `ON DELETE SET NULL`: a purge nulls the id link but the text
  survives, so "who did X" outlives the principal. The read side coalesces the live join to the snapshot.
- **Context:** [ADR-0006](#adr-0006-the-owner-invariant-is-enforced-by-bootstrap-for-now)'s single-owner invariant
  meant accounts were **disabled, never hard-deleted**, since audit rows referenced them (`RESTRICT`). But
  operators need to remove accounts created by mistake, a common task, without erasing history or orphaning the
  trail. Denormalizing the actor label decouples the audit record from the principal row, so the row can be
  purged while the history stays legible; the archive gate prevents an accidental one-click hard delete, and
  the last-active-owner guard (extended to archive) means a purgeable account is never the last owner. This
  retires the "never hard-deleted" statement in the identity-access page.
- **Naming:** the soft-delete verb was renamed **deactivate to archive** (and reactivate to **restore**) when
  the console UI landed ([#146](https://github.com/hyperscaleav/omniglass/issues/146)): "disable" and
  "deactivate" read as synonyms, blurring two distinct operations. The ladder is now a *suspend* (**disable**,
  reversible, still listed) then an *offboard* (**archive**, soft delete, hidden, recoverable) then a *destroy*
  (**purge**), so the labels read pause to remove to destroy, matching the industry suspend-vs-delete pair. The
  column, endpoints (`:archive` / `:restore`), capability (`principal:archive`), and list param
  (`include_archived`) all follow the verb.
- **Closes:** issue [#143](https://github.com/hyperscaleav/omniglass/issues/143) (backend),
  [#146](https://github.com/hyperscaleav/omniglass/issues/146) (console + rename).
