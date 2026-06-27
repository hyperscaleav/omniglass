---
title: AI
description: "AI as a governed capability acting through the same API, permission, and scope seams as any caller, marked and audited, with human-in-the-loop gating."
sidebar:
  badge:
    text: Design
    variant: caution
---

AI in Omniglass is a **capability that spans from assistive to operational**, governed exactly like any other actor: at the assistive end it enriches and explains, at the operational end it proposes and acts. Today an AI tool authenticates via **OAuth as a `human` or `service` principal** and acts with exactly that principal's grants, so it reaches the estate through the same seams every caller uses, never a private lane ([identity and access](/architecture/identity-access/)).

## The capability spectrum

What AI does, from the assistive end toward the operational end:

- **Enrichment.** Event and alarm enrichment: context, a likely cause, a suggested next step on an occurrence the operator is already looking at. Read-only, surfaced inline.
- **Diagnosis and reporting.** Troubleshooting support, root-cause analysis across correlated signals, and report generation (health summaries, incident write-ups, period reviews).
- **Natural-language surfaces.** NL business query ("which rooms had the most ghost meetings last month"), NL configuration (authoring dashboards, rules, and alarms from a description), and NL template development (drafting a component template from a device's behavior).
- **Operational actions.** Acting on the platform on an operator's behalf: room and meeting rebooking, and general platform configuration, under that operator's grants.
- **Closed-loop automation.** Diagnose-and-fix flows that close the loop on a known failure class. **Human-in-the-loop is the default**: a mutating action is gated until the class has earned looser handling.

## AI acts through the same seams as any principal

AI is **not a side channel**. It reaches the estate through the same three seams every actor uses:

- the **API** (no private back door, no direct database path),
- **IAM permissions** (the `<resource>:<action>` capability checked on every route), and
- the **Storage Gateway scope** (the ABAC visible-set injected on every applicable query).

The richest AI seam is the **generated [MCP server](/architecture/api/)**: an MCP tool call is a call to a real API operation, so an external model drives Omniglass through the same routes, permissions, scope, and [audit](/architecture/audit/) as the SPA or the CLI, carrying the **acting user or service principal's** credential, never a parallel surface. It is a generated client like the others (a curated tool catalog, the [views](/architecture/views/) exposed as search tools, not a raw one-method-per-tool dump).

If a permission or a scope would stop a human from doing something, it stops the AI doing it too. There is no elevated AI lane.

## Provenance and audit

Every AI-produced output, an enrichment, a calculated value, a configuration change, is **marked as AI-sourced and audited**. The marking is what keeps the capability assistive-not-authoritative: a reader can always tell what came from AI, weigh it accordingly, and trace it. The audit half is native: the write attributes to the **acting principal** (the human or service the AI authenticated as) in [`audit_log`](/architecture/audit/), and the AI-sourced marking rides alongside, so the trail names a responsible actor on every move. Nothing AI touches is anonymous or unattributable.

## Human-in-the-loop gating

Mutating AI actions can require **operator sign-off**: the AI surfaces a proposed change, an operator approves it, then it executes, and the approval lands in the audit trail. Read and diagnostic actions run within the acting principal's scope without a gate. This is a **policy on AI-sourced mutations**, not a separate authorization model: the AI never exceeds the grants of the principal it acts as, and the gate is an extra confirmation on top of that boundary.
