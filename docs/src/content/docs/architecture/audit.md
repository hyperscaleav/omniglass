---
title: Audit
description: The who-did-what record, written once in the same transaction as the change it describes.
sidebar:
  badge:
    text: Partial
    variant: note
---

The audit log is how an operator answers "who changed this, and to what?" without trusting memory: every mutation is recorded once, at the source.

:::note[Partial]
Built today: the `audit_log` row written in the same transaction as every entity mutation, carrying the
resolved actor, verb, resource, and `old -> new` diff; the secret-decrypt audit (distinct `reveal` and
`copy` verbs); the auth-event lane; and the `GET /audit-log` read, which returns the `old`/`new` images,
with the console Admin > Audit page rendering each row's field-level before/after diff in a drawer.
See [implementation status](/architecture/status/).
:::

## The model

`audit_log` is **ground truth** (not derived): one row per mutation, carrying `actor`, `verb`,
`resource`, `resource_id`, and the `old -> new` diff.

- **Write-time mandatory.** Every API write emits one `audit_log` in the **same transaction**
  as the data write, a storage-layer responsibility, not per-handler discipline, so it cannot
  be forgotten or bypassed.
- **The actor** is resolved by IAM ([identity and access](/architecture/identity-access/)): the
  human, service, or node. On the read side a **human** actor is additionally resolved to a username;
  a service or node actor surfaces as its principal id.
- **An AI-accepted suggestion is one row.** An AI tool acts via OAuth as a `human` or `service`
  principal, so the actor is **that principal**, attributed and audited like any caller; the AI-sourced
  marking rides alongside the row ([AI](/architecture/ai/)).

:::design[The backtest and reconcile consumers, tracked in #526]
- **Ground truth a backtest reads.** Operator-driven transitions and config changes are not
  recomputable from collected data, so the audit log is what a rule backtest reads for them: alarm ack and
  snooze ([alarms and actions](/architecture/alarms-actions/)), and every config change a
  reconcile consumes.
:::

## Reads

- **Secret decrypts are always audited and never filterable.** Every read of secret material
  emits an `audit_log` (a credential decrypt), and that subset cannot be filtered away.
- **Other reads are not audited at the storage layer.**

:::design[The read-audit toggle, tracked in #526]
Optional read-audit is config-driven at the API layer (per-resource opt-in or a verbosity setting),
off by default.
:::

:::caution[Open question]
The read-audit granularity: per-resource opt-in versus a global verbosity setting.
:::

## Reading the trail

The read surface is `GET /audit-log`, gated by the admin-sensitive `audit:read:admin` (which a two-token
wildcard cannot reach, so only admin and owner see the security trail). It returns rows newest first,
filterable by `resource` and `verb`, and pages backward with a `before` timestamp plus a `limit`
(default 100, capped at 500). Each event also carries the `old` and `new` row images the write
recorded, completing the "and to what?" half of the question: a create has only `new`, a delete only
`old`, an update both, and auth events neither. Redaction is owned by the write side (sealed secret
material and credential hashes are never written into an image), so the read passes the images
through verbatim. The console renders the trail as the Admin > Audit page, where a row opens into a
drawer with the field-level before/after diff.

Beside the in-transaction estate lane there is a **second write lane for auth events**. Login and logout
run on read / no-transaction paths, so they emit through a standalone non-transactional seam
(`WriteAuthEvent`), recorded under `resource = 'auth'` with the verbs `login`, `logout`, `login_failed`
(a wrong password on a real account), `login_denied` (a correct password on a disabled account),
`login_locked` (an attempt inside the lockout window), and `revoke_session` (an admin ending another
principal's session). An impersonated action records **both** actors: `actor_principal_id` is the
impersonated principal and `real_actor_principal_id` the admin behind it, and both usernames are
**denormalized onto the row** (`actor_username`, `real_actor_username`) with the foreign keys going
`ON DELETE SET NULL`, so the trail stays attributable even after its actor is purged
([ADR-0016](/architecture/decisions/#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it)).

## Retention and integrity

Audit carries the **longest retention** of any ground-truth log (compliance). It is append-only by
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

- **Backtest**: a rule backtest reads operator transitions and config changes from here, since they are not recomputable.
- **Reconcile**: config changes arrive as `audit_log` rows, so reconcile reacts to them.
- **The alarm projection**: ack and snooze come from audit.
:::

