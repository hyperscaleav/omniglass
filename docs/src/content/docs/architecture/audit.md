---
title: Audit
description: The who-did-what record, written once in the same transaction as the change it describes.
sidebar:
  badge:
    text: Spec
    variant: caution
---

Leaf of the [architecture spine](/architecture/). The who-did-what record: every mutation
recorded once, at the source.

## The model

`audit_log` is **ground truth** (not derived): one row per mutation, carrying `actor`, `verb`,
`resource_kind`, `resource_id`, and the `old -> new` diff.

- **Write-time mandatory.** Every API write emits one `audit_log` in the **same transaction**
  as the data write, a storage-layer responsibility, not per-handler discipline, so it cannot
  be forgotten or bypassed.
- **The actor** is resolved by IAM ([identity and access](/architecture/identity-access/)): the
  user, service account, or node.
- **Ground truth and replay source.** Operator-driven transitions and config changes are not
  recomputable from collected data, so the audit log is what replay reads for them: alarm ack and
  snooze ([alarms and actions](/architecture/alarms-actions/)), and every config change a
  reconcile consumes.

## Reads

- **Secret decrypts are always audited and never filterable.** Every read of secret material
  emits an `audit_log` (a credential decrypt), and that subset cannot be filtered away.
- **Other reads are not audited at the storage layer.** Optional read-audit is config-driven at
  the API layer (per-resource opt-in or a verbosity setting), off by default.

## Retention and integrity

Audit carries the **longest retention** of any ground-truth log (compliance), range-partitioned
by `ts` like the others. It is append-only by construction; a hash-chain or signed audit for
high-assurance deployments is an open item.

## Who consumes it

- **Replay**: operator transitions and config changes are replayed from here, not recomputed.
- **Reconcile**: config changes arrive as `audit_log` rows, so reconcile reacts to them.
- **The alarm projection**: ack and snooze come from audit.

## Open items

- Tamper-evidence (a hash-chain or signed audit) for high-assurance deployments.
- Read-audit granularity (per-resource opt-in versus a global verbosity setting).
- Whether an AI-accepted suggestion records the AI provenance and the approving operator in one
  row or two linked rows ([AI](/architecture/ai/)).
