---
title: Audit
description: The who-did-what record, written once in the same transaction as the change it describes.
sidebar:
  badge:
    text: Design
    variant: caution
---

The audit log is how an operator answers "who changed this, and to what?" without trusting memory: every mutation is recorded once, at the source.

## The model

`audit_log` is **ground truth** (not derived): one row per mutation, carrying `actor`, `verb`,
`resource_kind`, `resource_id`, and the `old -> new` diff.

- **Write-time mandatory.** Every API write emits one `audit_log` in the **same transaction**
  as the data write, a storage-layer responsibility, not per-handler discipline, so it cannot
  be forgotten or bypassed.
- **The actor** is resolved by IAM ([identity and access](/architecture/identity-access/)): the
  human, service, node, or agent.
- **An AI-accepted suggestion is one row.** The actor is the `agent` principal that performed the
  write; its sponsoring human is the accountable operator. Both are native principal facts ([AI](/architecture/ai/)),
  so the row names the agent actor and carries the sponsor as the accountable human, no special two-row
  case needed.
- **Ground truth a backtest reads.** Operator-driven transitions and config changes are not
  recomputable from collected data, so the audit log is what a rule backtest reads for them: alarm ack and
  snooze ([alarms and actions](/architecture/alarms-actions/)), and every config change a
  reconcile consumes.

## Reads

- **Secret decrypts are always audited and never filterable.** Every read of secret material
  emits an `audit_log` (a credential decrypt), and that subset cannot be filtered away.
- **Other reads are not audited at the storage layer.** Optional read-audit is config-driven at
  the API layer (per-resource opt-in or a verbosity setting), off by default.

:::caution[Open question]
The read-audit granularity: per-resource opt-in versus a global verbosity setting.
:::

## Retention and integrity

Audit carries the **longest retention** of any ground-truth log (compliance), range-partitioned
by `ts` like the others. It is append-only by construction.

:::caution[Open question]
Tamper-evidence (a hash-chain or signed audit) for high-assurance deployments.
:::

## Who consumes it

- **Backtest**: a rule backtest reads operator transitions and config changes from here, since they are not recomputable.
- **Reconcile**: config changes arrive as `audit_log` rows, so reconcile reacts to them.
- **The alarm projection**: ack and snooze come from audit.

