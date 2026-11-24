---
title: AI
description: "How AI is built into Omniglass: agents act through the same API, permissions and scope as any caller, on a person's token, audited and marked, with an install policy on what they may do."
sidebar:
  order: 53
  badge:
    text: Partial
    variant: note
---

AI in Omniglass is not a side channel. An agent reaches the fleet through the same three seams every
caller does: the [API](/architecture/api/), the permission on every route, and the scope the
[Storage Gateway](/architecture/storage/) injects on every query. What stops a person stops their
agent, and there is no elevated lane. Connecting one and running omnibot are
[connect an agent](/developer/agents/); this page is the design under it.

## An agent acts as its person

An agent authenticates with a bearer token as a person or a machine user, ideally an
[agent token](/architecture/identity-access/#agent-tokens) its person minted for it, and holds at
most that user's grants. An MCP client with no token is pointed at the server's OAuth 2.1 metadata and
signs its person in for an agent token of its own
([ADR-2048](/architecture/decisions/#adr-2048-an-mcp-client-signs-in-with-oauth-21-for-an-agent-token-linked-to-it)).
There is no agent user with grants of its own.

The richest seam is the [MCP server](/architecture/api/#also-an-mcp-surface), generated from the same
document as the console client and the CLI. A tool call is a call to a real route with the agent's
token, so the route's permission, scope and audit apply unchanged. The catalog is credential-blind: no
tool takes or returns a credential in plaintext, so a secret's value or a minted token never enters a
model's context
([ADR-2025](/architecture/decisions/#adr-2025-the-mcp-catalog-is-credential-blind-and-reads-the-fleets-alarms-in-one-call)).

Tools say what can be done; **skills** say how a job is done with them. Each is one `SKILL.md` embedded
in the binary (`internal/mcp/skills`) and served as an MCP prompt, so it ships with the release whose
tools it names, and a test fails a skill naming a tool the catalog lacks. A skill carries no permission
of its own; the [MCP skills reference](/reference/mcp/skills/) reads its needs from its tools' gates.
Every skill reads before it changes anything, asks before every write, and never asks for a credential
([ADR-2026](/architecture/decisions/#adr-2026-the-mcp-server-serves-embedded-skills-as-prompts)).

## Provenance

A write attributes to the acting user in the [audit log](/architecture/audit/), and the row's channel
says how it arrived: `mcp` for a tool call, with the client the agent declared beside it, and `omnibot`
for omnibot's own. An agent token's rows also name the agent. The `omnibot` channel is stamped from the
credential, never from what a client declares: only a token minted for that channel writes it. The
marking rides beside the actor and never replaces it.

## What an agent may do

An admin sets `always`, `ask` or `never` for each class of operation (read, write, destructive,
credential destination) in the `agent` [settings](/architecture/settings/)
([ADR-2063](/architecture/decisions/#adr-2063-an-install-sets-always-ask-or-never-per-class-of-operation-for-every-agent)).
The server refuses a `never` class to any agent request, an agent token or any call over MCP, before the
route runs, and audits the refusal. `ask` is the client's to honor: every listed tool carries its
resolved policy in its metadata, and the person approves in their own client, so nothing waits on the
server. A role override and a per-tool override can only narrow it; the strictest of the install, the
person's roles and the tool wins, and the answer names the layer that set it
([ADR-2088](/architecture/decisions/#adr-2088-role-and-per-tool-overrides-narrow-the-agent-policy-strictest-wins)).
The policy is a confirmation on top of the person's grants, never a reach beyond them.

## omnibot

omnibot is the assistant that ships with Omniglass. Its loop (`internal/assistant`) reads each tool's
resolved policy, runs an `always` call, puts an `ask` call to its person, and is never offered a `never`
tool; a declined call does not run, and the model is told so.

**The server runs it**
([ADR-2121](/architecture/decisions/#adr-2121-the-server-runs-omnibot-as-the-person-streaming-ag-ui)).
`POST /assistant/conversations/{name}:run` runs the person's last message over the stored
conversation and streams the run back as [AG-UI](https://docs.ag-ui.com/) events: the model's text and
reasoning, each call and its result, and the run's end. The run holds a token minted for the person
on the `omnibot` channel alone, for 30 minutes at most and revoked when the stream ends, and calls its
tools in process through the same router an MCP call takes. An `ask` ends the run on an interrupt
naming the call; the next run's `resume` answers it. A run claims its conversation before any tool runs,
so a second one is a 409 and a call runs once. Only the person, as themselves, runs their
conversations: an agent token or an impersonation session is refused. The console's panel and
`omniglass omnibot` on the CLI are both clients of this route.

**A conversation is its owner's alone**
([ADR-2113](/architecture/decisions/#adr-2113-a-persons-omnibot-conversations-are-theirs-alone)).
Conversations are stored by name under `/assistant/conversations`, behind `assistant:use`, and every
read and write carries the owner as its predicate, so anyone else, an admin included, gets a 404. An
append names the last message its client saw, so two clients cannot interleave one. A paused run keeps
only its transcript; there is no server-side pending approval. A conversation is pruned
`assistant.conversation_days` after it last moved, 30 by default.

**Its model is configured, and its key stays on the server**
([ADR-2109](/architecture/decisions/#adr-2109-omnibots-model-key-stays-on-the-server-with-no-relay)).
The `assistant` settings name an OpenAI-compatible provider (OpenRouter by default), its model, and
`key_secret`, the name of a `$platform` secret. One adapter speaks chat completions with tool calls, so
the same settings reach a hosted provider or a local one by `base_url`, and the call leaves through the
egress guard like any outbound request. The server unseals the key in process for each run and sends it
only as that call's bearer. With no provider, a server plays a recorded conversation where one is
configured, and says so; otherwise a run is a 503.

**A person hands omnibot a credential by name.** In the CLI's chat, `/secret` reads the value with the
echo off, creates the secret with the person's own token, and adds only its reference to the
conversation. The model binds it by that name and the server uses it.

:::design[AI from assistive to operational, tracked in #1161]
AI is a capability that spans enrichment to action, governed like any other actor:

- **Enrichment**: a likely cause and a next step on the alarm in front of an operator, read-only.
- **Diagnosis and reporting**: root cause across correlated signals, health summaries and reviews.
- **Natural-language authoring**: a question about the fleet, a rule, a driver, drafted for an
  operator to edit and approve through the ordinary audited create, never saved by the AI itself.
- **Operational actions**: acting on an operator's behalf under that operator's grants.
- **Closed-loop automation**: diagnose and fix a known class of failure, gated by a person until the
  class has earned looser handling.

Every AI-produced output (an explanation, a drafted rule or driver, a report) is marked as AI-sourced,
so a reader can always tell what came from a model, weigh it, and trace it.
:::
