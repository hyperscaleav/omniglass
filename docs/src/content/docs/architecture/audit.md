---
title: Audit
description: The who-did-what record, written once in the same transaction as the change it describes.
sidebar:
  badge:
    text: Partial
    variant: note
---

The audit log answers "who changed this, and to what?": every mutation is recorded once, at the source.

:::note[Partial]
Built today: the `audit_log` row written in the same transaction as every entity mutation; the
secret-decrypt audit (distinct `reveal` and `copy` verbs); the auth-event lane; and the
`GET /audit-log` read, rendered by the console Admin > Audit page with a per-row before/after diff
drawer. See [implementation status](/architecture/status/).
:::

## The model

`audit_log` is **ground truth** (not derived): one row per mutation, carrying `actor`, `verb`,
`resource`, `resource_id`, and the `old -> new` diff.

- **Write-time mandatory.** Every API write emits one `audit_log` in the **same transaction**
  as the data write, a storage-layer responsibility, so it cannot be forgotten or bypassed.
- **`resource_id` is the primary key, never the name.** A named entity is addressable two ways
  ([ADR-0062](/architecture/decisions/)), by its uuid or by its renameable handle, so recording
  whichever reference the caller happened to use would give one entity two audit keys and orphan
  every row keyed on a name the moment somebody renamed it. The name at the time of the action is
  not lost: it is in the `old` and `new` images, which is a point-in-time snapshot rather than a
  lookup key, exactly as `actor_username` is snapshotted beside `actor_principal_id`. A guard reads
  every `writeAuditRes` call site and fails on an argument that is not primary-key **shaped**, which
  catches the two ways this went wrong (a name, and a dual-accept route's reference) without being
  able to prove that a given uuid is the right table's. The remaining gap is narrow and known: a
  `credential` row is keyed on its principal's uuid rather than its own.
- **The actor** is resolved by IAM ([identity and access](/architecture/identity-access/)): the
  human, service, or node. The read resolves it to the actor's **identifier**, a human's username
  or a service account's name, through the gateway's one resolution
  ([ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions)),
  falling back to the snapshot on the row once that principal is purged. A **node** actor is the
  gap the resolution does not cover and reads empty, which is inherited behaviour and not a design;
  the principal id is on the row either way. Nothing here surfaces a raw uuid where a name was
  expected.
- **An AI-accepted suggestion is one row.** An AI tool acts via OAuth as a `human` or `service`
  principal, so the actor is that principal; the AI-sourced marking rides alongside the row
  ([AI](/architecture/ai/)).

:::design[The backtest and reconcile consumers, tracked in #526]
- **Ground truth a backtest reads.** Operator-driven transitions and config changes are not
  recomputable from collected data, so the [consumers below](#who-consumes-it) read them from here
  ([alarms and actions](/architecture/alarms-actions/)).
:::

## Reads

- **Secret decrypts are always audited and never filterable.** Every read of secret material
  emits an `audit_log` row (a credential decrypt), and that subset cannot be filtered away.
- **Other reads are not audited at the storage layer.**

:::design[The read-audit toggle, tracked in #526]
Optional read-audit is config-driven at the API layer (per-resource opt-in or a verbosity setting),
off by default.
:::

:::caution[Open question]
The read-audit granularity: per-resource opt-in versus a global verbosity setting.
:::

## Reading the trail

`GET /audit-log` is gated by the admin-sensitive `audit:read:admin`, out of a two-token wildcard's
reach, so only admin and owner see the security trail. Rows return newest first, filterable by
`resource` and `verb`, paged backward with `before` plus `limit` (default 100, capped at 500), each
carrying the `old` and `new` images the write recorded: a create only `new`, a delete only `old`, an
update both, auth events neither. Redaction is the write side's (sealed secret material and
credential hashes never enter an image); the read passes images through verbatim.

Auth events ride a **second write lane**: login and logout run on read / no-transaction paths, so
they emit through a standalone non-transactional seam (`WriteAuthEvent`) under `resource = 'auth'`,
with the verbs `login`, `logout`, `login_failed` (a wrong password on a real account),
`login_denied` (a correct password on a disabled account), `login_locked` (an attempt inside the
lockout window), and `revoke_session` (an admin ending another principal's session). An impersonated
action records **both** actors (`actor_principal_id` the impersonated principal,
`real_actor_principal_id` the admin behind it); both identifiers are **denormalized onto the row**
(`actor_username`, `real_actor_username`) with the foreign keys going `ON DELETE SET NULL`, so the
trail stays attributable after its actor is purged
([ADR-0016](/architecture/decisions/#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it)).
What gets written there is the actor's **identifier**, a human's username or a service account's
name, and the platform's answer to which one belongs to the gateway rather than to the schema: the
stored function that used to resolve it retired, so the order is stated once in Go and rendered into
the statements the gateway binds
([ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions)).

## Retention and integrity

Audit carries the **longest retention** of any ground-truth log (compliance) and is append-only by
construction.

:::design[Retention partitioning, tracked in #526]
Retention is enforced by time-partitioning `audit_log`: aging out a window drops a partition, never a
row-by-row delete.
:::

:::caution[Open question]
Tamper-evidence (a hash-chain or signed audit) for high-assurance deployments.
:::

:::design[The backtest, reconcile, and alarm-projection consumers, tracked in #526]
## Who consumes it

- **Backtest**: operator transitions and config changes, not recomputable elsewhere.
- **Reconcile**: config changes arrive as `audit_log` rows.
- **The alarm projection**: ack and snooze come from audit.
:::
