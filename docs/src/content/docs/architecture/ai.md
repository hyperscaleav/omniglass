---
title: AI
description: "AI as a governed capability along an assistive-to-agentic spectrum: same seams, OAuth delegation, provenance, and human-in-the-loop gating."
sidebar:
  badge:
    text: Spec
    variant: caution
---

AI in Omniglass is a **capability that spans an assistive-to-agentic spectrum**, governed exactly like any other actor. At the assistive end it enriches and explains; at the agentic end it proposes and acts. The capabilities differ across the spectrum; the governance does not move. This page is the architecture of that governance: how AI plugs into the same seams as any principal, why it acts through delegation rather than its own broad identity, and what keeps it assistive-not-authoritative and always traceable.

## The capability spectrum

What AI does, grouped from the assistive end toward the agentic end. The agentic groups rest on the governance below.

- **Enrichment.** Event and alarm enrichment: attaching context, a likely cause, and a suggested next step to an occurrence the operator is already looking at. Read-only, surfaced inline.
- **Diagnosis and reporting.** Troubleshooting support, root-cause analysis across correlated signals, and report generation (health summaries, incident write-ups, period reviews).
- **Natural-language surfaces.** NL business query ("which rooms had the most ghost meetings last month"), NL configuration (authoring dashboards, rules, and alarms from a description), and NL template development (drafting a component template from a device's behavior).
- **Operational actions.** Acting on the platform on an operator's behalf: room and meeting rebooking, and general platform configuration.
- **Autonomous agents.** Diagnose-and-fix agents that close the loop on a known failure class. **Human-in-the-loop is the default; autonomy is per-class and earned**: every agentic action is gated until the class has earned it.

## AI acts through the same seams as any principal

AI is **not a side channel**. It reaches the estate through the same three seams every actor uses:

- the **API** (no private back door, no direct database path),
- **IAM permissions** (the `<resource>:<action>` capability checked on every route), and
- the **Storage Gateway scope** (the ABAC visible-set injected on every applicable query).

If a permission or a scope would stop a human from doing something, it stops the AI doing it too. There is no elevated AI lane.

## AI-on-behalf-of-a-user is OAuth delegation

When AI acts for a user, it uses **that user's delegated authority**, not an identity of its own. This is the **delegation seam**: OAuth on-behalf-of (delegation), where an agent holds a delegated, scoped, audited credential and operates strictly within the granting user's permissions and scope. The agent cannot exceed the user who delegated to it, and the action is attributable to both. AI does **not** get its own broad principal kind. See [identity and access](/architecture/identity-access/) for the principal model and the delegation mechanism.

## Provenance and audit

Every AI-produced output, an enrichment, a calculated value, a configuration change, is **marked as AI-sourced and audited**. The marking is what makes the capability assistive-not-authoritative: a reader can always tell what came from AI, weigh it accordingly, and trace it. The audit half ties into the existing model ([audit](/architecture/audit/)): an AI-influenced write records both the AI provenance and the human in whose authority it ran, so the trail names a responsible actor on every move. Nothing AI touches is anonymous or unattributable.

## Human-in-the-loop gating

Autonomous action is **gated before it is allowed**: propose -> approve -> act. The agent surfaces a proposed change, a human approves it, then it executes, and the approval lands in the audit trail. Full autonomy for a given failure class is a deliberate promotion that a class earns by its track record under the gate, never the starting state. The gate is the safety boundary that lets the capability span toward agentic without the governance moving with it.
