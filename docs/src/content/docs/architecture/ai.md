---
title: AI
description: "AI as a governed capability along an assistive-to-agentic spectrum: the sponsored agent principal, same seams, provenance, and human-in-the-loop gating."
sidebar:
  badge:
    text: Spec
    variant: caution
---

AI in Omniglass is a **capability that spans an assistive-to-agentic spectrum**, governed exactly like any other actor. At the assistive end it enriches and explains; at the agentic end it proposes and acts. The capabilities differ across the spectrum; the governance does not move. This page is the architecture of that governance: how AI acts as the **`agent` principal kind**, sponsored by a human and scope-bounded to a strict subset of that sponsor's authority, how it plugs into the same seams as any principal, and what keeps it assistive-not-authoritative and always traceable.

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

## AI is the sponsored `agent` principal

An AI actor is its own principal: the **`agent`** kind, alongside human, service, and node. It carries its own identity, its own credential, and its own audit trail, so an agent's actions are first-class facts rather than a borrowed human session.

Every agent is **mandatorily sponsored by a human**. The sponsor is the accountable human behind the agent, recorded as a relationship on the agent principal itself. An agent's authority is the **upper boundary set by its sponsor**: its permissions and ABAC scope are a **strict subset of the sponsor's**, enforced at grant time and **clamped to the intersection** if the sponsor's scope later shrinks. An agent can never exceed, and never outlive, its sponsor's authority. Because the agent is a distinct principal, its credential has its own lifecycle: revoke or rotate the agent independently of the human.

**OAuth on-behalf-of is the auth mechanism backing the agent**, how an external AI proves it is acting for its sponsor. It is the credential the agent presents, not a shortcut that clones the sponsor's scope: the subset-and-clamp invariant is what bounds the agent, not the OAuth grant. This is symmetric with the other bounded kinds: a node is bounded by its placement, an agent is bounded by its sponsor.

See [identity and access](/architecture/identity-access/) for the principal-kinds model, the per-kind `agent` table, and the subset/clamp invariant.

## Provenance and audit

Every AI-produced output, an enrichment, a calculated value, a configuration change, is **marked as AI-sourced and audited**. The marking is what makes the capability assistive-not-authoritative: a reader can always tell what came from AI, weigh it accordingly, and trace it. The audit half is native to the principal model ([audit](/architecture/audit/)): an AI-influenced write attributes to the **agent** as the actor and names its **sponsor** as the accountable human, both as plain principal facts, so the trail names a responsible actor on every move. Nothing AI touches is anonymous or unattributable.

## Human-in-the-loop gating

**propose -> approve** is an agent-level policy. Mutating actions can require **sponsor sign-off**: the agent surfaces a proposed change, its sponsor approves it, then it executes, and the approval lands in the audit trail. Read and diagnostic actions run **autonomously within the agent's scope**, no approval gate. Full autonomy for a given mutating failure class is a deliberate promotion that a class earns by its track record under the gate, never the starting state. The gate is the safety boundary that lets the capability span toward agentic without the governance moving with it.
