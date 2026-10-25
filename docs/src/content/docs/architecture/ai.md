---
title: AI
description: "AI as a governed capability acting through the same API, permission, and scope seams as any caller, marked and audited, with human-in-the-loop gating."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
Built today: an AI tool authenticates with a **bearer token as a `human` or `service` principal** (OAuth/OIDC is deferred, [ADR-0004](/architecture/decisions/#adr-0004-credentials-ship-bearer-only)) and acts with exactly that principal's grants, and its richest seam, the **generated MCP server**, is served ([ADR-1162](/architecture/decisions/#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token)): every tool call runs its route with that token and is audited as that principal, marked as arriving over MCP. The capability spectrum, the in-app assistant (omnibot, epic #1161) and the confirmation policy below are target design, each in its fence.
:::

## AI acts through the same seams as any principal

AI is **not a side channel**. It reaches the fleet through the same three seams every actor uses: the **API** (no private back door, no direct database path), **IAM permissions** (`<resource>:<action>` on every route), and the **Storage Gateway scope** (the ABAC visible-set on every applicable query). What stops a human stops the AI; there is no elevated AI lane.

The richest seam is the **generated [MCP server](/architecture/api/#also-an-mcp-surface)**: an MCP tool call is a call to a real API operation, carrying the **acting user or service principal's** token through the same routes, permissions, scope, and [audit](/architecture/audit/) as the SPA or the CLI. It is a generated client like the others: the tools are the operations stamped for MCP in the server's Go source, listed in the [MCP tools reference](/reference/mcp/), a curated catalog rather than a raw one-method-per-tool dump. Each tool tells the client what it may assume (read-only, destructive, idempotent, open-world), so the agent's own client can hold a write for its user's confirmation. An agent connects at `/mcp` over streamable HTTP, or over stdio with `omniglass mcp`; see [connect an agent](/guides/agents/).

## Provenance and audit

The write attributes to the **acting principal** in [`audit_log`](/architecture/audit/), so the trail names a responsible actor on every move, and the row's **channel** marks how it arrived: `mcp` for a tool call, with the client the agent declared beside it. The marking rides alongside the actor, never replacing it.

:::design[Target design (ADR-0001): AI as a governed capability]

AI in Omniglass is a **capability spanning assistive to operational**, governed exactly like any other actor: it enriches and explains at one end, proposes and acts at the other.

From assistive toward operational:

- **Enrichment**: context, a likely cause, a suggested next step on the occurrence in front of the operator. Read-only, inline.
- **Diagnosis and reporting**: troubleshooting, root-cause analysis across correlated signals, report generation (health summaries, incident write-ups, period reviews).
- **Natural-language surfaces**: NL business query ("which rooms had the most ghost meetings last month"), NL configuration, NL template development. The first is omnibot, the in-app assistant that monitors a component and builds its driver, an MCP client of this same server (epic #1161).
- **Operational actions**: acting on an operator's behalf (room and meeting rebooking, platform configuration), under that operator's grants.
- **Closed-loop automation**: diagnose-and-fix for a known failure class. **Human-in-the-loop is the default**: a mutating action is gated until the class has earned looser handling.

Every AI-produced output (an explanation, a drafted driver, a report) is **marked as AI-sourced**, keeping the capability assistive, not authoritative: a reader can always tell what came from AI, weigh it, and trace it.
:::

:::design[Human-in-the-loop gating, tracked in #1161]

Mutating AI actions can require **operator sign-off**: propose, approve, execute, the approval landing in the audit trail; read and diagnostic actions run ungated within the acting principal's scope. A **policy on AI-sourced mutations**, not a separate authorization model: the gate is an extra confirmation on top of the principal's grants, never beyond them. omnibot never executes a write its user has not confirmed (#1166); an external agent's own client gates its writes from the tool annotations.
:::
