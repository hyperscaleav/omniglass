---
title: AI
description: "AI as a governed capability acting through the same API, permission, and scope seams as any caller, marked and audited, with human-in-the-loop gating."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Partial]
Built today: an AI tool authenticates with a **bearer token as a `human` or `service` principal**, or an MCP client signs its person in with OAuth 2.1 for an agent token of its own ([ADR-2048](/architecture/decisions/#adr-2048-an-mcp-client-signs-in-with-oauth-21-for-an-agent-token-linked-to-it)), and acts within that principal's grants, and its richest seam, the **generated MCP server**, is served ([ADR-1162](/architecture/decisions/#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token)): every tool call runs its route with that token and is audited as that principal, marked as arriving over MCP. The server also serves **skills** as MCP prompts, workflows (triage an alarm, troubleshoot a component) that point the agent at the tools for a whole job and have it ask before any write ([ADR-2026](/architecture/decisions/#adr-2026-the-mcp-server-serves-embedded-skills-as-prompts)). An install also sets what an agent may do in each class of operation, refusing a `never` class server-side ([ADR-2063](/architecture/decisions/#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent)). The capability spectrum, the in-app assistant (omnibot, epic #1161) and the rest of the confirmation policy below are target design, each in its fence.
:::

## AI acts through the same seams as any principal

AI is **not a side channel**. It reaches the fleet through the same three seams every actor uses: the **API** (no private back door, no direct database path), **IAM permissions** (`<resource>:<action>` on every route), and the **Storage Gateway scope** (the ABAC visible-set on every applicable query). What stops a human stops the AI; there is no elevated AI lane.

The richest seam is the **generated [MCP server](/architecture/api/#also-an-mcp-surface)**: an MCP tool call is a call to a real API operation, carrying the **acting user or service principal's** token through the same routes, permissions, scope, and [audit](/architecture/audit/) as the SPA or the CLI. It is a generated client like the others: the tools are the operations stamped for MCP in the server's Go source, listed in the [MCP tools reference](/reference/mcp/), a curated catalog rather than a raw one-method-per-tool dump. Each tool tells the client what it may assume (read-only, destructive, idempotent, open-world), so the agent's own client can hold a write for its user's confirmation. The catalog is **credential-blind**: no tool takes or returns a credential in plaintext, so a secret's value or a minted token never enters a model's context ([ADR-2025](/architecture/decisions/#adr-2025-the-mcp-catalog-is-credential-blind-and-reads-the-fleets-alarms-in-one-call)). An agent connects at `/mcp` over streamable HTTP, or over stdio with `omniglass mcp`; see [connect an agent](/guides/agents/).

Tools say what can be done; **skills** say how the house does a job with them. Each is one `SKILL.md` (the Agent Skills format: front matter, then a Markdown workflow) embedded in the binary and served as an MCP prompt, so it ships with the release whose tools it names and a test fails any tool it names that the catalog lacks. A skill carries no permissions of its own: the [MCP skills reference](/reference/mcp/skills/) reads them from the gates of the tools it uses. Every skill reads before it changes anything, asks before every write, and never asks for a credential: a poll or a command runs on the server with the credentials bound to the endpoint, so the agent never needs one, and a skill tells it not to repeat one a tool's answer quotes. Masking credentials out of an answer is the route's job, not the skill's.

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

The install-wide half of that policy is built ([ADR-2063](/architecture/decisions/#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent)): an admin sets `always`, `ask` or `never` for each class of operation (read, write, destructive, credential destination) in the `agent` settings namespace. The server refuses a `never` class to any agent request (an agent token, or any call over MCP) and audits the refusal as the agent's; `ask` is the client's to honor, carried on every listed tool's `_meta`. There is no server-side pending approval: a person approves in their own client. Two narrower layers sit on top ([ADR-2088](/architecture/decisions/#adr-2088-role-and-per-tool-overrides-narrow-the-agent-policy-strictest-wins)): a **role** override sets a class for the agents of anyone granted that role, and a **tool** override sets one operation. Each only narrows: the strictest of the platform, every role the person is granted and the tool wins, and the resolved policy names the layer that set it, so a refusal says whether a tool, a role or the class refused it. See [what an agent may do](/guides/agents/#what-an-agent-may-do).
