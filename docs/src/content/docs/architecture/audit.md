---
title: Audit
description: "How the audit log is built: the who-did-what record, written once in the same transaction as the change it describes."
sidebar:
  order: 23
  badge:
    text: Partial
    variant: note
---

The audit log answers "who changed this, and to what?": every mutation is recorded once, at the
source. To read it, see [the admin Audit page](/admin/audit/).

## The model

`audit_log` is ground truth, not derived: one row per mutation, carrying `actor`, `verb`, `resource`,
`resource_id` and the `old` and `new` images.

- **Written in the same transaction.** Every API write emits its row in the transaction that makes the
  change, in the storage layer, so it cannot be forgotten or bypassed.
- **`resource_id` is the primary key, never the name.** A named entity is addressable by uuid or by a
  renameable handle, so recording whichever the caller used would give one entity two audit keys. The
  name at the time is in the images, a point-in-time snapshot rather than a key. A guard fails on a
  `writeAuditRes` argument that is not primary-key shaped.
- **A row outlives its subject.** `resource_id` is text and never a foreign key. An update carries the
  moved fields in both images and enough of the subject to name it; a delete carries the whole subject
  in `old`. Images are read under the row's lock, so they are the state the write replaced. A secret's
  image is its metadata and a digest of the sealed value, never the plaintext, and the password verbs
  carry no field images ([ADR-1442](/architecture/decisions/#adr-1442-an-audit-row-outlives-its-subject-both-images-and-a-string-reference)).
- **The actor** is a person, machine user or outpost, resolved by the gateway to its identifier and
  snapshotted on the row (`actor_username`), so the trail stays attributable after the actor is purged
  ([ADR-0016](/architecture/decisions/#adr-0016-a-principal-can-be-purged-and-the-audit-trail-is-denormalized-to-survive-it),
  [ADR-0110](/architecture/decisions/#adr-0110-a-principals-identifier-is-the-gateways-answer-not-a-stored-functions)).
  An impersonated action records both actors.
- **How it arrived rides beside the actor.** A row carries `channel` and `client`: an
  [MCP](/architecture/api/#also-an-mcp-surface) call reads `channel = 'mcp'` with the client the caller
  declared, and an [omnibot](/architecture/ai/) run reads `omnibot`, stamped from the credential and
  never claimed by the caller ([ADR-1162](/architecture/decisions/#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token)).
- **An agent token names its agent.** A row written with an
  [agent token](/architecture/identity-access/#agent-tokens) carries `agent` beside the person, on
  every channel ([ADR-1179](/architecture/decisions/#adr-1179-an-agent-token-is-a-token-type-on-its-persons-credential-and-never-reveals)).
- **A token is audited.** Minting and revoking write rows carrying the prefix, purpose and expiry,
  never the token or its hash.

## The second write lane

Login and logout run on read paths with no transaction, so they emit through a standalone seam under
`resource = 'auth'`, with the verbs `login`, `logout`, `login_failed`, `login_denied`, `login_locked`
and `revoke_session`, and `agent_refused` when the install's agent policy refuses an agent's call.

## Reads

Every read of secret material writes a row, with distinct `reveal` and `copy` verbs, and that subset
cannot be filtered away. Other reads are not audited.

`GET /audit-log` is gated by `audit:read:admin`, out of a wildcard's reach, so only admin and owner
see it. Rows return newest first, filterable by `resource`, `verb`, `agent`, `channel` and `client`,
paged backward with `before` and `limit` (default 100, at most 500). Redaction is the write side's:
the read passes images through verbatim.

:::design[The read-audit toggle, tracked in #526]
Optional audit of ordinary reads, per resource or by a verbosity setting, off by default.
:::

## Retention and integrity

Audit is append-only by construction and meant to have the longest retention of any log.

:::design[Retention partitioning and the consumers, tracked in #526]
Retention by time-partitioning `audit_log`, so aging out drops a partition and never deletes row by
row, is not built. Neither are the consumers that read operator transitions and config changes from
the trail: backtest, reconcile, and the alarm projection of ack and snooze.
:::

:::caution[Open question]
Tamper evidence (a hash chain or signed audit) for high-assurance installs.
:::
