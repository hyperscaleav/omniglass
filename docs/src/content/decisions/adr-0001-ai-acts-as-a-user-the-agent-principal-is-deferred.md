---
title: "AI acts as a user; the `agent` principal is deferred"
date: 2026-06-27
status: "Accepted"
summary: "AI acts as a `human` / `service` principal; a first-class `agent` principal is deferred"
---

- **Date:** 2026-06-27 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/), [AI](/architecture/ai/)
- **Decision:** An AI tool authenticates over OAuth as an ordinary `human` or `service` principal and acts
  with exactly that principal's grants. A dedicated first-class `agent` principal kind is **not** in the
  initial architecture.
- **Context:** A separate `agent` identity would need its own authN, its own grant semantics, and its own
  audit treatment before any AI surface exists to use it. Treating AI as a scoped, audited user reuses the
  whole identity machinery and keeps the audit trail honest (the acting principal is the human or service).
- **Note:** The schema's `principal.kind` CHECK already **reserves** the `agent` value so a later slice
  adds the kind without editing the applied auth migration; no `agent` identity is issued today. If and
  when a first-class agent identity is built, that is a new entry that supersedes this one.
- **Amended (#1162):** "over OAuth" above predates [ADR-0004](#adr-0004-credentials-ship-bearer-only),
  which ships bearer tokens and defers OAuth. An AI tool authenticates with a person's bearer token,
  and the MCP server runs every tool call as that token, its audit row marked with the MCP channel
  ([ADR-1162](#adr-1162-the-mcp-server-is-a-generated-client-of-the-operations-stamped-for-it-run-as-the-callers-token)).
  The decision itself stands: AI acts as a `human` or `service` principal, never as its own.
