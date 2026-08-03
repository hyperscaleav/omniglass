---
title: AI
description: "AI as a governed capability acting through the same API, permission, and scope seams as any caller, marked and audited, with human-in-the-loop gating."
sidebar:
  badge:
    text: Design
    variant: caution
---

:::note[Built today]
An AI tool authenticates with a **bearer token or password as a `human` or `service` principal** (OAuth/OIDC is deferred, [ADR-0004](/architecture/decisions/#adr-0004-credentials-ship-bearer-only)) and acts with exactly that principal's grants, so it reaches the estate through the same seams every caller uses, never a private lane ([identity and access](/architecture/identity-access/)). Everything below this note is the target design.
:::

:::design[Target design (ADR-0001): AI as a governed capability]

AI in Omniglass is a **capability spanning assistive to operational**, governed exactly like any other actor: it enriches and explains at one end, proposes and acts at the other.

## The capability spectrum

From assistive toward operational:

- **Enrichment**: context, a likely cause, a suggested next step on the occurrence in front of the operator. Read-only, inline.
- **Diagnosis and reporting**: troubleshooting, root-cause analysis across correlated signals, report generation (health summaries, incident write-ups, period reviews).
- **Natural-language surfaces**: NL business query ("which rooms had the most ghost meetings last month"), NL configuration, NL template development.
- **Operational actions**: acting on an operator's behalf (room and meeting rebooking, platform configuration), under that operator's grants.
- **Closed-loop automation**: diagnose-and-fix for a known failure class. **Human-in-the-loop is the default**: a mutating action is gated until the class has earned looser handling.

## AI acts through the same seams as any principal

AI is **not a side channel**. It reaches the estate through the same three seams every actor uses: the **API** (no private back door, no direct database path), **IAM permissions** (`<resource>:<action>` on every route), and the **Storage Gateway scope** (the ABAC visible-set on every applicable query). What stops a human stops the AI; there is no elevated AI lane.

The richest seam is the **generated [MCP server](/architecture/api/)**: an MCP tool call is a call to a real API operation, carrying the **acting user or service principal's** credential through the same routes, permissions, scope, and [audit](/architecture/audit/) as the SPA or the CLI. A generated client like the others: a curated tool catalog, the [views](/architecture/views/) exposed as search tools, not a raw one-method-per-tool dump.

## Provenance and audit

Every AI-produced output is **marked as AI-sourced and audited**, keeping the capability assistive, not authoritative: a reader can always tell what came from AI, weigh it, and trace it. The write attributes to the **acting principal** in [`audit_log`](/architecture/audit/), the marking riding alongside, so the trail names a responsible actor on every move.

## Human-in-the-loop gating

Mutating AI actions can require **operator sign-off**: propose, approve, execute, the approval landing in the audit trail; read and diagnostic actions run ungated within the acting principal's scope. A **policy on AI-sourced mutations**, not a separate authorization model: the gate is an extra confirmation on top of the principal's grants, never beyond them.
:::
